package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	sessionApprovalAssistedAuto = "assisted_auto"
	sessionApprovalFullAuto     = "full_auto"
	sessionApprovalRiskPhrase   = "我熟知并已确认风险"
)

type sessionApprovalGrant struct {
	SessionID string
	Mode      string
	Method    string
	ExpiresAt time.Time
}

func sessionApprovalModeElevated(mode string) bool {
	return mode == sessionApprovalAssistedAuto || mode == sessionApprovalFullAuto
}

func (a *App) sessionAutoReviewLocked(session *Session, assistantManaged bool) bool {
	if session != nil {
		switch session.ApprovalMode {
		case sessionApprovalAssistedAuto:
			return true
		case sessionApprovalFullAuto:
			return false
		}
	}
	return a.approvalPolicy.Enabled || assistantManaged && a.settings.AssistantAutoReview
}

func (a *App) taskSessionApprovalModeLocked(task *Task) string {
	if session := a.approvalSessionLocked(task); session != nil {
		return session.ApprovalMode
	}
	return ""
}

func (a *App) masterAuthSuccess(w http.ResponseWriter, method, sessionID, mode string) {
	if (sessionID != "" || mode != "") && method != "password" && method != "webauthn" {
		fail(w, http.StatusForbidden, errors.New("高风险审批必须通过账户密码或指纹授权，请先在设置 → 账户中配置"))
		return
	}
	response := map[string]any{"ok": true, "scope": "master", "unlocked": a.vaultIsUnlocked()}
	if method == "password" {
		response["unlocked"] = true
	}
	if sessionID != "" || mode != "" {
		grant, err := a.issueSessionApprovalGrant(sessionID, mode, method)
		if err != nil {
			fail(w, http.StatusServiceUnavailable, err)
			return
		}
		response["approvalGrant"] = grant
	}
	jsonOut(w, http.StatusOK, response)
}

func (a *App) issueSessionApprovalGrant(sessionID, mode, method string) (string, error) {
	if method != "password" && method != "webauthn" {
		return "", errors.New("高风险审批只接受账户密码或指纹授权")
	}
	if !sessionApprovalModeElevated(mode) || strings.TrimSpace(sessionID) == "" {
		return "", errors.New("审批授权必须绑定会话和目标模式")
	}
	a.mu.Lock()
	session := a.sessionLocked(sessionID)
	a.mu.Unlock()
	if session == nil || session.Deleted {
		return "", errors.New("目标会话不存在")
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	grant := base64.RawURLEncoding.EncodeToString(raw)
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(grant)))
	if err := a.approvalAudit.append(approvalAuditEntry{Action: "session_approval_auth_granted", SessionID: sessionID, Mode: mode, AuthMethod: method, Detail: "账户密码或指纹主身份校验通过；凭证两分钟有效且仅可使用一次"}); err != nil {
		return "", err
	}
	a.mu.Lock()
	if a.approvalGrants == nil {
		a.approvalGrants = map[string]sessionApprovalGrant{}
	}
	now := time.Now()
	for id, existing := range a.approvalGrants {
		if now.After(existing.ExpiresAt) {
			delete(a.approvalGrants, id)
		}
	}
	a.approvalGrants[key] = sessionApprovalGrant{SessionID: sessionID, Mode: mode, Method: method, ExpiresAt: now.Add(2 * time.Minute)}
	a.mu.Unlock()
	return grant, nil
}

// consumeSessionApprovalGrantLocked validates and consumes a grant atomically.
// The browser's risk dialog is only UX; this server-side grant is the actual
// authorization boundary and cannot be reused for another session or mode.
func (a *App) consumeSessionApprovalGrantLocked(sessionID, mode, token string) (string, error) {
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.TrimSpace(token))))
	grant, ok := a.approvalGrants[key]
	delete(a.approvalGrants, key)
	if !ok || time.Now().After(grant.ExpiresAt) || grant.SessionID != sessionID || grant.Mode != mode {
		return "", errors.New("审批授权无效、已过期或已使用，请重新验证密码或指纹")
	}
	return grant.Method, nil
}

