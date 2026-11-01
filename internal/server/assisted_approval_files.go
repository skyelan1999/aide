package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
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
	eligible := keyErr == nil && authorized && sandbox != "read-only" && workspaceMode != "ssh"
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
	reason := "现有文件、远程目标、只读沙箱、符号链接或已变化的提案需要手动确认"
	var usage TokenUsage
	if eligible {
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
				reason = v.Reason
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
	status := "manual"
	if approved {
		status = "approved"
	}
	if task.approvalGeneration == gen && task.Status == "awaiting_approval" {
		task.ApprovalState = status
	}
	if index < len(task.ApprovalReviews) && task.ApprovalReviews[index].At == record.At {
		task.ApprovalReviews[index].Status = status
		task.ApprovalReviews[index].Reason = reason
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
	if approved {
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
