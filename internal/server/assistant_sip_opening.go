package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
)

// Prepare a history-guided opening before answering Qwen SIP calls. Qwen cannot
// generate a response from an empty native conversation; do not invent a user
// utterance. The selected dialogue model generates one tool-free line and the
// host gateway reads it through the configured speech engine.
func (a *App) sipIncomingOpening(w http.ResponseWriter, r *http.Request) {
	if !a.sipGatewayAuthorized(r) {
		fail(w, 403, errors.New("SIP host authentication required"))
		return
	}
	var in struct {
		CallID string `json:"callId"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	a.mu.Lock()
	c := a.assistantSIPCalls[in.CallID]
	if c == nil || c.Bound || c.CallbackID != "" || !c.Closed.IsZero() || time.Since(c.Created) > time.Minute ||
		c.WorkspaceID != a.wsID() || !a.isAssistantUnlocked(c.AssistantID) || a.loadLockStateLocked().Locked {
		a.mu.Unlock()
		fail(w, 409, errors.New("incoming call unavailable"))
		return
	}
	native, nativeErr := a.realtimeCallConfigLocked()
	if nativeErr == nil && openAINativeCall(native) {
		a.mu.Unlock()
		jsonOut(w, 200, map[string]string{"text": ""})
		return
	}
	if c.OpeningPreparing {
		a.mu.Unlock()
		fail(w, 409, errors.New("opening already preparing"))
		return
	}
	if c.OpeningText != "" {
		text := c.OpeningText
		a.mu.Unlock()
		jsonOut(w, 200, map[string]string{"text": text})
		return
	}
	cfg, err := a.xiaomiModelConfigLocked()
	id, workspace := c.AssistantID, c.WorkspaceID
	if err == nil && nativeErr == nil {
		c.OpeningPreparing = true
	}
	a.mu.Unlock()
	if err != nil || nativeErr != nil {
		fail(w, 409, errors.New("call models unavailable"))
		return
	}
	defer func() { a.mu.Lock(); c.OpeningPreparing = false; a.mu.Unlock() }()
	instructions := a.nativeOpeningInstructions(id, workspace)
	if instructions == "" {
		fail(w, 423, errors.New("assistant locked"))
		return
	}
	ctx, stop := context.WithTimeout(r.Context(), 12*time.Second)
	defer stop()
	text, err := nativeIncomingOpeningText(ctx, cfg, instructions)
	if err != nil {
		fail(w, 502, errors.New("opening preparation failed"))
		return
	}
	a.mu.Lock()
	valid := a.assistantSIPCalls[in.CallID] == c && c.Closed.IsZero() && !c.Bound && c.WorkspaceID == a.wsID() &&
		a.isAssistantUnlocked(id) && !a.loadLockStateLocked().Locked
	if valid {
		c.OpeningText = text
	}
	a.mu.Unlock()
	if !valid {
		fail(w, 423, errors.New("call scope changed"))
		return
	}
	jsonOut(w, 200, map[string]string{"text": text})
}

func nativeIncomingOpeningText(ctx context.Context, cfg Settings, instructions string) (string, error) {
	text, calls, _, err := complete(ctx, cfg, []Message{{Role: "system", Content: instructions},
		{Role: "user", Content: "请输出此刻接通电话时的一句引导话。只输出要说的话。"}}, ProfileParams{MaxTokens: 100, Temperature: fp(.4)}, nil, nil)
	text = strings.Trim(strings.TrimSpace(text), "\"“”")
	if err != nil || len(calls) != 0 || text == "" || len([]rune(text)) > 60 || strings.ContainsAny(text, "\n\r") ||
		strings.Contains(text, "我是") || strings.Contains(text, "审批") || initiativeSpeechUnsafe(text) || speculativeSpeechNeedsRevision(text) {
		return "", errors.New("invalid phone opening")
	}
	return text, nil
}
