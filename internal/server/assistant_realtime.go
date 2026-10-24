package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Provider credentials never leave this process. Browser authentication uses a
// WebSocket subprotocol header, never a URL parameter or provider API key.
func assistantSocketToken(r *http.Request) string {
	if !strings.HasSuffix(r.URL.Path, "/assistant-call/socket") || !websocket.IsWebSocketUpgrade(r) {
		return ""
	}
	for _, p := range websocket.Subprotocols(r) {
		if strings.HasPrefix(p, "aide-auth.") {
			return strings.TrimPrefix(p, "aide-auth.")
		}
	}
	return ""
}

func realtimeCallURL(cfg Settings) (string, error) {
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !isBailianEndpoint(cfg.BaseURL) {
		return "", errors.New("原生通话需要已配置的千问业务空间提供商")
	}
	if u.Scheme != "https" {
		return "", errors.New("原生通话需要 HTTPS 提供商地址")
	}
	if !strings.HasSuffix(strings.ToLower(u.Hostname()), ".maas.aliyuncs.com") {
		return "", errors.New("请为千问配置业务空间地址后使用原生通话")
	}
	u.Scheme = "wss"
	u.Path = "/api-ws/v1/realtime"
	u.RawQuery = url.Values{"model": {"qwen3.8-omni-flash-realtime"}}.Encode()
	return u.String(), nil
}

func (a *App) realtimeCallConfigLocked() (Settings, error) {
	cfg, err := a.xiaomiModelConfigLocked()
	if err == nil {
		if _, e := realtimeCallURL(cfg); e == nil && cfg.APIKey != "" {
			return cfg, nil
		}
	}
	for _, m := range a.settings.Models {
		c, e := settingsForSelectedModel(a.settings, m.ID)
		if e != nil {
			continue
		}
		if _, e = realtimeCallURL(c); e != nil {
			continue
		}
		c.APIKey, e = a.modelAPIKeyForConfigLocked(c)
		if e == nil && c.APIKey != "" {
			return c, nil
		}
	}
	return cfg, errors.New("请先配置千问业务空间地址和 API Key，再使用原生通话")
}

func (a *App) realtimeCallValid(id, workspace string) bool {
	a.bgMu.Lock()
	closing := a.bgClosing
	a.bgMu.Unlock()
	if closing {
		return false
	}
	if !a.isAssistantUnlocked(id) || a.readLockState().Locked {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.sessions[id]
	return s != nil && s.Kind == assistantSessionKind && a.wsID() == workspace
}

func (a *App) assistantRealtimeStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a.mu.Lock()
	workspace := a.wsID()
	a.mu.Unlock()
	if !a.realtimeCallValid(id, workspace) {
		fail(w, 423, errors.New("请先解锁助理会话"))
		return
	}
	a.mu.Lock()
	_, err := a.realtimeCallConfigLocked()
	a.mu.Unlock()
	if err != nil {
		fail(w, 400, err)
		return
	}
	jsonOut(w, 200, map[string]any{"model": "qwen3.8-omni-flash-realtime", "voice": "Tina", "transport": "native-duplex", "inputRate": 16000, "outputRate": 24000})
}

type realtimeCallDial func(context.Context, string, http.Header) (*websocket.Conn, *http.Response, error)

