package server

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// 小蜜模型配置独立于工作台默认模型；凭据单独保存在加密 vault 条目。
func (a *App) xiaomiModelSettings(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	source := a.settings.XiaomiModelSource
	if source == "" {
		source = "inherit"
	}
	jsonOut(w, 200, map[string]any{"source": source, "baseURL": a.settings.XiaomiBaseURL, "model": a.settings.XiaomiModel, "hasKey": a.vault != nil && a.vault.Has(VaultIDXiaomiModelAPIKey)})
}

func (a *App) updateXiaomiModelSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Source   string `json:"source"`
		BaseURL  string `json:"baseURL"`
		Model    string `json:"model"`
		APIKey   string `json:"apiKey"`
		ClearKey bool   `json:"clearKey"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	in.Source = strings.TrimSpace(in.Source)
	if in.Source != "inherit" && in.Source != "custom" {
		fail(w, 400, errors.New("模型来源必须是 inherit 或 custom"))
		return
	}
	in.BaseURL = strings.TrimSpace(in.BaseURL)
	in.Model = strings.TrimSpace(in.Model)
	if in.Source == "custom" {
		u, err := url.Parse(in.BaseURL)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			fail(w, 400, errors.New("请输入有效的 HTTP(S) API Base URL"))
			return
		}
		if in.Model == "" || len(in.Model) > 128 {
			fail(w, 400, errors.New("自定义模型名称不能为空且最长 128 个字符"))
			return
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if strings.TrimSpace(in.APIKey) != "" {
		if !a.vaultIsUnlocked() {
			fail(w, 400, errVaultLocked)
			return
		}
		if err := a.vault.Put(VaultIDXiaomiModelAPIKey, VaultTypeModelAPIKey, "小秘模型 API Key", []byte(strings.TrimSpace(in.APIKey)), ""); err != nil {
			fail(w, 500, err)
			return
		}
		if err := a.vault.Save(); err != nil {
			fail(w, 500, err)
			return
		}
	} else if in.ClearKey && a.vault != nil {
		a.vault.Delete(VaultIDXiaomiModelAPIKey)
		if err := a.vault.Save(); err != nil {
			fail(w, 500, err)
			return
		}
	}
	a.settings.XiaomiModelSource = in.Source
	a.settings.XiaomiBaseURL = in.BaseURL
	a.settings.XiaomiModel = in.Model
	if err := atomicJSON(SettingsPath(a.dataPath), a.settings); err != nil {
		fail(w, 500, err)
		return
	}
	jsonOut(w, 200, map[string]any{"source": in.Source, "baseURL": in.BaseURL, "model": in.Model, "hasKey": a.vault != nil && a.vault.Has(VaultIDXiaomiModelAPIKey)})
}

func (a *App) xiaomiModelConfigLocked() (Settings, error) {
	cfg := a.settings
	if cfg.XiaomiModelSource != "custom" {
		return cfg, nil
	}
	cfg.BaseURL, cfg.Model, cfg.ActiveModel = cfg.XiaomiBaseURL, cfg.XiaomiModel, cfg.XiaomiModel
	cfg.Models = []ModelRef{{ID: cfg.XiaomiModel, Name: cfg.XiaomiModel, ContextWindow: defaultContextWindow}}
	cfg.APIKey = ""
	if a.vault != nil && a.vault.Has(VaultIDXiaomiModelAPIKey) {
		if !a.vaultIsUnlocked() {
			return cfg, errVaultLocked
		}
		key, err := a.vault.Get(VaultIDXiaomiModelAPIKey)
		if err != nil {
			return cfg, err
		}
		cfg.APIKey = string(key)
	}
	return cfg, nil
}

// ── #62：小蜜历史归位 + 文字=语音 + agentic 自主决策 ──────────────────────────
// 小秘语音/文字往来统一写进 assistant 会话 messages；文字与语音都走小秘 agentic
// 自主决策管线（assistant_agent.go）：小秘自己用工具决定 陪聊/转交 aide/静默/追问，
// 不再由后端写死 analyze 的 send/ignore/standby 分支。analyze 保留为兜底能力。

// 消息类型（Message.Type）取值，前端据此区分渲染：
const (
	msgTypeVoiceIn   = "voice-in"   // 用户听到的原话
	msgTypeVoiceNote = "voice-note" // 小蜜的甄别结论/转交说明
	msgTypeVoiceAsk  = "voice-ask"  // 小蜜的追问
	msgTypeTextIn    = "text-in"    // 小蜜会话视图里用户手敲的文字
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

// recentAssistantConversation 提供小秘当前系统会话的短期上下文。它只把已有会话
// 消息临时带入本次模型请求，不写入长期记忆；跨会话内容仍需显式调用历史检索工具。
func (a *App) recentAssistantConversation() []Message {
	const maxMessages = 16
	const maxRunes = 8000

	a.mu.Lock()
	history := a.xiaomiHistoryLocked()
	a.mu.Unlock()

	selected := make([]Message, 0, maxMessages)
	used := 0
	for i := len(history) - 1; i >= 0 && len(selected) < maxMessages; i-- {
		msg := history[i]
		if msg.Role != "user" && msg.Role != "assistant" {
			continue
		}
		content := []rune(strings.TrimSpace(msg.Content))
		if len(content) == 0 || used >= maxRunes {
			continue
		}
		remaining := maxRunes - used
		if len(content) > remaining {
			content = content[len(content)-remaining:]
		}
		msg.Content = string(content)
		msg.ToolCalls = nil
		msg.ToolCallID = ""
		selected = append(selected, msg)
		used += len(content)
	}
	for left, right := 0, len(selected)-1; left < right; left, right = left+1, right-1 {
		selected[left], selected[right] = selected[right], selected[left]
	}
	return selected
}

// assistantMessageHandler POST /api/sessions/{id}/assistant-message
// 小秘系统会话视图的文字输入：直接按发给小秘的消息处理；不做语音环境过滤。
// 入参 {text, context?}；返回小秘决策 + 回复文本 +（dispatch 时）转交的 aide 会话信息。
// dispatch 由后端直接新建 aide 会话承接；chat/ask/silent 只留在小秘会话。
func (a *App) assistantMessageHandler(w http.ResponseWriter, r *http.Request) {
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
	// 在锁内取 *Session 指针副本，避免与 dispatchToAideLocked 并发写 a.sessions 触发 map 竞争。
	a.mu.Lock()
	s := a.sessions[r.PathValue("id")]
	cfg, cfgErr := a.xiaomiModelConfigLocked()
	va := a.voiceAgent
	a.mu.Unlock()
	if s == nil {
		fail(w, 404, errors.New("会话不存在"))
		return
	}
	if s.Kind != assistantSessionKind {
		fail(w, 400, errors.New("不是小秘系统会话"))
		return
	}
	if cfgErr != nil {
		fail(w, 423, cfgErr)
		return
	}

	runFallback := func(reason string) {
		a.mu.Lock()
		var entry VoiceHistoryEntry
		if va != nil {
			entry = va.recordFallback(text, reason)
		} else {
			entry = VoiceHistoryEntry{Time: time.Now().Format("2006-01-02 15:04:05"), Heard: text, Text: text, Action: "send", Mode: "queue", Reason: reason}
		}
		a.recordAssistantExchangeLocked(text, entry, "text")
		disp := a.dispatchToAideLocked(entry.Text)
		a.mu.Unlock()
		jsonOut(w, 200, map[string]any{
			"action": entry.Action, "text": entry.Text, "reply": describeVoiceEntry(entry),
			"dispatched": disp, "reason": entry.Reason,
		})
	}

	if cfg.BaseURL == "" || cfg.Model == "" || va == nil {
		runFallback("小秘模型未配置，原文已转交 aide")
		return
	}
	// #62 修复：与 unlockAssistantSession 同一把锁（a.assistantUnlocked），不再误用 voice-history
	// 的加密锁定（va.encStatus）——后者仅用于设置页历史查看，两者独立。
	if !a.isAssistantUnlocked(s.ID) {
		jsonOut(w, 200, map[string]any{"action": "locked", "reply": "", "reason": "小秘已锁定，请在小秘会话中解锁"})
		return
	}
	// 注入模型 API Key（与 execute 一致），跑 agentic 自主决策循环
	if cfg.XiaomiModelSource != "custom" {
		cfg.APIKey, _ = a.modelAPIKeyLocked()
	}
	dec, err := a.runAssistantAgenticLoop(r.Context(), cfg, text, in.Context, "text")
	if err != nil {
		// 键盘输入绝不回落到语音 analyze 过滤器；模型不可用时按明确文字指令兜底转交。
		detail := strings.ReplaceAll(err.Error(), cfg.BaseURL, "<model-endpoint>")
		log.Printf("小秘文字决策失败 session=%s model=%s: %s", s.ID, cfg.Model, detail)
		runFallback("小秘模型调用失败，原文已转交 aide")
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
		Runs:     []*Task{}, PendingPrompt: intent,
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
