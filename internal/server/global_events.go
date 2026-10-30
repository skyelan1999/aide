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

// beginRuntimeShutdown prioritizes the terminal privacy event over queued
// refreshes and waits a bounded time for event writers to flush it. New work is
// rejected as soon as runtimeStopping becomes true.
func (a *App) beginRuntimeShutdown() {
	if !a.runtimeStopping.CompareAndSwap(false, true) {
		return
	}
	a.eventMu.Lock()
	for ch := range a.globalSubs {
		select {
		case ch <- "runtime-stopping:":
		default:
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- "runtime-stopping:":
			default:
			}
		}
	}
	a.eventMu.Unlock()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		a.eventMu.Lock()
		remaining := len(a.globalSubs)
		a.eventMu.Unlock()
		if remaining == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
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
	if a.runtimeStopping.Load() {
		fmt.Fprint(w, "event: runtime-stopping\ndata: {\"event\":\"runtime-stopping\"}\n\n")
		flusher.Flush()
		a.eventMu.Lock()
		delete(a.globalSubs, ch)
		a.eventMu.Unlock()
		return
	}
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
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
			event := "sessions-changed"
			if parts[0] == "runtime-stopping" {
				event = "runtime-stopping"
			} else if parts[0] == "approval" {
				event = "approval-policy-changed"
			}
			data, _ := json.Marshal(map[string]any{"event": event, "sessionId": sid})
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
			flusher.Flush()
			if event == "runtime-stopping" {
				return
			}
		}
	}
}