func (a *App) assistantRealtimeSocket(w http.ResponseWriter, r *http.Request) {
	a.assistantRealtimeSocketWithDial(w, r, func(ctx context.Context, u string, h http.Header) (*websocket.Conn, *http.Response, error) {
		return (&websocket.Dialer{HandshakeTimeout: 10 * time.Second}).DialContext(ctx, u, h)
	})
}
func (a *App) assistantRealtimeSocketWithDial(w http.ResponseWriter, r *http.Request, dial realtimeCallDial) {
	id := r.PathValue("id")
	a.mu.Lock()
	workspace := a.wsID()
	cfg, err := a.realtimeCallConfigLocked()
	a.mu.Unlock()
	if !a.realtimeCallValid(id, workspace) {
		fail(w, 423, errors.New("请先解锁助理会话"))
		return
	}
	if err != nil {
		fail(w, 400, err)
		return
	}
	a.mu.Lock()
	if a.assistantRealtimeActive == nil {
		a.assistantRealtimeActive = map[string]bool{}
	}
	if a.assistantRealtimeActive[id] {
		a.mu.Unlock()
		fail(w, 409, errors.New("已有实时通话，请先挂断"))
		return
	}
	a.assistantRealtimeActive[id] = true
	a.mu.Unlock()
	defer func() { a.mu.Lock(); delete(a.assistantRealtimeActive, id); a.mu.Unlock() }()
	upstreamURL, err := realtimeCallURL(cfg)
	if err != nil {
		fail(w, 400, err)
		return
	}
	agentBinding, agentErr := a.selectedAssistantAgent()
	if agentErr != nil {
		fail(w, 503, agentErr)
		return
	}
	nativeSession := realtimeCallSession(cfg)
	if agentBinding != nil {
		prepared, e := a.assistantAgentCall(r.Context(), *agentBinding, map[string]any{"phase": "native.begin", "now": time.Now().UTC().Format(time.RFC3339Nano), "identity": voiceIdentityPrompt(cfg)})
		if e != nil || prepared.Instructions == "" {
			fail(w, 503, errors.New("assistant agent native context unavailable"))
			return
		}
		nativeSession["instructions"] = prepared.Instructions
		nativeSession["tools"] = append(nativeSession["tools"].([]any), map[string]any{"type": "function", "name": "assistant_context", "description": "Retrieve assistant context for the current transcribed utterance when earlier experiences or preferences are relevant.", "parameters": map[string]any{"type": "object", "properties": map[string]any{}}})
	}
	upgrade := websocket.Upgrader{Subprotocols: []string{"aide-realtime"}, CheckOrigin: func(r *http.Request) bool {
		u, e := url.Parse(r.Header.Get("Origin"))
		return e == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host == r.Host
	}}
	client, err := upgrade.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancel()
	var clientMu, upMu, audioMu sync.Mutex
	emittedAudio := map[string]int{}
	emit := func(event any) error {
		clientMu.Lock()
		defer clientMu.Unlock()
		client.SetWriteDeadline(time.Now().Add(5 * time.Second))
		return client.WriteJSON(event)
	}
	report := func(msg string) { _ = emit(map[string]any{"type": "aide.error", "message": msg}) }
	upstream, resp, err := dial(ctx, upstreamURL, http.Header{"Authorization": {"Bearer " + cfg.APIKey}})
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
			resp.Body.Close()
		}
		report(fmt.Sprintf("原生实时通话连接失败（HTTP %d），请检查业务空间、模型开通和网络", status))
		return
	}
	defer upstream.Close()
	send := func(v any) error {
		upMu.Lock()
		defer upMu.Unlock()
		upstream.SetWriteDeadline(time.Now().Add(5 * time.Second))
		return upstream.WriteJSON(v)
	}
	upstream.SetReadLimit(2 << 20)
	client.SetReadLimit(96 << 10)
	upstream.SetPongHandler(func(string) error { return upstream.SetReadDeadline(time.Now().Add(60 * time.Second)) })
	// One reader per socket. Closing either side unblocks both readers.
	go func() { <-ctx.Done(); upstream.Close(); client.Close() }()
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		ticks := 0
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				ticks++
				if ticks%20 == 0 {
					_ = upstream.WriteControl(websocket.PingMessage, nil, time.Now().Add(3*time.Second))
				}
				if !a.realtimeCallValid(id, workspace) || (agentBinding != nil && !a.assistantAgentCurrent(*agentBinding)) {
					report("助理已锁定或工作区已变化，通话结束")
					cancel()
					return
				}
			}
		}
	}()
	ready := false
	// Only audio/control data from the browser. Model instructions, tool outputs,
	// histories and credential-bearing payloads are never accepted from this socket.
	go func() {
		defer cancel()
		for {
			var in struct {
				Type       string `json:"type"`
				Audio      string `json:"audio"`
				ItemID     string `json:"item_id"`
				AudioEndMS int    `json:"audio_end_ms"`
			}
			client.SetReadDeadline(time.Now().Add(45 * time.Second))
			if client.ReadJSON(&in) != nil {
				return
			}
			if !a.realtimeCallValid(id, workspace) {
				return
			}
			switch in.Type {
			case "input_audio_buffer.append":
				b, e := base64.StdEncoding.DecodeString(in.Audio)
				if e != nil || len(b) == 0 || len(b) > 16000 || len(b)%2 != 0 {
					return
				}
				if send(map[string]any{"type": in.Type, "audio": in.Audio}) != nil {
					return
				}
			case "input_audio_buffer.clear", "response.cancel":
				if send(map[string]any{"type": in.Type}) != nil {
					return
				}
			case "conversation.item.truncate":
				audioMu.Lock()
				duration, ok := emittedAudio[in.ItemID]
				audioMu.Unlock()
				if !ok || in.AudioEndMS < 0 || in.AudioEndMS > duration/48 {
					return
				}
				if send(map[string]any{"type": in.Type, "item_id": in.ItemID, "content_index": 0, "audio_end_ms": in.AudioEndMS}) != nil {
					return
				}
			case "aide.ping":
				_ = emit(map[string]any{"type": "aide.pong"})
			default:
				return
			}
		}
	}()
	// All history/tool state below belongs to this reader and this call only.
	heard := ""
	heardID := ""
	usedTurn := ""
	seen := map[string]bool{}
	toolsUsed := 0
	for {
		upstream.SetReadDeadline(time.Now().Add(60 * time.Second))
		var event map[string]json.RawMessage
		if upstream.ReadJSON(&event) != nil {
			return
		}
		var kind string
		_ = json.Unmarshal(event["type"], &kind)
		str := func(key string) string { var s string; _ = json.Unmarshal(event[key], &s); return s }
		switch kind {
		case "session.created":
			session := nativeSession
			if send(map[string]any{"type": "session.update", "session": session}) != nil {
				return
			}
		case "session.updated":
			ready = true
			_ = emit(map[string]any{"type": "aide.ready", "model": "qwen3.8-omni-flash-realtime", "voice": "Tina", "inputRate": 16000, "outputRate": 24000})
		case "error":
			// Do not forward provider bodies or URLs containing account details.
			var e struct {
				Code string `json:"code"`
			}
			_ = json.Unmarshal(event["error"], &e)
			if e.Code == "response_cancel_not_active" || e.Code == "response_cancel_not_active_error" {
				continue
			}
			report("实时音频服务返回错误（" + e.Code + "），通话已结束；未自动重复计费连接")
			return
		case "conversation.item.input_audio_transcription.completed":
			heard = str("transcript")
			heardID = str("item_id")
			a.realtimeCallRecord(id, workspace, "user", heard)
			if agentBinding != nil {
				if _, e := a.assistantAgentCall(ctx, *agentBinding, map[string]any{"phase": "native.observe", "role": "user", "text": heard, "turnId": heardID, "now": time.Now().UTC().Format(time.RFC3339Nano)}); e != nil {
					report("Assistant agent observation failed")
					return
				}
			}
			_ = emit(map[string]any{"type": kind, "transcript": heard, "item_id": heardID})
		case "response.function_call_arguments.done":
			callID := str("call_id")
			if seen[callID] {
				continue
			}
			if len(seen) >= 128 {
				report("本次通话工具轮次已达上限，请重新拨通")
				return
			}
			seen[callID] = true
			toolsUsed++
			result := "未执行：没有本轮已转写的用户原话，或该轮已经处理。"
			if ready && str("name") == "assistant_work" && heard != "" && heardID != "" && usedTurn != heardID && toolsUsed <= 32 && a.realtimeCallValid(id, workspace) {
				usedTurn = heardID
				result = a.realtimeCallWork(ctx, id, workspace, heard)
			}
			if ready && str("name") == "assistant_context" && agentBinding != nil && heard != "" && heardID != "" && toolsUsed <= 32 {
				prepared, e := a.assistantAgentCall(ctx, *agentBinding, map[string]any{"phase": "native.recall", "heard": heard, "now": time.Now().UTC().Format(time.RFC3339Nano), "identity": voiceIdentityPrompt(cfg)})
				if e != nil {
					report("Assistant agent recall failed")
					return
				}
				result = prepared.Instructions
			}
			if send(map[string]any{"type": "conversation.item.create", "item": map[string]any{"type": "function_call_output", "call_id": callID, "output": result}}) != nil {
				return
			}
			if send(map[string]any{"type": "response.create"}) != nil {
				return
			}
		case "response.audio_transcript.done":
			a.realtimeCallRecord(id, workspace, "assistant", str("transcript"))
			_ = emit(event)
		case "response.audio.delta":
			audioBytes, e := base64.StdEncoding.DecodeString(str("delta"))
			if e != nil {
				return
			}
			item := str("item_id")
			audioMu.Lock()
			if len(emittedAudio) > 128 {
				audioMu.Unlock()
				return
			}
			emittedAudio[item] += len(audioBytes)
			audioMu.Unlock()
			if emit(event) != nil {
				return
			}
		case "input_audio_buffer.speech_started":
			heard = ""
			heardID = ""
			if emit(event) != nil {
				return
			}
		case "input_audio_buffer.speech_stopped", "response.created", "response.audio.done", "response.audio_transcript.delta", "response.done", "response.output_item.added":
			if emit(event) != nil {
				return
			}
		}
	}
}

