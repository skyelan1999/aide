package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
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
	agentBinding, agentErr := a.selectedMixSoul()
	if agentErr != nil {
		fail(w, 503, agentErr)
		return
	}
	nativeCallID := newID()
	nativeSession := realtimeCallSession(cfg)
	if agentBinding != nil {
		prepared, e := a.mixSoulCall(r.Context(), *agentBinding, map[string]any{"phase": "native.begin", "callId": nativeCallID, "now": time.Now().UTC().Format(time.RFC3339Nano), "identity": voiceIdentityPrompt(cfg)})
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
	// Ordinary socket hangup is not revocation of an already accepted utterance.
	// Every write still checks assistant unlock, vault and workspace revision.
	persist := func(input map[string]any) (mixSoulResponse, error) {
		input["callId"] = nativeCallID
		saveCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		return a.mixSoulCall(saveCtx, *agentBinding, input)
	}
	if agentBinding != nil {
		defer func() { _, _ = persist(map[string]any{"phase": "native.end"}) }()
	}
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
	// Qwen Omni documents instructions on session.update, not response.create.
	// Keep the update and trigger adjacent; a provider may silently ignore an
	// unsupported per-response instructions object rather than reject it.
	nativeInstructionOverride := false
	respondWithInstructions := func(instructions string) error {
		upMu.Lock()
		defer upMu.Unlock()
		upstream.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if e := upstream.WriteJSON(map[string]any{"type": "session.update", "session": map[string]any{"instructions": instructions}}); e != nil {
			return e
		}
		nativeInstructionOverride = true
		return upstream.WriteJSON(map[string]any{"type": "response.create"})
	}
	upstream.SetReadLimit(2 << 20)
	client.SetReadLimit(96 << 10)
	upstream.SetPongHandler(func(string) error { return upstream.SetReadDeadline(time.Now().Add(60 * time.Second)) })
	// One reader per socket. Closing either side unblocks both readers.
	go func() { <-ctx.Done(); upstream.Close(); client.Close() }()
	var nativeReady atomic.Bool
	var lastMicrophoneEnergy atomic.Int64
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

				if !a.realtimeCallValid(id, workspace) || (agentBinding != nil && !a.mixSoulCurrent(*agentBinding)) {
					report("助理已锁定或工作区已变化，通话结束")
					cancel()
					return
				}
			}
		}
	}()
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
				if nativePCMHasSpeechEnergy(b) {
					lastMicrophoneEnergy.Store(time.Now().UnixNano())
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
	nativeLearned := map[string]bool{}
	ledger := newNativeTurnLedger()
	activeResponse := ""
	toolResponsePending := false
	toolResponseProactive := false
	reviewTurns := map[string]bool{}
	quotationTurns := map[string]nativeQuotation{}
	speechRetried := map[string]bool{}
	initiativeEligible, initiativePending := false, false
	initiativeInterrupted := false
	toneRetryProactive := false
	toneRetryTurn := ""
	nativeLearningCount := 0
	lastObservedOrder := -1
	seen := map[string]bool{}
	toolsUsed := 0
	retryTone := func() bool {
		if toneRetryTurn == "" || activeResponse != "" {
			return true
		}
		turn := toneRetryTurn
		toneRetryTurn = ""
		if turn != heardID {
			return true
		}
		// Apply through the documented session configuration before triggering
		// the bounded retry. The next observed user turn restores full context.
		if toneRetryProactive {
			initiativePending = true
		}
		proactive := toneRetryProactive
		toneRetryProactive = false
		instructions := nativeCalibratedSpeechInstructions(voiceIdentityPrompt(cfg), heard)
		if q := quotationTurns[turn]; q.valid() && !proactive {
			instructions = nativeVerbatimSpeechInstructions(voiceIdentityPrompt(cfg), heard, q)
		}
		return respondWithInstructions(instructions) == nil
	}
	rejectSpeech := func(reply *nativeReplyEvidence, reason string) bool {
		if os.Getenv("AIDE_NATIVE_ACCEPTANCE_DIAGNOSTICS") == "1" {
			limit := reply.review.maxAudioBytes
			if limit <= 0 {
				limit = 480000
			}
			if reply.review.audioBytes > limit || len(reply.review.events) >= 1024 {
				reason = "buffer-limit"
			}
			nativeProviderDiagnostic(a.dataPath, "speech_review", reason, reply.text, cfg.APIKey, a.token)
		}
		reply.canceled = true
		reply.review.rejected = true
		reply.review.events = nil
		toolResponsePending = false
		if reply.turn != heardID {
			return true
		}
		if speechRetried[reply.turn] {
			if reply.quotation.valid() && reason == "quotation" {
				report("本轮未能逐字复述原文，请重试")
			} else {
				report("本轮语音表达未通过校准，请换一种说法重试")
			}
			return false
		}
		speechRetried[reply.turn] = true
		toneRetryTurn = reply.turn
		toneRetryProactive = reply.proactive
		if send(map[string]any{"type": "response.cancel"}) != nil {
			return false
		}
		return retryTone()
	}
	playbackUntil := time.Now()
	releaseSpeech := func(reply *nativeReplyEvidence) bool {
		if reply.review == nil || reply.review.rejected {
			return true
		}
		events := reply.review.events
		reply.review.events = nil
		reply.review.enabled = false
		for _, e := range events {
			var kind string
			_ = json.Unmarshal(e["type"], &kind)
			if kind == "response.audio.delta" {
				var delta, item string
				_ = json.Unmarshal(e["delta"], &delta)
				_ = json.Unmarshal(e["item_id"], &item)
				pcm, _ := base64.StdEncoding.DecodeString(delta)
				audioMu.Lock()
				emittedAudio[item] += len(pcm)
				if playbackUntil.Before(time.Now()) {
					playbackUntil = time.Now()
				}
				playbackUntil = playbackUntil.Add(time.Duration(len(pcm)) * time.Second / 48000)
				audioMu.Unlock()
			}
			if kind == "response.audio_transcript.done" {
				var text string
				_ = json.Unmarshal(e["transcript"], &text)
				a.realtimeCallRecord(id, workspace, "assistant", text)
			}
			if emit(e) != nil {
				return false
			}
		}
		return true
	}
	checkSpeech := func(reply *nativeReplyEvidence) bool {
		if reply.review == nil || !reply.review.enabled || reply.review.rejected || reply.text == "" {
			return true
		}
		affect, known := reviewTurns[reply.turn]
		if !known {
			return true
		}
		remainder := reply.text
		if reply.quotation.valid() {
			var ok bool
			remainder, ok = nativeQuotationRemainder(reply.text, reply.quotation)
			if !ok {
				return rejectSpeech(reply, "quotation")
			}
			remainder = nativeQuotationAffectRemainder(remainder, reply.quotation)
		}
		if (affect || reply.proactive || reply.quotation.valid()) && speculativeSpeechNeedsRevision(remainder) || reply.proactive && initiativeSpeechUnsafe(remainder) {
			return rejectSpeech(reply, "affect")
		}
		return releaseSpeech(reply)
	}
	initiativeCount := 0
	inputSpeaking := false
	lastConversationActivity := time.Now()
	saveReplies := func() {
		if agentBinding == nil {
			return
		}
		for _, reply := range ledger.ready() {
			if saved, e := persist(map[string]any{"phase": "native.observe", "role": "assistant", "text": reply.text, "turnId": reply.turn, "evidenceTurnIds": reply.inputs, "proactive": reply.proactive, "now": time.Now().UTC().Format(time.RFC3339Nano)}); e == nil {
				reply.saved = true
				var state struct {
					Pending bool `json:"initiativePending"`
				}
				_ = json.Unmarshal(saved.State, &state)
				initiativeEligible = state.Pending
			}
		}
	}
	// One socket reader pumps events so an idle tick never blocks incoming speech.
	events := make(chan map[string]json.RawMessage, 1)
	go func() {
		defer close(events)
		for {
			var e map[string]json.RawMessage
			upstream.SetReadDeadline(time.Now().Add(60 * time.Second))
			if upstream.ReadJSON(&e) != nil {
				return
			}
			select {
			case events <- e:
			case <-ctx.Done():
				return
			}
		}
	}()
	lastTimeRefresh := time.Now()
	initiativeTicker := time.NewTicker(time.Second)
	defer initiativeTicker.Stop()
	for {
		var event map[string]json.RawMessage
		select {
		case <-ctx.Done():
			return
		case <-initiativeTicker.C:
			if agentBinding != nil && nativeReady.Load() && nativeClockRefreshDue(time.Now(), lastTimeRefresh, time.Unix(0, lastMicrophoneEnergy.Load()), inputSpeaking, activeResponse != "" || initiativePending, playbackUntil) {
				refreshed, e := a.mixSoulCall(ctx, *agentBinding, map[string]any{"phase": "native.time", "now": time.Now().UTC().Format(time.RFC3339Nano), "identity": voiceIdentityPrompt(cfg)})
				if e != nil || refreshed.Instructions == "" {
					report("助理时间上下文更新失败，通话结束")
					return
				}
				if send(map[string]any{"type": "session.update", "session": map[string]any{"instructions": refreshed.Instructions}}) != nil {
					return
				}
				lastTimeRefresh = time.Now()
			}
			if agentBinding == nil || !nativeReady.Load() || !initiativeEligible || initiativeCount >= 3 || initiativePending || activeResponse != "" || inputSpeaking || heardID == "" || time.Since(lastConversationActivity) < 30*time.Second || time.Now().Before(playbackUntil.Add(25*time.Second)) {
				continue
			}
			initiativeEligible = false
			claim, e := a.mixSoulCall(ctx, *agentBinding, map[string]any{"phase": "initiative.claim", "channel": "native", "callId": nativeCallID, "now": time.Now().UTC().Format(time.RFC3339Nano), "identity": voiceIdentityPrompt(cfg)})
			if e != nil || claim.Instructions == "" {
				continue
			}
			select {
			case incoming, ok := <-events:
				if !ok {
					return
				}
				event = incoming
			default:
				initiativePending = true
				initiativeInterrupted = false
				initiativeCount++
				lastConversationActivity = time.Now()
				if respondWithInstructions(claim.Instructions) != nil {
					return
				}
				continue
			}
		case e, ok := <-events:
			if !ok {
				return
			}
			event = e
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
			if !nativeReady.Swap(true) {
				_ = emit(map[string]any{"type": "aide.ready", "model": "qwen3.8-omni-flash-realtime", "voice": "Tina", "inputRate": 16000, "outputRate": 24000})
			}
		case "error":
			// Do not forward provider bodies or URLs containing account details.
			var e struct {
				Code    string `json:"code"`
				Type    string `json:"type"`
				Message string `json:"message"`
			}
			_ = json.Unmarshal(event["error"], &e)
			if os.Getenv("AIDE_NATIVE_ACCEPTANCE_DIAGNOSTICS") == "1" {
				nativeProviderDiagnostic(a.dataPath, e.Type, e.Code, e.Message, cfg.APIKey, a.token)
			}
			if e.Code == "response_cancel_not_active" || e.Code == "response_cancel_not_active_error" {
				continue
			}
			// Qwen omits code for this specific VAD/tool continuation race.
			// The existing native response continues; never retry or redial here.
			if nativeResponseAlreadyActive(e.Type, e.Message) {
				continue
			}
			report("实时音频服务返回错误（" + e.Code + "），通话已结束；未自动重复计费连接")
			return
		case "conversation.item.input_audio_transcription.completed":
			heard = str("transcript")
			heardID = str("item_id")
			if heard == "" || heardID == "" {
				continue
			}
			inputOrder := -1
			for i, item := range ledger.inputs {
				if item == heardID {
					inputOrder = i
					break
				}
			}
			isolatedFragment := inputOrder < 0 || inputOrder < lastObservedOrder
			if inputOrder > lastObservedOrder {
				lastObservedOrder = inputOrder
			}
			a.realtimeCallRecord(id, workspace, "user", heard)
			if agentBinding != nil {
				if observed, e := persist(map[string]any{"phase": "native.observe", "role": "user", "text": heard, "turnId": heardID, "isolatedFragment": isolatedFragment, "now": time.Now().UTC().Format(time.RFC3339Nano), "identity": voiceIdentityPrompt(cfg)}); e != nil {
					report("Assistant agent observation failed")
					return
				} else if observed.Instructions != "" {
					var state struct {
						Context struct {
							Calibration struct {
								Review bool `json:"reviewBeforeAudio"`
							} `json:"speechCalibration"`
							Quotation nativeQuotation `json:"nativeQuotation"`
						} `json:"context"`
					}
					_ = json.Unmarshal(observed.State, &state)
					reviewTurns[heardID] = state.Context.Calibration.Review
					quotationTurns[heardID] = state.Context.Quotation
					if send(map[string]any{"type": "session.update", "session": map[string]any{"instructions": observed.Instructions}}) != nil {
						return
					}
					nativeInstructionOverride = state.Context.Quotation.valid()
				}
				if heard != "" && heardID != "" && !nativeLearned[heardID] && nativeLearningCount < 16 {
					nativeLearned[heardID] = true
					nativeLearningCount++
					binding, statement, turn := *agentBinding, heard, heardID
					a.background(func() {
						learnCtx, stop := context.WithTimeout(context.Background(), 45*time.Second)
						defer stop()
						a.learnMixSoulNative(learnCtx, binding, statement, turn)
					})
				}
			}
			ledger.transcript(heardID, heard)
			if review, known := reviewTurns[heardID]; known {
				for _, reply := range ledger.replies {
					if reply.turn != heardID || reply.review == nil || reply.review.rejected {
						continue
					}
					if !reply.proactive {
						reply.quotation = quotationTurns[heardID]
					}
					if reply.quotation.valid() {
						reply.review.maxAudioBytes = 1440000
					}
					if reply.text != "" {
						if !checkSpeech(reply) {
							return
						}
					} else if !review && !reply.quotation.valid() && !reply.proactive {
						if !releaseSpeech(reply) {
							return
						}
					}
				}
			}
			saveReplies()
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
			toolHeard, toolTurn := "", ""
			toolIsProactive := false
			responseID := str("response_id")
			if responseID == "" {
				responseID = activeResponse
			}
			if reply := ledger.replies[responseID]; reply != nil {
				toolTurn = reply.turn
				toolIsProactive = reply.proactive
				toolHeard = ledger.heard[toolTurn]
			}
			result := "未执行：没有本轮已转写的用户原话，或该轮已经处理。"
			if !toolIsProactive && nativeReady.Load() && str("name") == "assistant_work" && toolHeard != "" && toolTurn == heardID && usedTurn != toolTurn && toolsUsed <= 32 && a.realtimeCallValid(id, workspace) {
				usedTurn = toolTurn
				result = a.realtimeCallWork(ctx, id, workspace, toolHeard)
			}
			if !toolIsProactive && nativeReady.Load() && str("name") == "assistant_context" && agentBinding != nil && toolHeard != "" && toolsUsed <= 32 {
				prepared, e := a.mixSoulCall(ctx, *agentBinding, map[string]any{"phase": "native.recall", "callId": nativeCallID, "heard": toolHeard, "now": time.Now().UTC().Format(time.RFC3339Nano), "identity": voiceIdentityPrompt(cfg)})
				if e != nil {
					report("Assistant agent recall failed")
					return
				}
				result = prepared.Instructions
			}
			if send(map[string]any{"type": "conversation.item.create", "item": map[string]any{"type": "function_call_output", "call_id": callID, "output": result}}) != nil {
				return
			}
			if activeResponse != "" {
				toolResponsePending = true
				toolResponseProactive = toolIsProactive
				continue
			}
			if toolIsProactive {
				initiativePending = true
			}
			if send(map[string]any{"type": "response.create"}) != nil {
				return
			}
		case "response.audio_transcript.done":
			if reply := ledger.replies[str("response_id")]; reply != nil {
				reply.text = str("transcript")
				if reply.review != nil && reply.review.enabled {
					if reply.review.rejected {
						continue
					}
					reply.review.hold(event, 0)
					if reply.review.rejected {
						if !rejectSpeech(reply, "buffer") {
							return
						}
					} else if !checkSpeech(reply) {
						return
					}
					saveReplies()
					continue
				}
				if reply.canceled {
					continue
				}
			}
			saveReplies()
			a.realtimeCallRecord(id, workspace, "assistant", str("transcript"))
			_ = emit(event)
		case "response.audio.delta":
			audioBytes, e := base64.StdEncoding.DecodeString(str("delta"))
			if e != nil {
				return
			}
			if reply := ledger.replies[str("response_id")]; reply != nil && reply.review.hold(event, len(audioBytes)) {
				if reply.review.rejected && !reply.canceled {
					if !rejectSpeech(reply, "buffer") {
						return
					}
				}
				continue
			}
			item := str("item_id")
			audioMu.Lock()
			if len(emittedAudio) > 128 {
				audioMu.Unlock()
				return
			}
			emittedAudio[item] += len(audioBytes)
			if playbackUntil.Before(time.Now()) {
				playbackUntil = time.Now()
			}
			playbackUntil = playbackUntil.Add(time.Duration(len(audioBytes)) * time.Second / 48000)
			audioMu.Unlock()
			if emit(event) != nil {
				return
			}
		case "input_audio_buffer.speech_started":
			// A quoted replay/proactive instruction applies to one turn only.
			// Restore before the next audio-triggered response, without waiting
			// for its potentially late ASR and inheriting the old quote format.
			if nativeInstructionOverride {
				if send(map[string]any{"type": "session.update", "session": map[string]any{"instructions": nativeSession["instructions"]}}) != nil {
					return
				}
				nativeInstructionOverride = false
			}
			inputSpeaking = true
			if initiativePending {
				initiativeInterrupted = true
			}
			initiativeEligible = false
			toolResponseProactive = false
			lastConversationActivity = time.Now()
			playbackUntil = time.Now()
			heard = ""
			heardID = ""
			toolResponsePending = false
			toneRetryTurn = ""
			if reply := ledger.replies[activeResponse]; reply != nil {
				reply.canceled = true
				if reply.review != nil {
					reply.review.rejected = true
					reply.review.events = nil
				}
			}
			if emit(event) != nil {
				return
			}
		case "response.created":
			var response struct {
				ID string `json:"id"`
			}
			_ = json.Unmarshal(event["response"], &response)
			activeResponse = response.ID
			ledger.created(response.ID)
			if reply := ledger.replies[response.ID]; reply != nil {
				reply.proactive = initiativePending
				initiativePending = false
				if inputSpeaking || reply.proactive && initiativeInterrupted {
					reply.canceled = true
					_ = send(map[string]any{"type": "response.cancel"})
				}
				review, known := reviewTurns[reply.turn]
				if !reply.proactive {
					reply.quotation = quotationTurns[reply.turn]
				}
				reply.review = &nativeSpeechReview{enabled: agentBinding != nil && (reply.proactive || !known || review || reply.quotation.valid()), rejected: reply.canceled}
				if reply.quotation.valid() {
					reply.review.maxAudioBytes = 1440000
				}
				if reply.proactive {
					initiativeInterrupted = false
				}
			}
			if emit(event) != nil {
				return
			}
		case "input_audio_buffer.speech_stopped":
			inputSpeaking = false
			lastConversationActivity = time.Now()
			ledger.commit(str("item_id"))
			if emit(event) != nil {
				return
			}
		case "response.done":
			lastConversationActivity = time.Now()
			var response struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			}
			_ = json.Unmarshal(event["response"], &response)
			if reply := ledger.replies[response.ID]; reply != nil {
				reply.done = true
				reply.canceled = reply.canceled || response.Status != "" && response.Status != "completed"
			}
			if activeResponse == response.ID {
				activeResponse = ""
			}
			saveReplies()
			if !retryTone() {
				return
			}
			if toolResponsePending && activeResponse == "" {
				toolResponsePending = false
				if toolResponseProactive {
					initiativePending = true
				}
				toolResponseProactive = false
				if send(map[string]any{"type": "response.create"}) != nil {
					return
				}
			}
			if emit(event) != nil {
				return
			}
		case "response.audio.done", "response.audio_transcript.delta":
			if reply := ledger.replies[str("response_id")]; reply != nil && reply.review.hold(event, 0) {
				if reply.review.rejected && !reply.canceled {
					if !rejectSpeech(reply, "buffer") {
						return
					}
				}
				continue
			}
			if emit(event) != nil {
				return
			}
		case "response.output_item.added":
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
	session := map[string]any{"modalities": []string{"text", "audio"}, "instructions": instruction, "turn_detection": map[string]any{"type": nativeVADMode(), "silence_duration_ms": 650}, "input_audio_transcription": map[string]any{"model": "qwen3-asr-flash-realtime"}, "audio": map[string]any{"input": map[string]any{"format": map[string]any{"type": "pcm", "sample_rate": 16000, "sample_format": "s16le", "channels": 1, "packing": "interleaved", "channel_layout": "mono"}}, "output": map[string]any{"voice": "Tina", "format": map[string]any{"type": "pcm", "sample_rate": 24000}}}, "tools": []any{tool}, "tool_choice": "auto"}
	return session
}
