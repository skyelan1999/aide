package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"sync/atomic"
	"time"
)

const (
	terminalOutputTool    = "terminal_session_poll"
	terminalOutputReaders = 4
	terminalOutputPage    = 64 << 10
)

var terminalSessionIDPattern = regexp.MustCompile(`^[a-f0-9]{24}$`)

type terminalOutputReader struct{ cancel context.CancelFunc }

type terminalOutputChunk struct {
	ID     string `json:"id"`
	Start  uint64 `json:"start"`
	Data   string `json:"data"`
	Offset uint64 `json:"offset"`
	Reset  bool   `json:"reset"`
	More   bool   `json:"more"`
}

func (a *App) terminalOutputEnabledLocked(packageDigest string) bool {
	if !integrityDigestPattern.MatchString(packageDigest) || toolDeniedIn(a.settings.DisabledTools, terminalOutputTool) || sandboxMode(a.settings.SandboxMode) == "read-only" {
		return false
	}
	for _, p := range a.pluginRegistry.Plugins {
		if p.ID == "terminal" && p.Enabled && p.Daemon && p.PackageSHA256 == packageDigest &&
			p.Workbench != nil && p.Workbench.TerminalOutput == 1 && slices.Contains(p.Workbench.Tools, terminalOutputTool) {
			return true
		}
	}
	return false
}

func (a *App) terminalOutputBridgeLocked(key string, full bool) (*pluginWorkbenchBridge, error) {
	b, err := a.workbenchBridgeLocked("terminal", key, full)
	if err != nil {
		return nil, err
	}
	if !b.TerminalOutput || !a.terminalOutputEnabledLocked(b.Package) || a.nativeEditorWriteErrorLocked() != nil {
		return nil, errors.New("Terminal output capability denied")
	}
	return b, nil
}

