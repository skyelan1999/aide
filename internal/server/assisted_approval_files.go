package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
)

// File proposals use the same independent reviewer, then the normal versioned
// apply path. Local workspace updates require exact original bytes and bound paths.
func (a *App) prepareAssistedLocalProposalLocked(f Change) (Change, bool) {
	if f.Source != "" || f.Applied || safePath(f.Path) != nil {
		return f, false
	}
	f.NoSymlinks = true
	t, err := a.proposalTargetLocked(f)
	if err != nil {
		return f, false
	}
	defer t.close()
	if t.root == nil {
		return f, false
	}
	t.noSymlinks = true
	original, err := readBoundText(t.root, f.Path)
	if errors.Is(err, os.ErrNotExist) {
		if f.BaseHash != "" || f.Before != "" {
			return f, false
		}
		return f, true
	}
	if err != nil || f.BaseHash == "" || hash(original) != f.BaseHash {
		return f, false
	}
	if f.Before != "" && !bytes.Equal([]byte(f.Before), original) {
		return f, false
	}
	// Give the reviewer the exact original bytes whose digest will be checked
	// again by the apply path; an omitted original is not treated as evidence.
	f.Before = string(original)
	return f, true
}

// An explicitly enabled assisted policy may apply ordinary new Markdown
// documents in the configured writable document binding without a second
// model call. Existing files and mixed batches still require review.
func routineNewDocumentBatch(files []Change) bool {
	if len(files) == 0 {
		return false
	}
	for _, f := range files {
		if f.Source != systemDocsSource || f.Applied || f.BaseHash != "" || f.Before != "" || f.Content == "" ||
			len(f.Content) > 64<<10 || strings.ToLower(path.Ext(f.Path)) != ".md" || safePath(f.Path) != nil {
			return false
		}
	}
	return true
}

