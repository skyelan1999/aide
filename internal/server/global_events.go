package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ── 全局 SSE：会话列表变更广播（#60）─────────────────────────────────────────
//
// 前端开一条 GET /api/events 长连接；后端在会话创建/删除/归档/置顶/标题或状态变更时
// 广播 sessions-changed 事件。前端据此自动 loadSessions（带 300ms 节流、保留折叠态、
// 输入聚焦时延迟刷新）。纯内存、单用户本地场景；断开即重连。

// broadcastSessionsChanged 向所有全局 SSE 订阅者广播一次会话列表变更。
// sessionID 为空表示全量刷新；非空时前端可做增量更新。仅取 eventMu，不持 a.mu。
func (a *App) broadcastSessionsChanged(sessionID string) {
	a.eventMu.Lock()
	defer a.eventMu.Unlock()
	if len(a.globalSubs) == 0 {
		return
	}
	msg := "sessions:" + sessionID
	for ch := range a.globalSubs {
		select {
		case ch <- msg:
		default: // 慢订阅者丢弃（最终由轮询/下次事件兜底）
		}
	}
}

// globalEvents GET /api/events：全局事件 SSE。
func (a *App) globalEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		fail(w, 500, errors.New("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)

	ch := make(chan string, 32)
	a.eventMu.Lock()
	if a.globalSubs == nil {
		a.globalSubs = map[chan string]struct{}{}
	}
	a.globalSubs[ch] = struct{}{}
	a.eventMu.Unlock()
	defer func() {
		a.eventMu.Lock()
		delete(a.globalSubs, ch)
		a.eventMu.Unlock()
	}()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()
		case msg := <-ch:
			parts := strings.SplitN(msg, ":", 2)
			sid := ""
			if len(parts) == 2 {
				sid = parts[1]
			}
			data, _ := json.Marshal(map[string]any{"event": "sessions-changed", "sessionId": sid})
			fmt.Fprintf(w, "event: sessions-changed\ndata: %s\n\n", data)
			flusher.Flush()
		}
	}
}