func (a *App) setSessionApprovalMode(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Mode             string `json:"mode"`
		Grant            string `json:"grant"`
		RiskConfirmation string `json:"riskConfirmation"`
	}
	if err := decode(w, r, &input); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if input.Mode != "" && !sessionApprovalModeElevated(input.Mode) {
		fail(w, http.StatusBadRequest, errors.New("未知会话审批模式"))
		return
	}
	if sessionApprovalModeElevated(input.Mode) && input.RiskConfirmation != sessionApprovalRiskPhrase {
		fail(w, http.StatusBadRequest, errors.New("必须明确确认风险"))
		return
	}
	a.mu.Lock()
	session := a.sessionLocked(r.PathValue("id"))
	if session == nil || session.Deleted {
		a.mu.Unlock()
		fail(w, http.StatusNotFound, errors.New("会话不存在"))
		return
	}
	method := ""
	var err error
	if sessionApprovalModeElevated(input.Mode) {
		method, err = a.consumeSessionApprovalGrantLocked(session.ID, input.Mode, input.Grant)
		if err != nil {
			a.mu.Unlock()
			fail(w, http.StatusForbidden, err)
			return
		}
	}
	previous := session.ApprovalMode
	if previous == input.Mode {
		a.mu.Unlock()
		jsonOut(w, http.StatusOK, map[string]any{"ok": true, "mode": input.Mode})
		return
	}
	if err = a.approvalAudit.append(approvalAuditEntry{Action: "session_approval_mode_changed", SessionID: session.ID, AuthMethod: method, FromMode: previous, Mode: input.Mode, Detail: "仅当前会话生效；高风险模式已核对风险确认和一次性主身份授权"}); err != nil {
		a.mu.Unlock()
		fail(w, http.StatusServiceUnavailable, fmt.Errorf("审计日志不可写，审批模式未更改: %w", err))
		return
	}
	previousAuto := map[*Task]bool{}
	for _, task := range session.Runs {
		previousAuto[task] = task.AutoReview
	}
	session.ApprovalMode = input.Mode
	pending := []*Task{}
	for _, task := range session.Runs {
		task.AutoReview = a.sessionAutoReviewLocked(session, task.AssistantManaged)
		switch task.Status {
		case "running", "awaiting_approval", "awaiting_clarification", "queued", "paused":
			task.approvalGeneration++
			if task.approvalCancel != nil {
				task.approvalCancel()
			}
			if hasPendingApproval(task) {
				pending = append(pending, task)
			}
		}
	}
	if err = a.save(session); err != nil {
		session.ApprovalMode = previous
		for task, auto := range previousAuto {
			task.AutoReview = auto
		}
		a.mu.Unlock()
		fail(w, http.StatusInternalServerError, err)
		return
	}
	a.mu.Unlock()
	for _, task := range pending {
		task := task
		go func() {
			a.autoAnswerFullApproval(task)
			a.reviewPendingCommand(task)
			a.reviewPendingSurfaceAction(task)
			a.reviewPendingFiles(context.Background(), task)
		}()
	}
	a.broadcastSessionsChanged(session.ID)
	jsonOut(w, http.StatusOK, map[string]any{"ok": true, "mode": input.Mode})
}

func (a *App) verifySessionApprovalAudit(w http.ResponseWriter, _ *http.Request) {
	entries, head, err := a.approvalAudit.verify()
	if err != nil {
		fail(w, http.StatusConflict, err)
		return
	}
	jsonOut(w, http.StatusOK, map[string]any{"ok": true, "entries": entries, "head": head})
}

func approvalReceiptForTask(task *Task, question json.RawMessage, source, status, reason string) ApprovalReview {
	var current struct {
		Kind    string           `json:"approvalKind"`
		Command string           `json:"command"`
		Purpose *approvalPurpose `json:"approvalPurpose,omitempty"`
	}
	_ = json.Unmarshal(question, &current)
	effect := current.Command
	if surfaceApprovalKind(current.Kind) {
		effect = approvalSurfaceReceiptAction(question)
	}
	return ApprovalReview{Command: effect, Status: status, Reason: reason, At: time.Now().UTC().Format(time.RFC3339Nano), Source: source, Kind: current.Kind, Workspace: task.WorkspaceID, Root: task.AgentRoot, Fingerprint: approvalFingerprint(effect), Purpose: cleanApprovalPurpose(current.Purpose)}
}

// autoAnswerFullApproval confirms only actual approval questions, never ordinary
// model clarifications. The receipt and its chained audit record are durable
// before the execution goroutine is released.
func (a *App) autoAnswerFullApproval(task *Task) {
	a.mu.Lock()
	defer a.mu.Unlock()
	session := a.approvalSessionLocked(task)
	if session == nil || session.ApprovalMode != sessionApprovalFullAuto || task.Status != "awaiting_clarification" || task.AnswerCh == nil || task.approvalAnswered {
		return
	}
	var question struct {
		Kind string `json:"approvalKind"`
	}
	_ = json.Unmarshal(task.PendingQuestion, &question)
	if question.Kind != "shell" && !surfaceApprovalKind(question.Kind) {
		return
	}
	receipt := approvalReceiptForTask(task, task.PendingQuestion, "session_full_auto", "confirmed", "当前会话已启用全部通过审批；未经辅助审核自动确认，仍受沙箱、路径、目标与版本约束")
	if err := a.approvalAudit.append(approvalAuditEntry{Action: "session_full_auto_confirmed", SessionID: session.ID, RunID: task.ID, Mode: sessionApprovalFullAuto, Kind: receipt.Kind, Fingerprint: receipt.Fingerprint}); err != nil {
		task.ApprovalState = "manual"
		task.Error = "全部通过审批的审计日志写入失败，操作未执行: " + err.Error()
		_ = a.save(session)
		return
	}
	if len(task.ApprovalReviews) >= 50 {
		task.ApprovalReviews = task.ApprovalReviews[len(task.ApprovalReviews)-49:]
	}
	task.ApprovalReviews = append(task.ApprovalReviews, receipt)
	task.ApprovalState = "approved"
	if err := a.save(session); err != nil {
		task.ApprovalState = "manual"
		return
	}
	select {
	case task.AnswerCh <- "确认":
		task.approvalAnswered = true
		if task.approvalCancel != nil {
			task.approvalCancel()
		}
	default:
	}
}

