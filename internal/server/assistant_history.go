package server

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ── #62：小蜜历史归位 + 文字=语音 + agentic 自主决策 ──────────────────────────
// 小秘语音/文字往来统一写进 assistant 会话 messages；文字与语音都走小秘 agentic
// 自主决策管线（assistant_agent.go）：小秘自己用工具决定 陪聊/转交 aide/静默/追问，
// 不再由后端写死 analyze 的 send/ignore/standby 分支。analyze 保留为兜底能力。

// 消息类型（Message.Type）取值，前端据此区分渲染：
const (
	msgTypeVoiceIn   = "voice-in"  // 用户听到的原话
	msgTypeVoiceNote = "voice-note" // 小蜜的甄别结论/转交说明
	msgTypeVoiceAsk  = "voice-ask"  // 小蜜的追问
	msgTypeTextIn    = "text-in"   // 小蜜会话视图里用户手敲的文字
)

// recordAssistantExchangeLocked 把一次小秘交互（用户听到/输入的原话 + 小秘决策）
// 追加到小秘系统会话的 messages，并落盘。调用方必须持有 a.mu。
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
	// 2) 小秘的决策/说明
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

// describeVoiceEntry 把一条小秘决策翻译成给用户看的一句话说明。
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

// recordAgenticExchangeLocked 把一次小秘 agentic 交互落进 assistant 会话时间线。
// source="voice"|"text"。dispatch 的实际派发给 aide 由调用方另做（这里只记录往来）。
// 调用方持 a.mu。
func (a *App) recordAgenticExchangeLocked(heard string, dec assistantDecision, source string) {
	s := a.findAssistantSessionLocked()
	if s == nil {
		return
	}
	inType := msgTypeTextIn
	if source == "voice" {
		inType = msgTypeVoiceIn
	}
	s.Messages = append(s.Messages, Message{
		Role: "user", Content: strings.TrimSpace(heard), Type: inType,
	})
	// 小秘回复：陪聊=正文气泡；dispatch/ask/silent 沿用既有 voice-note/voice-ask 样式。
	outType := msgTypeVoiceNote
	outContent := dec.Reply
	switch dec.Action {
	case "dispatch":
		outContent = "已转交 aide：" + dec.DispatchText
		if strings.TrimSpace(dec.Reason) != "" {
			outContent += "（" + dec.Reason + "）"
		}
	case "ask":
		outType = msgTypeVoiceAsk
		outContent = dec.Ask
	case "silent":
		outContent = "（背景声/与他人对话，已静默）"
		if strings.TrimSpace(dec.Reason) != "" {
			outContent += "：" + dec.Reason
		}
	case "chat":
		outType = "" // 普通聊天气泡
		if outContent == "" {
			outContent = "我在。"
		}
	}
	s.Messages = append(s.Messages, Message{
		Role: "assistant", Content: outContent, Type: outType,
	})
	s.Updated = time.Now().UTC().Format(time.RFC3339Nano)
	_ = a.save(s)
}

// xiaomiHistoryLocked 返回小秘专属消息，加上 #62 之前尚未迁入会话的语音历史。
// 旧语音历史受 VoiceAgent 自身加密锁控制；锁定时 snapshotHistory 为空，不绕过该锁。
// 调用方持有 a.mu。
func (a *App) xiaomiHistoryLocked() []Message {
	s := a.findAssistantSessionLocked()
	if s == nil {
		return nil
	}
	current := append([]Message(nil), s.Messages...)
	if a.voiceAgent == nil {
		return current
	}
	history := a.voiceAgent.snapshotHistory()
	if len(history) == 0 {
		return current
	}

	// #62 之后语音对话同时写入 voice-history 与 assistant session；按原话出现次数
	// 抵消重叠记录，避免重复。相同原话多次出现时按次数匹配，不丢弃重复轮次。
	seen := make(map[string]int)
	for _, msg := range current {
		if msg.Type == msgTypeVoiceIn {
			seen[strings.TrimSpace(msg.Content)]++
		}
	}
	legacy := make([]Message, 0)
	for _, entry := range history { // voice-history 保持旧到新的存储顺序
		heard := strings.TrimSpace(entry.Heard)
		if heard == "" {
			continue
		}
		if seen[heard] > 0 {
			seen[heard]--
			continue
		}
		legacy = append(legacy, Message{Role: "user", Content: heard, Type: msgTypeVoiceIn})
		noteType := msgTypeVoiceNote
		if entry.Action == "ask" {
			noteType = msgTypeVoiceAsk
		}
		legacy = append(legacy, Message{Role: "assistant", Content: describeVoiceEntry(entry), Type: noteType})
	}
	return append(legacy, current...)
}

// assistantMessageHandler POST /api/sessions/{id}/assistant-message
// 小秘系统会话视图的文字输入：走小秘 agentic 自主决策管线（#62 升级）。
// 入参 {text, context?}；返回小秘决策 + 回复文本 +（dispatch 时）转交的 aide 会话信息。
// dispatch 由后端直接新建 aide 会话承接；chat/ask/silent 只留在小秘会话。
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
	// #62 修复：与 unlockAssistantSession 同一把锁（a.assistantUnlocked），不再误用 voice-history
	// 的加密锁定（va.encStatus）——后者仅用于设置页历史查看，两者独立。
	if !a.isAssistantUnlocked(s.ID) {
		jsonOut(w, 200, map[string]any{"action": "locked", "reply": "", "reason": "小秘已锁定，请在小秘会话中解锁"})
		return
	}
	// 注入模型 API Key（与 execute 一致），跑 agentic 自主决策循环
	cfg.APIKey, _ = a.modelAPIKeyLocked()
	dec, err := a.runAssistantAgenticLoop(r.Context(), cfg, text, in.Context)
	if err != nil {
		// 兜底：agentic 循环失败则回落到既有 analyze 甄别（保留该能力不删）
		entry, aerr := va.analyze(r.Context(), cfg, text, in.Context)
		if aerr != nil {
			runFallback()
			return
		}
		if entry.Action == "send" && strings.TrimSpace(entry.Text) == "" {
			entry.Text = text
		}
		var disp map[string]any
		a.mu.Lock()
		a.recordAssistantExchangeLocked(text, entry, "text")
		if entry.Action == "send" {
			disp = a.dispatchToAideLocked(entry.Text)
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
			"mode": entry.Mode, "reason": entry.Reason, "reply": describeVoiceEntry(entry),
			"dispatched": disp,
		})
		return
	}

	var disp map[string]any
	a.mu.Lock()
	a.recordAgenticExchangeLocked(text, dec, "text")
	if dec.Action == "dispatch" {
		disp = a.dispatchToAideLocked(dec.DispatchText)
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
	replyOut := dec.Reply
	if dec.Action == "dispatch" {
		replyOut = "好，我已经把这件事交给 aide 了。"
		if n, ok := disp["number"]; ok {
			replyOut = fmt.Sprintf("好，我已经把这件事交给 aide 在 #%v 那边做了，我帮你盯着。", n)
		}
	}
	jsonOut(w, 200, map[string]any{
		"action": dec.Action, "text": dec.DispatchText, "ask": dec.Ask,
		"mode": dec.Mode, "reason": dec.Reason, "reply": replyOut,
		"dispatched": disp, "toolsUsed": dec.ToolsUsed,
	})
}

// dispatchToAideLocked 小秘把一条意图转交给 aide：新建一个普通会话承接并落盘。
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
