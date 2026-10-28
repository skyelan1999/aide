package server

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Page-owned POST, never a model call from passive watch/clock polling. The
// encrypted agent atomically spends a grounded candidate before any generation.
func initiativeSpeechUnsafe(text string) bool {
	return speculativeSpeechNeedsRevision(text) || regexp.MustCompile(`(?:已经|已|刚刚|正在|替你|帮你).{0,6}(?:删除|清理|派发|交办|发送|部署|发布|提交|转账|购买|完成)|一直在想|后台.*(?:做|处理)`).MatchString(text)
}
func (a *App) initiativeIdleCurrent(b mixSoulBinding) bool {
	if !a.mixSoulCurrent(b) {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return !a.assistantRealtimeActive[b.Session]
}
func (a *App) assistantInitiative(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a.mu.Lock()
	workspace := a.wsID()
	s := a.sessions[id]
	active := a.assistantRealtimeActive[id]
	cfg, cfgErr := a.xiaomiModelConfigLocked()
	a.mu.Unlock()
	if s == nil || s.Kind != assistantSessionKind {
		fail(w, 404, http.ErrMissingFile)
		return
	}
	if !a.realtimeCallValid(id, workspace) {
		jsonOut(w, 423, map[string]any{"error": "请先解锁助理"})
		return
	}
	b, err := a.selectedMixSoul()
	if err != nil || b == nil || active || cfgErr != nil {
		jsonOut(w, 200, map[string]any{"reply": ""})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	claim, err := a.mixSoulCall(ctx, *b, map[string]any{"phase": "initiative.claim", "channel": "idle", "now": time.Now().UTC().Format(time.RFC3339Nano), "identity": voiceIdentityPrompt(cfg)})
	if err != nil || claim.Action != "generate" {
		jsonOut(w, 200, map[string]any{"reply": ""})
		return
	}
	var state struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(claim.State, &state) != nil || state.ID == "" || len(claim.Tools) > 0 {
		jsonOut(w, 200, map[string]any{"reply": ""})
		return
	}
	text, calls, _, err := complete(ctx, cfg, claim.Messages, ProfileParams{MaxTokens: 180, Temperature: fp(.4)}, nil, nil)
	if err != nil || len(calls) > 0 || !a.initiativeIdleCurrent(*b) || ctx.Err() != nil {
		jsonOut(w, 200, map[string]any{"reply": ""})
		return
	}
	// Do not turn silence into permission to dispatch work or assert a mood.
	var proposal struct {
		Speak bool   `json:"speak"`
		Reply string `json:"reply"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(text)), &proposal) != nil || !proposal.Speak || initiativeSpeechUnsafe(proposal.Reply) {
		text = `{"speak":false}`
	}
	result, err := a.mixSoulCall(ctx, *b, map[string]any{"phase": "initiative.complete", "id": state.ID, "modelText": text, "now": time.Now().UTC().Format(time.RFC3339Nano)})
	if err != nil || ctx.Err() != nil || !a.initiativeIdleCurrent(*b) {
		jsonOut(w, 200, map[string]any{"reply": ""})
		return
	}
	if result.Reply != "" {
		a.realtimeCallRecord(id, workspace, "assistant", result.Reply)
	}
	jsonOut(w, 200, map[string]any{"reply": result.Reply, "proactive": true})
}