func (a *App) autoApplySessionFiles(task *Task) {
	a.mu.Lock()
	session := a.approvalSessionLocked(task)
	if session == nil || session.ApprovalMode != sessionApprovalFullAuto || task.Status != "awaiting_approval" || len(task.Files) == 0 {
		a.mu.Unlock()
		return
	}
	generation := task.approvalGeneration
	fingerprint := approvalFingerprint(approvalFilesFingerprint(task.Files))
	a.mu.Unlock()
	r, _ := http.NewRequest(http.MethodPost, "/", nil)
	r.SetPathValue("id", session.ID)
	r.SetPathValue("run", task.ID)
	w := &approvalApplyResult{header: make(http.Header)}
	a.applyTaskGuarded(w, r, func(current *Task) error {
		if current != task || current.approvalGeneration != generation || current.Status != "awaiting_approval" || session.ApprovalMode != sessionApprovalFullAuto {
			return errors.New("全部通过审批状态或文件提案已变化")
		}
		record := ApprovalReview{Command: "write_file: session full auto proposal", Status: "confirmed", Reason: "当前会话已启用全部通过审批；未经辅助审核自动应用，仍执行路径、原文件版本与工作区校验", At: time.Now().UTC().Format(time.RFC3339Nano), Source: "session_full_auto", Kind: "files", Workspace: current.WorkspaceID, Root: current.AgentRoot, Fingerprint: fingerprint, FilePurposes: proposedFilePurposes(current.Files)}
		if err := a.approvalAudit.append(approvalAuditEntry{Action: "session_full_auto_files_confirmed", SessionID: session.ID, RunID: current.ID, Mode: sessionApprovalFullAuto, Kind: "files", Fingerprint: fingerprint}); err != nil {
			return fmt.Errorf("全部通过审批的审计日志写入失败: %w", err)
		}
		return a.persistApprovalReceiptLocked(current, record)
	})
	if w.status != http.StatusOK {
		a.mu.Lock()
		if task.approvalGeneration == generation && task.Status == "awaiting_approval" {
			task.Status = "failed"
			task.CanResume = false
			task.ApprovalState = "declined"
			task.Error = "全部通过审批未能安全应用文件，未转人工: " + w.body.String()
			_ = a.save(session)
		}
		a.mu.Unlock()
	}
	a.broadcastSessionsChanged(session.ID)
}

// rejectAssistedAutoApproval implements the no-human branch of assisted_auto:
// an absent, negative, timed-out or uncertain review declines the operation.
func (a *App) rejectAssistedAutoApproval(task *Task, round int64, generation uint64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	session := a.approvalSessionLocked(task)
	if session == nil || session.ApprovalMode != sessionApprovalAssistedAuto || task.answerRound != round || task.approvalGeneration != generation || task.Status != "awaiting_clarification" || task.AnswerCh == nil || task.approvalAnswered {
		return
	}
	if err := a.approvalAudit.append(approvalAuditEntry{Action: "session_assisted_auto_declined", SessionID: session.ID, RunID: task.ID, Mode: sessionApprovalAssistedAuto, Detail: "辅助审核未明确批准，按会话模式自动拒绝且不转人工"}); err != nil {
		task.Error = "辅助通过审批的拒绝审计写入失败: " + err.Error()
	}
	task.ApprovalState = "declined"
	if len(task.ApprovalReviews) > 0 {
		receipt := &task.ApprovalReviews[len(task.ApprovalReviews)-1]
		receipt.Status = "declined"
		receipt.Reason = strings.TrimSpace(receipt.Reason + "；会话辅助通过模式已自动拒绝，未转人工")
	}
	if err := a.save(session); err != nil {
		return
	}
	select {
	case task.AnswerCh <- "拒绝":
		task.approvalAnswered = true
	default:
	}
}
