package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

// Identity excludes expiry so editing validity does not change a rule's identity.
func approvalRuleID(r ApprovalRule) string {
	b, _ := json.Marshal(struct {
		Kind, Workspace, Fingerprint, At string
		Root                             AgentRoot
	}{r.Kind, r.Workspace, r.Fingerprint, r.At, r.Root})
	return approvalFingerprint(string(b))
}
func approvalRuleActive(r ApprovalRule, now time.Time) bool {
	if r.ExpiresAt == "" {
		return true
	}
	deadline, err := time.Parse(time.RFC3339Nano, r.ExpiresAt)
	return err == nil && now.Before(deadline)
}
func validateApprovalExpiry(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	deadline, err := time.Parse(time.RFC3339Nano, value)
	now := time.Now()
	if err != nil || !deadline.After(now) || deadline.After(now.AddDate(1, 0, 0)) {
		return "", errors.New("有效期须为未来一年内的时间")
	}
	return deadline.UTC().Format(time.RFC3339Nano), nil
}
func (a *App) listApprovalRules(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	rules := make([]map[string]any, 0, len(a.approvalPolicy.Rules))
	now := time.Now()
	for _, rule := range a.approvalPolicy.Rules {
		rules = append(rules, map[string]any{"id": approvalRuleID(rule), "kind": rule.Kind, "workspace": rule.Workspace, "root": rule.Root, "fingerprint": rule.Fingerprint, "createdAt": rule.At, "expiresAt": rule.ExpiresAt, "active": approvalRuleActive(rule, now)})
	}
	jsonOut(w, 200, map[string]any{"revision": a.approvalPolicy.Revision, "rules": rules})
}
func (a *App) updateApprovalRule(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Revision  *uint64 `json:"revision"`
		ExpiresAt string  `json:"expiresAt"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := decode(w, r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	if in.Revision == nil {
		fail(w, 400, errors.New("缺少审批规则版本"))
		return
	}
	if r.Method == http.MethodPut {
		var err error
		in.ExpiresAt, err = validateApprovalExpiry(in.ExpiresAt)
		if err != nil {
			fail(w, 400, err)
			return
		}
	}
	a.mu.Lock()
	p := a.approvalPolicy
	if p.Revision != *in.Revision {
		a.mu.Unlock()
		fail(w, 409, errors.New("审批规则已变化，请重新读取"))
		return
	}
	index := -1
	for i, rule := range p.Rules {
		if approvalRuleID(rule) == r.PathValue("rule") {
			index = i
			break
		}
	}
	if index < 0 {
		a.mu.Unlock()
		fail(w, 404, errors.New("审批规则不存在"))
		return
	}
	p.Rules = append([]ApprovalRule(nil), p.Rules...)
	if r.Method == http.MethodDelete {
		p.Rules = append(p.Rules[:index], p.Rules[index+1:]...)
	} else {
		p.Rules[index].ExpiresAt = in.ExpiresAt
	}
	p.Revision++
	err := a.saveApprovalPolicyLocked(p)
	a.mu.Unlock()
	if err != nil {
		fail(w, 500, err)
		return
	}
	a.broadcastApprovalPolicy()
	jsonOut(w, 200, map[string]any{"ok": true, "revision": p.Revision})
}
