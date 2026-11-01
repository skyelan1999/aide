package server

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"net/http"
	"runtime/debug"
	"sync/atomic"
	"time"
)

const serverPanicCode = "AIDE-SERVER-PANIC"

// Track only response commitment. Do not buffer a stream or replay a request.
type crashResponse struct {
	app        *App
	request    *http.Request
	started    time.Time
	owner      bool
	incidentID string
	http.ResponseWriter
	committed atomic.Bool
	hijacked  net.Conn
}

func (w *crashResponse) markIncidentAuthenticated() { w.owner = true }
func (w *crashResponse) recordFailure(code int, err error) string {
	if w.app == nil || !w.owner {
		return ""
	}
	if w.incidentID != "" {
		return w.incidentID
	}
	w.incidentID = w.app.recordIncident(w.request.Context(), incident{Module: incidentModule(w.request.URL.Path), Phase: "http", Status: code}, err, w.started)
	return w.incidentID
}

func (w *crashResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *crashResponse) WriteHeader(code int) {
	if code >= 500 && w.incidentID == "" {
		if id := w.recordFailure(code, fmt.Errorf("HTTP status %d", code)); id != "" {
			w.Header().Set("X-AIDE-Incident-ID", id)
		}
	}
	if code >= 200 || code == http.StatusSwitchingProtocols {
		w.committed.Store(true)
	}
	w.ResponseWriter.WriteHeader(code)
}
func (w *crashResponse) Write(data []byte) (int, error) {
	w.committed.Store(true)
	return w.ResponseWriter.Write(data)
}
func (w *crashResponse) flush() {
	w.committed.Store(true)
	w.ResponseWriter.(http.Flusher).Flush()
}
func (w *crashResponse) hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := w.ResponseWriter.(http.Hijacker).Hijack()
	if err == nil {
		w.hijacked = conn
	}
	return conn, rw, err
}

type crashFlush struct{ *crashResponse }

func (w crashFlush) Flush() { w.flush() }

type crashHijack struct{ *crashResponse }

func (w crashHijack) Hijack() (net.Conn, *bufio.ReadWriter, error) { return w.hijack() }

type crashFlushHijack struct{ *crashResponse }

func (w crashFlushHijack) Flush()                                       { w.flush() }
func (w crashFlushHijack) Hijack() (net.Conn, *bufio.ReadWriter, error) { return w.hijack() }

func crashIncidentID() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err == nil {
		return hex.EncodeToString(bytes[:])
	}
	return fmt.Sprintf("%032x", time.Now().UnixNano())
}

// A request panic must not expose its value or overwrite a committed stream.
// Panics in unrelated goroutines/process exits are outside this recovery scope;
// the browser observes loss of reachability or a changed process instance.
func recoverHTTP(next http.Handler) http.Handler { return recoverHTTPWithApp(nil, next) }

func recoverHTTPWithApp(a *App, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := crashIncidentID()
		r = r.WithContext(context.WithValue(r.Context(), incidentContextKey{}, incidentScope{Request: requestID}))
		w.Header().Set("X-AIDE-Request-ID", requestID)
		tracked := &crashResponse{ResponseWriter: w, app: a, request: r, started: time.Now()}
		var output http.ResponseWriter = tracked
		_, flush := w.(http.Flusher)
		_, hijack := w.(http.Hijacker)
		switch {
		case flush && hijack:
			output = crashFlushHijack{tracked}
		case flush:
			output = crashFlush{tracked}
		case hijack:
			output = crashHijack{tracked}
		}
		defer func() {
			if value := recover(); value != nil {
				if err, ok := value.(error); ok && err == http.ErrAbortHandler {
					panic(value)
				}
				id := crashIncidentID()
				tracked.incidentID = id
				if a != nil {
					a.recordIncident(r.Context(), incident{ID: id, Module: incidentModule(r.URL.Path), Phase: "panic", Status: 500, PanicType: fmt.Sprintf("%T", value)}, fmt.Errorf("panic type %T", value), tracked.started)
				}
				// Type and stack only: panic values can contain credentials/content.
				// Route patterns omit concrete paths and every query/header/body.
				log.Printf("[%s] incident=%s route=%q type=%T stack=%s", serverPanicCode, id, r.Pattern, value, debug.Stack())
				if tracked.hijacked != nil {
					_ = tracked.hijacked.Close()
				}
				if tracked.committed.Load() || tracked.hijacked != nil {
					panic(http.ErrAbortHandler)
				}
				// A failed download may have prepared entity headers before writing.
				// They must not describe the diagnostic JSON response.
				for _, header := range []string{"Content-Length", "Content-Encoding", "Content-Disposition", "Content-Range", "Transfer-Encoding", "Trailer"} {
					output.Header().Del(header)
				}
				output.Header().Set("Cache-Control", "no-store")
				output.Header().Set("X-AIDE-Error-Code", serverPanicCode)
				output.Header().Set("X-AIDE-Incident-ID", id)
				jsonOut(output, http.StatusInternalServerError, map[string]any{
					"error": "服务请求发生内部异常，请按事件编号查看本机服务日志",
					"code":  serverPanicCode, "incidentId": id,
				})
			}
		}()
		next.ServeHTTP(output, r)
	})
}
