package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

// File proposals use the same independent reviewer, then the normal versioned
// apply path. Only new local files are eligible; existing originals stay manual.
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
	cfg := a.settings
	if task.Model != "" {
		cfg.Model = task.Model
	}
	key, keyErr := a.modelAPIKeyLocked()
	cfg.APIKey = key
	cwd := task.AgentRoot.ContainerAbs
	if cwd == "" {
		cwd = a.containerAbs
	}
	eligible := keyErr == nil && authorized && a.settings.SandboxMode != "read-only" && a.workspaceMode() != "ssh"
	paths := []string{}
	for _, f := range files {
		paths = append(paths, f.Path)
		if f.BaseHash != "" || f.Before != "" || f.Applied || safePath(f.Path) != nil || checkVersion(a.workspace, f.Path, "") != nil {
			eligible = false
		}
	}
	record := ApprovalReview{Command: "write_file: " + strings.Join(paths, ", "), Status: "reviewing", Reason: "正在独立审核新文件提案", At: time.Now().UTC().Format(time.RFC3339Nano), Source: "model", Kind: "files", Workspace: task.WorkspaceID, Root: task.AgentRoot, Fingerprint: approvalFingerprint(approvalFilesFingerprint(files))}
	if len(task.ApprovalReviews) >= 50 {
		task.ApprovalReviews = task.ApprovalReviews[len(task.ApprovalReviews)-49:]
	}
	task.ApprovalReviews = append(task.ApprovalReviews, record)
	index := len(task.ApprovalReviews) - 1
	_ = a.save(s)
	a.mu.Unlock()
	a.broadcastSessionsChanged(s.ID)
	defer cancel()
	approved := false
	reason := "现有文件覆盖、远程写入、只读沙箱或已变化的提案需要手动确认"
	var usage TokenUsage
	if eligible {
		payload, _ := json.Marshal(map[string]any{"userRequest": prompt, "workingDirectory": cwd, "operation": "create new files using write_file; no existing file replacement", "files": files})
		if len(payload) <= 64000 {
			out, _, u, err := complete(ctx, cfg, []Message{{Role: "system", Content: assistedApprovalPolicy + "\n本次待审对象是 files 新文件提案，不是 shell command。逐项判断路径与完整内容是否符合用户授权。"}, {Role: "user", Content: string(payload)}}, ProfileParams{MaxTokens: 512, Temperature: fp(0)}, nil, nil)
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
				reason = "新文件审核失败或响应格式无效，请手动确认"
			}
		} else {
			reason = "提案超出独立审核内容限额，请手动确认"
		}
	}
	current := func(t *Task) error {
		if t != task || !t.AutoReview || t.approvalGeneration != gen || t.Status != "awaiting_approval" || ctx.Err() != nil || a.settings.SandboxMode == "read-only" || a.workspaceMode() == "ssh" || a.sessions[s.ID] != s || s.Deleted || len(t.Files) != len(files) {
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
	if a.save(s) != nil {
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