func (a *App) reviewPendingFiles(parent context.Context, task *Task) {
	if a.applyRememberedFiles(task) {
		return
	}
	a.mu.Lock()
	s := a.approvalSessionLocked(task)
	if s == nil || !task.AutoReview || task.Status != "awaiting_approval" || len(task.Files) == 0 {
		a.mu.Unlock()
		return
	}
	gen := task.approvalGeneration
	round := int64(-1)
	if task.approvalReviewRound == round && task.approvalReviewGeneration == gen {
		a.mu.Unlock()
		return
	}
	task.approvalReviewRound, task.approvalReviewGeneration = round, gen
	task.ApprovalState = "reviewing"
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	task.approvalCancel = cancel
	files := append([]Change(nil), task.Files...)
	prompt, authorized := a.approvalAuthorizationLocked(task)
	cfg, keyErr := settingsForSelectedModel(a.settings, task.Model)
	if task.ModelSelection != nil {
		cfg.ReasoningEffort = task.ModelSelection.ReasoningEffort
	}
	if keyErr == nil {
		cfg.APIKey, keyErr = a.modelAPIKeyForConfigLocked(cfg)
	}
	cwd := task.AgentRoot.ContainerAbs
	if cwd == "" {
		cwd = a.containerAbs
	}
	sandbox, workspaceMode := sandboxMode(a.settings.SandboxMode), a.workspaceMode()
	workspaceID, workspaceRevision := a.wsID(), a.wsRevision
	eligible := authorized && sandbox != "read-only" && workspaceMode != "ssh"
	paths := []string{}
	for i, f := range files {
		if f.Source == "" {
			prepared, ok := a.prepareAssistedLocalProposalLocked(f)
			files[i] = prepared
			task.Files[i] = prepared
			if !ok {
				eligible = false
			}
		} else if f.BaseHash != "" || f.Before != "" || f.Applied || safePath(f.Path) != nil || !a.newProposalTargetEligibleLocked(f) {
			// Configured document destinations remain eligible only for new files.
			eligible = false
		}
		paths = append(paths, proposalDisplayPath(files[i]))
	}
	record := ApprovalReview{Command: "write_file: " + strings.Join(paths, ", "), Status: "reviewing", Reason: "正在独立审核文件提案及目标版本", At: time.Now().UTC().Format(time.RFC3339Nano), Source: "model", Kind: "files", Workspace: task.WorkspaceID, Root: task.AgentRoot, Fingerprint: approvalFingerprint(approvalFilesFingerprint(files))}
	if task.AssistantManaged && a.settings.AssistantAutoReview {
		record.Source = "assistant"
	}
	routineDocument := eligible && routineNewDocumentBatch(files)
	if routineDocument {
		record.Source = "system"
	}
	if len(task.ApprovalReviews) >= 50 {
		task.ApprovalReviews = task.ApprovalReviews[len(task.ApprovalReviews)-49:]
	}
	task.ApprovalReviews = append(task.ApprovalReviews, record)
	index := len(task.ApprovalReviews) - 1
	statePersisted := a.save(s) == nil
	if !statePersisted {
		eligible = false
	}
	a.mu.Unlock()
	a.broadcastSessionsChanged(s.ID)
	defer cancel()
	approved := false
	risk := ""
	reason := "现有文件、远程目标、只读沙箱、符号链接或已变化的提案需要手动确认"
	var usage TokenUsage
	if routineDocument {
		approved = true
		reason = "已启用辅助审批；仅创建绑定的本地读写目录中的新 Markdown 文档，目标与版本在应用时再次校验"
	} else if eligible && keyErr == nil {
		payload, _ := json.Marshal(map[string]any{"userRequest": prompt, "workingDirectory": cwd, "operation": "apply exact local workspace file proposals; configured document sources may create new files only", "executionIntent": fileApprovalExecutionIntent(files), "sandboxMode": sandbox, "workspaceMode": workspaceMode, "files": files, "documentDestination": "仅认可已记录的本地目标；已有工作区文件修改必须是用户明确要求，且 Before 与 BaseHash 对应将再次校验的原文"})
		if len(payload) <= 64000 {
			out, _, u, err := complete(ctx, cfg, []Message{{Role: "system", Content: assistedApprovalPolicy + "\n本次待审对象是 files 提案，不是 shell command。逐项判断路径、原文、完整新内容及用户授权。只有低/中风险且目标精确时才批准。"}, {Role: "user", Content: string(payload)}}, ProfileParams{MaxTokens: 512, Temperature: fp(0)}, nil, nil)
			usage = u
			var v struct {
				Decision        string
				Risk            string
				WithinWorkspace bool
				Authorized      bool
				Reason          string
			}
			if err == nil && json.Unmarshal(approvalDecisionJSON(out), &v) == nil && strings.TrimSpace(v.Reason) != "" {
				reason, risk = v.Reason, v.Risk
				approved = v.Decision == "approve" && (v.Risk == "low" || v.Risk == "medium") && v.WithinWorkspace && v.Authorized
			} else {
				reason = "文件提案审核失败或响应格式无效，请手动确认"
			}
		} else {
			reason = "提案超出独立审核内容限额，请手动确认"
		}
	}
	current := func(t *Task) error {
		if t != task || !t.AutoReview || t.approvalGeneration != gen || t.Status != "awaiting_approval" || ctx.Err() != nil || sandboxMode(a.settings.SandboxMode) != sandbox || a.workspaceMode() != workspaceMode || a.wsID() != workspaceID || a.wsRevision != workspaceRevision || a.sessions[s.ID] != s || s.Deleted || len(t.Files) != len(files) {
			return errors.New("审批已取消或提案环境已变化")
		}
		for i, f := range files {
			if t.Files[i] != f {
				return errors.New("文件提案已变化")
			}
		}
		return nil
	}
	a.mu.Lock()
	task.Usage = addUsage(task.Usage, usage)
	if current(task) != nil {
		approved = false
		reason = "审批已取消或提案环境已变化"
	}
	grace := a.approvalGracePeriod()
	status := "manual"
	if approved {
		status = "approved"
		if grace > 0 {
			status = "recommended"
		}
	}
	if task.approvalGeneration == gen && task.Status == "awaiting_approval" {
		task.ApprovalState = status
		task.ApprovalDeadline = ""
		if status == "recommended" {
			task.ApprovalDeadline = time.Now().UTC().Add(grace).Format(time.RFC3339Nano)
		}
	}
	if index < len(task.ApprovalReviews) && task.ApprovalReviews[index].At == record.At {
		rec := &task.ApprovalReviews[index]
		rec.Status, rec.Reason, rec.Risk = status, reason, risk
		rec.Deadline = task.ApprovalDeadline
		if approved {
			rec.Recommendation = "approve"
		} else {
			rec.Recommendation = "reject"
		}
	}
	if !statePersisted || a.save(s) != nil {
		approved = false
		if task.approvalGeneration == gen && task.Status == "awaiting_approval" {
			task.ApprovalState = "manual"
		}
		if index < len(task.ApprovalReviews) && task.ApprovalReviews[index].At == record.At {
			task.ApprovalReviews[index].Status = "manual"
			task.ApprovalReviews[index].Reason = "审批记录保存失败，未自动应用"
		}
	}
	a.mu.Unlock()
	if approved && status == "recommended" {
		// 推荐方案先展示给用户：在等待窗口内不落盘，超时未操作才按推荐执行。
		time.AfterFunc(grace, func() { a.applyRecommendedFiles(s.ID, task.ID, gen) })
	} else if approved {
		r, _ := http.NewRequest(http.MethodPost, "/", nil)
		r.SetPathValue("id", s.ID)
		r.SetPathValue("run", task.ID)
		w := &approvalApplyResult{header: make(http.Header)}
		a.applyTaskGuarded(w, r, current)
		if w.status != http.StatusOK {
			a.mu.Lock()
			if task.approvalGeneration == gen {
				task.ApprovalState = "manual"
			}
			if index < len(task.ApprovalReviews) && task.ApprovalReviews[index].At == record.At {
				task.ApprovalReviews[index].Status = "manual"
				task.ApprovalReviews[index].Reason = "审核通过，但文件未成功应用：" + w.body.String()
			}
			_ = a.save(s)
			a.mu.Unlock()
		}
	}
	a.broadcastSessionsChanged(s.ID)
}

