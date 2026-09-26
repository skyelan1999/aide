package server

import (
	"errors"
	"net/http"
	"strings"
	"time"
)

// ── #62：小蜜历史归位 + 文字=语音 ─────────────────────────────────────────────
// 此前小蜜语音往来只存在 voice-history.json（独立加密信封），assistant 会话是空的；
// 设置页另起一个时间线。这里把语音/文字往来统一写进 assistant 会话的 messages，
// 让小蜜会话时间线本身就是历史；并新增端点让小蜜会话视图的文字输入走与语音相同的
// analyze 管线（理解意图→甄别/总结→转交 aide 或追问/回应）。

// 消息类型（Message.Type）取值，前端据此区分渲染：
const (
	msgTypeVoiceIn   = "voice-in"  // 用户听到的原话
	msgTypeVoiceNote = "voice-note" // 小蜜的甄别结论/转交说明
	msgTypeVoiceAsk  = "voice-ask"  // 小蜜的追问
	msgTypeTextIn    = "text-in"   // 小蜜会话视图里用户手敲的文字（同样过 analyze）
)

// recordAssistantExchangeLocked 把一次小蜜交互（用户听到/输入的原话 + 小蜜决策）
// 追加到小蜜系统会话的 messages，并落盘。调用方必须持有 a.mu。
// source 标记来源："voice" 或 "text"。不触发任何向 aide 的派发（派发由调用方/前端负责）。
func (a *App) recordAssistantExchangeLocked(heard string, entry VoiceHistoryEntry, source string) {
	s := a.findAssistantSessionLocked()
	if s == nil {
		return
	}
	inType := msgTypeVoiceIn
	if source == "text" {
		inType = msgTypeTextIn
	}
	// 1) 用户原话
	s.Messages = append(s.Messages, Message{
		Role:    "user",
		Content: strings.TrimSpace(heard),
		Type:    inType,
	})
	// 2) 小蜜的决策/说明
	note := describeVoiceEntry(entry)
	noteType := msgTypeVoiceNote
	if entry.Action == "ask" {
		noteType = msgTypeVoiceAsk
	}
	s.Messages = append(s.Messages, Message{
		Role:    "assistant",
		Content: note,
		Type:    noteType,
	})
	s.Updated = time.Now().UTC().Format(time.RFC3339Nano)
	_ = a.save(s)
}

// describeVoiceEntry 把一条小蜜决策翻译成给用户看的一句话说明。
func describeVoiceEntry(e VoiceHistoryEntry) string {
	switch e.Action {
	case "send":
		mode := "排队"
		if e.Mode == "insert" {
			mode = "插队"
		}
		reason := ""
		if strings.TrimSpace(e.Reason) != "" {
			reason = "（" + e.Reason + "）"
		}
		return "已理解为意图并转交 aide（" + mode + "）：" + e.Text + reason
	case "ask":
		if strings.TrimSpace(e.Ask) != "" {
			return e.Ask
		}
		return "我没太听清，能再说一遍你想做什么吗？"
	case "standby":
		return "我先退下了，需要时叫我。"
	default: // ignore
		if strings.TrimSpace(e.Reason) != "" {
			return "（背景声，已忽略：" + e.Reason + "）"
		}
		return "（已忽略）"
	}
}

// assistantMessageHandler POST /api/sessions/{id}/assistant-message
// 小蜜系统会话视图的文字输入：走与语音完全相同的 analyze 管线。
// 入参 {text, context?}；返回小蜜决策 + 回复文本 +（send 时）转交的 aide 会话信息。
// 与语音不同：send 时由后端直接新建 aide 会话承接（小蜜控制其他对话）。
func (a *App) assistantMessageHandler(w http.ResponseWriter, r *http.Request) {
	s := a.sessions[r.PathValue("id")]
	if s == nil {
		fail(w, 404, errors.New("会话不存在"))
		return
	}
	if s.Kind != assistantSessionKind {
		fail(w, 400, errors.New("不是小秘系统会话"))
		return
	}
	var in struct {
		Text    string `json:"text"`
		Context string `json:"context"`
	}
	if err := decode(w, r, &in); err != nil {
		return
	}
	text := strings.TrimSpace(in.Text)
	if text == "" {
		jsonOut(w, 200, map[string]any{"action": "ignore", "reply": "", "reason": "空文本"})
		return
	}
	a.mu.Lock()
	cfg := a.settings
	va := a.voiceAgent
	a.mu.Unlock()

	runFallback := func() {
		a.mu.Lock()
		entry := va.recordFallback(text, "小蜜未就绪，直接转交")
		a.recordAssistantExchangeLocked(text, entry, "text")
		disp := a.dispatchToAideLocked(entry.Text)
		a.mu.Unlock()
		jsonOut(w, 200, map[string]any{
			"action": entry.Action, "text": entry.Text, "reply": describeVoiceEntry(entry),
			"dispatched": disp, "reason": entry.Reason,
		})
	}

	if cfg.BaseURL == "" || cfg.Model == "" || va == nil {
		runFallback()
		return
	}
	// 历史加密锁定：不持有明文，不记录也不派发
	if st := va.encStatus(); func() bool { e, _ := st["encrypted"].(bool); u, _ := st["unlocked"].(bool); return e && !u }() {
		jsonOut(w, 200, map[string]any{"action": "locked", "reply": "", "reason": "小蜜对话历史已锁定，请先在设置中解锁后再发言"})
		return
	}
	entry, err := va.analyze(r.Context(), cfg, text, in.Context)
	if err != nil {
		runFallback()
		return
	}
	if entry.Action == "send" && strings.TrimSpace(entry.Text) == "" {
		entry.Text = text
	}
	var disp map[string]any
	reply := describeVoiceEntry(entry)
	a.mu.Lock()
	a.recordAssistantExchangeLocked(text, entry, "text")
	if entry.Action == "send" {
		disp = a.dispatchToAideLocked(entry.Text)
		// 小蜜有效交互计数（与语音一致，触发性格演化）
		fire, trigger := a.onPersonalityInteractLocked(personaXiaomi)
		var sample string
		if fire {
			sample = a.personalitySampleLocked(personaXiaomi)
		}
		a.mu.Unlock()
		if fire {
			go a.runAutoEvolve(personaXiaomi, modeRefine, trigger, sample)
		}
	} else {
		a.mu.Unlock()
	}
	jsonOut(w, 200, map[string]any{
		"action": entry.Action, "text": entry.Text, "ask": entry.Ask,
		"mode": entry.Mode, "reason": entry.Reason, "reply": reply,
		"dispatched": disp,
	})
}

// dispatchToAideLocked 小蜜把一条意图转交给 aide：新建一个普通会话承接并落盘。
// 返回新会话的 #编号/ID/标题。调用方持 a.mu。
func (a *App) dispatchToAideLocked(intent string) map[string]any {
	intent = strings.TrimSpace(intent)
	if intent == "" {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	titleRunes := []rune(intent)
	if len(titleRunes) > 24 {
		titleRunes = titleRunes[:24]
	}
	target := &Session{
		ID: newID(), Title: string(titleRunes), Created: now, Updated: now,
		Messages: []Message{{Role: "user", Content: intent}},
		Runs:     []*Task{},
	}
	a.assignSessionNumber(target)
	a.sessions[target.ID] = target
	_ = a.save(target)
	a.broadcastSessionsChanged(target.ID)
	return map[string]any{
		"sessionId": target.ID,
		"number":    target.Number,
		"title":     target.Title,
	}
}