func (a *App) realtimeCallRecord(id, workspace, role, text string) {
	if text == "" || len(text) > 32000 || !a.realtimeCallValid(id, workspace) {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.sessions[id]
	if s == nil {
		return
	}
	typ := ""
	if role == "user" {
		typ = msgTypeVoiceIn
	}
	s.Messages = append(s.Messages, Message{Role: role, Content: text, Type: typ})
	s.Updated = time.Now().UTC().Format(time.RFC3339Nano)
	_ = a.save(s)
}

func (a *App) realtimeCallWork(ctx context.Context, id, workspace, heard string) string {
	if !a.realtimeCallValid(id, workspace) {
		return "未执行：助理已锁定。"
	}
	a.mu.Lock()
	cfg, err := a.xiaomiModelConfigLocked()
	a.mu.Unlock()
	if err != nil {
		return "未执行：助理模型不可用。"
	}
	dec, err := a.runAssistantAgenticLoop(ctx, cfg, heard, "", "call")
	if err != nil {
		return "未执行：助理处理失败。"
	}
	if !a.realtimeCallValid(id, workspace) || ctx.Err() != nil {
		return "未执行：通话已结束。"
	}
	if dec.Action != "dispatch" {
		return dec.Reply
	}
	a.mu.Lock()
	if a.wsID() != workspace {
		a.mu.Unlock()
		return "未执行：工作区变化。"
	}
	disp := a.dispatchToAideLocked(dec.DispatchText)
	a.mu.Unlock()
	if disp == nil {
		return "未能创建任务。"
	}
	a.startDispatchedAssistantTaskWithContext(ctx, disp, dec.DispatchText, false, heard)
	if disp["started"] == true {
		return fmt.Sprintf("已启动 aide 会话 #%v。", disp["number"])
	}
	return fmt.Sprintf("已创建会话 #%v，但没有成功启动，请在工作台查看。", disp["number"])
}

func realtimeCallSession(cfg Settings) map[string]any {
	instruction := voiceIdentityPrompt(cfg) + "\n" + assistantCallGuidance + "\n你通过原生音频实时交谈。遇到用户明确交办工作、查询任务进展或管理事项，调用 assistant_work；它将用户本轮原话交给现有助理权限链处理。工具成功前不要声称已经完成或交办。闲聊直接回答。不要自行执行工作区命令。"
	tool := map[string]any{"type": "function", "name": "assistant_work", "description": "用户明确交办任务或查询工作时，通过现有助理安全权限链处理本轮原话；闲聊不用。", "parameters": map[string]any{"type": "object", "properties": map[string]any{}}}
	session := map[string]any{"modalities": []string{"text", "audio"}, "instructions": instruction, "turn_detection": map[string]any{"type": "semantic_vad", "silence_duration_ms": 650}, "input_audio_transcription": map[string]any{"model": "gummy-realtime-v1"}, "audio": map[string]any{"input": map[string]any{"format": map[string]any{"type": "pcm", "sample_rate": 16000, "sample_format": "s16le", "channels": 1, "packing": "interleaved", "channel_layout": "mono"}}, "output": map[string]any{"voice": "Tina", "format": map[string]any{"type": "pcm", "sample_rate": 24000}}}, "tools": []any{tool}, "tool_choice": "auto"}
	return session
}