type approvalApplyResult struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *approvalApplyResult) Header() http.Header { return w.header }
func (w *approvalApplyResult) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}
func (w *approvalApplyResult) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	return w.body.Write(b)
}

// approvalGracePeriod 返回推荐方案等待用户操作的时间；0 表示立即按推荐执行
// （仅用于测试或特殊部署）。
func (a *App) approvalGracePeriod() time.Duration {
	return a.approvalGrace
}

func updateLastApprovalReview(task *Task, status, reason string) {
	if task == nil || len(task.ApprovalReviews) == 0 {
		return
	}
	rec := &task.ApprovalReviews[len(task.ApprovalReviews)-1]
	rec.Status, rec.Reason = status, reason
	rec.Deadline = ""
}

// applyRecommendedFiles 在推荐方案的等待窗口到点后执行：只有提案、审批代次
// 和 run 状态都保持不变才真正落盘，用户已手动处理或提案已变化时静默放弃。
func (a *App) applyRecommendedFiles(sessionID, runID string, gen uint64) {
	a.mu.Lock()
	s := a.sessions[sessionID]
	if s == nil || s.Deleted {
		a.mu.Unlock()
		return
	}
	var task *Task
	for _, t := range s.Runs {
		if t.ID == runID {
			task = t
		}
	}
	if task == nil || task.Status != "awaiting_approval" || task.ApprovalState != "recommended" || task.approvalGeneration != gen {
		a.mu.Unlock()
		return
	}
	files := append([]Change(nil), task.Files...)
	task.ApprovalState = "applying"
	a.mu.Unlock()

	r, _ := http.NewRequest(http.MethodPost, "/", nil)
	r.SetPathValue("id", sessionID)
	r.SetPathValue("run", runID)
	guard := func(t *Task) error {
		if t != task || t.approvalGeneration != gen || len(t.Files) != len(files) {
			return errors.New("审批已取消或提案已变化")
		}
		for i, f := range files {
			if t.Files[i] != f {
				return errors.New("文件提案已变化")
			}
		}
		return nil
	}
	w := &approvalApplyResult{header: make(http.Header)}
	a.applyTaskGuarded(w, r, guard)

	a.mu.Lock()
	if w.status != http.StatusOK {
		if task.Status == "awaiting_approval" && task.approvalGeneration == gen {
			task.ApprovalState = "manual"
			task.ApprovalDeadline = ""
		}
		updateLastApprovalReview(task, "manual", "推荐方案自动执行失败："+w.body.String())
	} else {
		task.ApprovalState = "approved"
		task.ApprovalDeadline = ""
		updateLastApprovalReview(task, "approved", "已按推荐方案自动执行（等待窗口内未操作）")
	}
	_ = a.save(s)
	a.mu.Unlock()
	a.broadcastSessionsChanged(s.ID)
}

// declineTask 让用户明确不使用当前推荐方案：结束等待窗口并转手动确认，
// 已写入的提案仍保留，用户仍可自行应用。
func (a *App) declineTask(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.sessions[r.PathValue("id")]
	if s == nil {
		fail(w, 404, errors.New("会话不存在"))
		return
	}
	var task *Task
	for _, t := range s.Runs {
		if t.ID == r.PathValue("run") {
			task = t
		}
	}
	if task == nil {
		fail(w, 404, errors.New("任务不存在"))
		return
	}
	if task.Status != "awaiting_approval" || task.ApprovalState != "recommended" {
		fail(w, 409, errors.New("任务当前没有待确认的推荐方案"))
		return
	}
	task.approvalGeneration++
	task.ApprovalState = "manual"
	task.ApprovalDeadline = ""
	updateLastApprovalReview(task, "declined", "用户选择不使用推荐方案，转为手动确认")
	if err := a.save(s); err != nil {
		fail(w, 500, err)
		return
	}
	a.broadcastSessionsChanged(s.ID)
	jsonOut(w, 200, map[string]any{"declined": true})
}
