package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ApprovalPolicy belongs to this Aide instance, independent of sessions/models.
type ApprovalPolicy struct {
	Enabled  bool           `json:"enabled"`
	Revision uint64         `json:"revision"`
	Rules    []ApprovalRule `json:"rules,omitempty"`
}
type ApprovalRule struct {
	Kind        string    `json:"kind"`
	Workspace   string    `json:"workspace"`
	Root        AgentRoot `json:"root"`
	Fingerprint string    `json:"fingerprint"`
	At          string    `json:"at"`
	ExpiresAt   string    `json:"expiresAt,omitempty"`
}

func approvalFingerprint(text string) string {
	v := sha256.Sum256([]byte(text))
	return hex.EncodeToString(v[:])
}
func approvalFilesFingerprint(files []Change) string {
	// Applied flags are execution state; paths, original hashes and full contents are authorization.
	type item struct{ Path, BaseHash, Content string }
	values := make([]item, 0, len(files))
	for _, f := range files {
		values = append(values, item{f.Path, f.BaseHash, f.Content})
	}
	b, _ := json.Marshal(values)
	return string(b)
}
func (a *App) loadApprovalPolicy() error {
	b, err := os.ReadFile(filepath.Join(a.dataPath, "config", "approval-policy.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = json.Unmarshal(b, &a.approvalPolicy); err != nil {
		return err
	}
	return nil
}
func (a *App) saveApprovalPolicyLocked(p ApprovalPolicy) error {
	if err := os.MkdirAll(filepath.Join(a.dataPath, "config"), 0700); err != nil {
		return err
	}
	if err := atomicJSON(filepath.Join(a.dataPath, "config", "approval-policy.json"), p); err != nil {
		return err
	}
	a.approvalPolicy = p
	return nil
}
func (a *App) getApprovalPolicy(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	jsonOut(w, 200, map[string]any{"enabled": a.approvalPolicy.Enabled, "revision": a.approvalPolicy.Revision, "rememberedRules": len(a.approvalPolicy.Rules)})
}
func (a *App) updateApprovalPolicy(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	a.changeGlobalApprovalMode(w, in.Enabled)
}
func (a *App) broadcastApprovalPolicy() {
	a.eventMu.Lock()
	defer a.eventMu.Unlock()
	for ch := range a.globalSubs {
		select {
		case ch <- "approval:":
		default:
		}
	}
}
func (a *App) changeGlobalApprovalMode(w http.ResponseWriter, enabled bool) {
	a.mu.Lock()
	p := a.approvalPolicy
	p.Enabled = enabled
	p.Revision++
	if err := a.saveApprovalPolicyLocked(p); err != nil {
		a.mu.Unlock()
		fail(w, 500, err)
		return
	}
	var pending []*Task
	var persistenceError error
	for _, s := range a.sessions {
		if s.Deleted {
			continue
		}
		changed := false
		for _, task := range s.Runs {
			switch task.Status {
			case "running", "awaiting_clarification", "awaiting_approval", "queued", "paused":
			default:
				continue
			}
			changed = true
			task.AutoReview = enabled
			task.approvalGeneration++
			if task.approvalCancel != nil {
				task.approvalCancel()
			}
			task.ApprovalState = "manual"
			if enabled {
				task.ApprovalState = "reviewing"
				pending = append(pending, task)
			}
		}
		if changed {
			if err := a.save(s); err != nil {
				persistenceError = err
			}
		}
	}
	a.mu.Unlock()
	a.broadcastApprovalPolicy()
	a.broadcastSessionsChanged("")
	for _, task := range pending {
		go func(t *Task) { a.reviewPendingCommand(t); a.reviewPendingFiles(context.Background(), t) }(task)
	}
	if persistenceError != nil {
		fail(w, 500, errors.New("全局模式已保存并生效，但部分会话快照保存失败"))
		return
	}
	jsonOut(w, 200, map[string]any{"enabled": enabled, "revision": p.Revision, "rememberedRules": len(p.Rules)})
}
func (a *App) clearApprovalRules(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	p := a.approvalPolicy
	p.Rules = nil
	p.Revision++
	err := a.saveApprovalPolicyLocked(p)
	a.mu.Unlock()
	if err != nil {
		fail(w, 500, err)
		return
	}
	a.broadcastApprovalPolicy()
	jsonOut(w, 200, map[string]bool{"ok": true})
}
func (a *App) rememberApprovalLocked(task *Task, kind, payload string) error {
	return a.rememberApprovalUntilLocked(task, kind, payload, "")
}
func (a *App) rememberApprovalUntilLocked(task *Task, kind, payload, expiresAt string) error {
	deadline, err := validateApprovalExpiry(expiresAt)
	if err != nil {
		return err
	}
	if task.WorkspaceID == "" || task.WorkspaceID != a.wsID() {
		return errors.New("工作空间不匹配，不能记住")
	}
	if kind == "shell" && (payload == "" || approvalCommandProtected(payload) || approvalCommandCannotRemember(payload)) {
		return errors.New("该命令不能保存为长期授权，请仅批准本次")
	}
	rule := ApprovalRule{Kind: kind, Workspace: task.WorkspaceID, Root: task.AgentRoot, Fingerprint: approvalFingerprint(payload), At: time.Now().UTC().Format(time.RFC3339Nano), ExpiresAt: deadline}
	p := a.approvalPolicy
	for i, r := range p.Rules {
		if r.Kind == rule.Kind && r.Workspace == rule.Workspace && r.Root == rule.Root && r.Fingerprint == rule.Fingerprint {
			if approvalRuleActive(r, time.Now()) && r.ExpiresAt == rule.ExpiresAt {
				return nil
			}
			if approvalRuleActive(r, time.Now()) {
				rule.At = r.At
			}
			p.Rules = append([]ApprovalRule(nil), p.Rules...)
			p.Rules[i] = rule
			p.Revision++
			if err := a.saveApprovalPolicyLocked(p); err != nil {
				return err
			}
			a.broadcastApprovalPolicy()
			return nil
		}
	}
	// Never silently evict user permissions or widen matching rules.
	if len(p.Rules) >= 1000 {
		return errors.New("审批记忆已满，请在策略中清空记忆后重试")
	}
	p.Rules = append(append([]ApprovalRule(nil), p.Rules...), rule)
	p.Revision++
	if err := a.saveApprovalPolicyLocked(p); err != nil {
		return err
	}
	a.broadcastApprovalPolicy()
	return nil
}
func (a *App) rememberedApprovalLocked(task *Task, kind, payload string) bool {
	_, matched := a.matchApprovalRuleLocked(task, kind, payload)
	return matched
}
func (a *App) matchApprovalRuleLocked(task *Task, kind, payload string) (ApprovalRule, bool) {
	if task.WorkspaceID == "" || task.WorkspaceID != a.wsID() || a.settings.SandboxMode == "read-only" {
		return ApprovalRule{}, false
	}
	fingerprint := approvalFingerprint(payload)
	for _, r := range a.approvalPolicy.Rules {
		if r.Kind == kind && r.Workspace == task.WorkspaceID && r.Root == task.AgentRoot && r.Fingerprint == fingerprint && approvalRuleActive(r, time.Now()) {
			return r, true
		}
	}
	return ApprovalRule{}, false
}
func (a *App) rememberedShellApproval(task *Task, command string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if approvalCommandProtected(command) {
		return false
	}
	return a.rememberedApprovalLocked(task, "shell", command)
}

func approvalCommandProtected(command string) bool {
	_, blocked := shellBlocked(command)
	_, private := shellTouchesAssistantZone(command)
	return blocked || private
}
func (a *App) applyRememberedFiles(task *Task) bool {
	a.mu.Lock()
	s := a.approvalSessionLocked(task)
	payload := approvalFilesFingerprint(task.Files)
	matched := s != nil && !s.Deleted && task.Status == "awaiting_approval" && len(task.Files) > 0 && a.rememberedApprovalLocked(task, "files", payload)
	a.mu.Unlock()
	if !matched {
		return false
	}
	r, _ := http.NewRequest(http.MethodPost, "/", nil)
	r.SetPathValue("id", s.ID)
	r.SetPathValue("run", task.ID)
	w := &approvalApplyResult{header: make(http.Header)}
	a.applyTaskGuarded(w, r, func(t *Task) error {
		if t != task || a.sessions[s.ID] != s || s.Deleted || approvalFilesFingerprint(t.Files) != payload || !a.rememberedApprovalLocked(t, "files", payload) {
			return errors.New("审批记忆或提案环境已变化")
		}
		rule, _ := a.matchApprovalRuleLocked(t, "files", payload)
		return a.persistApprovalReceiptLocked(t, rememberedApprovalReceipt(rule, "write_file: remembered exact proposal"))
	})
	if w.status != http.StatusOK {
		a.mu.Lock()
		task.ApprovalState = "manual"
		_ = a.save(s)
		a.mu.Unlock()
	}
	a.broadcastSessionsChanged(s.ID)
	return true
}

func approvalCommandCannotRemember(command string) bool {
	for _, word := range []string{"rm", "rmdir", "unlink", "sudo", "chmod", "chown", "python", "python3", "node", "bash", "sh", "zsh"} {
		if hasShellWord(strings.ToLower(command), word) {
			return true
		}
	}
	return false
}