func (a *App) streamTerminalOutput(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	query := r.URL.Query()
	id, rawOffset := query.Get("session"), query.Get("offset")
	offset, err := strconv.ParseUint(rawOffset, 10, 53)
	if len(query) != 2 || len(query["session"]) != 1 || len(query["offset"]) != 1 || !terminalSessionIDPattern.MatchString(id) || err != nil {
		fail(w, 400, errors.New("invalid Terminal output query"))
		return
	}
	key := r.PathValue("instance")
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	read := &terminalOutputReader{cancel: cancel}
	a.mu.Lock()
	b, err := a.terminalOutputBridgeLocked(key, true)
	n := 0
	for _, candidate := range a.pluginWorkbenchBridges {
		if candidate.OutputReader != nil {
			n++
		}
	}
	switch {
	case err != nil || a.daemons == nil:
		a.mu.Unlock()
		fail(w, 403, errors.New("Terminal output capability denied"))
		return
	case n >= terminalOutputReaders || b.OutputReader != nil:
		a.mu.Unlock()
		fail(w, 409, errors.New("Terminal output reader is already in use"))
		return
	}
	b.OutputReader = read
	workspace := a.pluginWorkspaceLocked()
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		if b.OutputReader == read {
			b.OutputReader = nil
		}
		a.mu.Unlock()
	}()

	scopeCurrent := func(full bool) bool {
		if ctx.Err() != nil {
			return false
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		current, err := a.terminalOutputBridgeLocked(key, full)
		return err == nil && current == b && current.OutputReader == read
	}
	ctx = context.WithValue(ctx, terminalWorkbenchContextKey{}, true)
	terminal, err := a.terminalToolContext(ctx, terminalOutputTool, nil)
	if err != nil || !scopeCurrent(false) {
		fail(w, 403, errors.New("Terminal output scope revoked"))
		return
	}
	terminal["outputWatch"] = true
	ctx = context.WithValue(ctx, terminalContextKey{}, terminal)
	ctx = context.WithValue(ctx, pluginWorkspaceContextKey{}, workspace)
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	controller := http.NewResponseController(w)
	encoder := json.NewEncoder(w)
	write := func(output *terminalOutputChunk) bool {
		_ = controller.SetWriteDeadline(time.Now().Add(3 * time.Second))
		var line struct {
			Output    *terminalOutputChunk `json:"output,omitempty"`
			Heartbeat bool                 `json:"heartbeat,omitempty"`
		}
		line.Output, line.Heartbeat = output, output == nil
		return encoder.Encode(line) == nil && controller.Flush() == nil
	}
	if !write(nil) {
		return
	}
	last := time.Now()
	for scopeCurrent(false) {
		started := time.Now()
		output, err := a.daemons.readTerminalOutput(ctx, id, offset)
		if err != nil {
			return
		}
		if output.Data != "" || output.Reset {
			if !scopeCurrent(true) || !write(output) {
				return
			}
			offset = output.Offset
			last = time.Now()
			continue
		}
		if time.Since(last) >= 2*time.Second {
			if !scopeCurrent(true) || !write(nil) {
				return
			}
			last = time.Now()
		}
		if remain := 25*time.Millisecond - time.Since(started); remain > 0 {
			timer := time.NewTimer(remain)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}
}

// Only this fixed, read-only Terminal method may abandon its pending reply
// without terminating the daemon. The IPC slot remains occupied until reply or
// process exit, so a deadline or early disconnect cannot enqueue orphan reads.
func (m *DaemonManager) readTerminalOutput(ctx context.Context, session string, offset uint64) (*terminalOutputChunk, error) {
	m.mu.Lock()
	p := m.procs["terminal"]
	m.mu.Unlock()
	if p == nil {
		return nil, errors.New("Terminal daemon unavailable")
	}

	p.lifecycleMu.Lock()
	if ctx.Err() != nil || p.statusSnapshot() != dsRunning {
		p.lifecycleMu.Unlock()
		return nil, errors.New("Terminal daemon unavailable")
	}
	p.mu.Lock()
	if len(p.terminalReads) >= terminalOutputReaders {
		p.mu.Unlock()
		p.lifecycleMu.Unlock()
		return nil, errors.New("Terminal output reader limit reached")
	}
	if p.terminalReads == nil {
		p.terminalReads = make(map[int64]struct{})
	}
	id := atomic.AddInt64(&p.nextID, 1)
	ch := make(chan rpcResp, 1)
	p.terminalReads[id] = struct{}{}
	p.pending[id] = ch
	done := p.doneCh
	p.mu.Unlock()
	p.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": "tool.call", "params": map[string]any{
		"tool": terminalOutputTool, "args": map[string]any{"id": session, "offset": offset, "outputWatch": true},
		"workspace": pluginWorkspaceFromContext(ctx), "terminalContext": ctx.Value(terminalContextKey{}),
	}})
	p.lifecycleMu.Unlock()

	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return nil, context.DeadlineExceeded
	case <-done:
		return nil, errors.New("Terminal daemon exited")
	case reply := <-ch:
		if reply.err != nil {
			return nil, errors.New("Terminal output unavailable")
		}
		return decodeTerminalOutput(reply.result, session, offset)
	}
}

func decodeTerminalOutput(raw []byte, id string, requested uint64) (*terminalOutputChunk, error) {
	var result struct {
		Output *terminalOutputChunk `json:"output"`
	}
	malformed := errors.New("invalid Terminal output")
	if len(raw) > base64.StdEncoding.EncodedLen(terminalOutputPage)+512 || json.Unmarshal(raw, &result) != nil || result.Output == nil {
		return nil, malformed
	}
	out := result.Output
	if out.ID != id || (!out.Reset && out.Start != requested) || out.Offset < out.Start || out.Offset > (1<<53)-1 ||
		len(out.Data) > base64.StdEncoding.EncodedLen(terminalOutputPage) {
		return nil, malformed
	}
	data, err := base64.StdEncoding.Strict().DecodeString(out.Data)
	if err != nil || len(data) > terminalOutputPage || uint64(len(data)) != out.Offset-out.Start {
		return nil, malformed
	}
	return out, nil
}
