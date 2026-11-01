package server

// Correlated, bounded diagnostics. Error values, arguments, provider payloads,
// command text, file bodies, URLs and credentials are deliberately not stored.
import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const incidentCapacity = 256
const incidentFileBytes int64 = 4 << 20

type incidentContextKey struct{}
type incidentScope struct{ Request, Task, Workspace string }

func incidentTrace(ctx context.Context) string {
	scope, _ := ctx.Value(incidentContextKey{}).(incidentScope)
	return scope.Request
}
func withIncidentTask(ctx context.Context, task *Task) context.Context {
	if task == nil {
		return ctx
	}
	scope, _ := ctx.Value(incidentContextKey{}).(incidentScope)
	if scope.Request == "" {
		scope.Request = task.DiagnosticTraceID
	}
	scope.Task = task.ID
	scope.Workspace = task.WorkspaceID
	return context.WithValue(ctx, incidentContextKey{}, scope)
}

type incident struct {
	ID            string                 `json:"incidentId"`
	Time          string                 `json:"time"`
	Request       string                 `json:"requestId,omitempty"`
	Task          string                 `json:"runId,omitempty"`
	Session       string                 `json:"sessionId,omitempty"`
	Workspace     string                 `json:"workspaceId,omitempty"`
	Module        string                 `json:"module"`
	Phase         string                 `json:"phase"`
	Plugin        string                 `json:"pluginId,omitempty"`
	PluginVersion string                 `json:"pluginVersion,omitempty"`
	Tool          string                 `json:"tool,omitempty"`
	Code          string                 `json:"code"`
	Severity      string                 `json:"severity"`
	Retryable     bool                   `json:"retryable"`
	CauseTypes    []string               `json:"causeTypes"`
	Locations     []string               `json:"locations"`
	PanicType     string                 `json:"panicType,omitempty"`
	Instance      string                 `json:"instanceId"`
	Fingerprint   string                 `json:"fingerprint"`
	DurationMS    int64                  `json:"durationMs"`
	Version       string                 `json:"version,omitempty"`
	Commit        string                 `json:"commit,omitempty"`
	Status        int                    `json:"httpStatus,omitempty"`
	PluginFault   *pluginFailureMetadata `json:"pluginFault,omitempty"`
}
type pluginFailureFrame struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}
type pluginFailureMetadata struct {
	Type   string               `json:"type"`
	Code   string               `json:"code,omitempty"`
	Frames []pluginFailureFrame `json:"frames"`
}
type pluginCallFailure struct {
	message    string
	diagnostic *pluginFailureMetadata
}

func (e *pluginCallFailure) Error() string { return e.message }
func sanitizedPluginFault(raw *pluginFailureMetadata) *pluginFailureMetadata {
	if raw == nil {
		return nil
	}
	out := &pluginFailureMetadata{Type: "Error", Frames: []pluginFailureFrame{}}
	for _, kind := range []string{"Error", "TypeError", "RangeError", "SyntaxError", "ReferenceError", "AggregateError", "URIError", "EvalError"} {
		if raw.Type == kind {
			out.Type = kind
		}
	}
	for _, code := range []string{"EIO", "ENOENT", "EACCES", "EPERM", "ENOSPC", "EMFILE", "ETIMEDOUT", "ECONNREFUSED", "ECONNRESET", "ENOTFOUND", "EPIPE", "MODULE_NOT_FOUND", "ERR_MODULE_NOT_FOUND", "ERR_REQUIRE_ESM", "ERR_INVALID_ARG_TYPE", "ERR_OUT_OF_RANGE", "ERR_INVALID_URL"} {
		if raw.Code == code {
			out.Code = code
		}
	}
	for _, frame := range raw.Frames {
		clean := filepath.ToSlash(filepath.Clean(frame.File))
		if len(clean) > 180 || strings.ContainsAny(clean, "\r\n\t:\x00") || clean == "." || strings.Contains(clean, "\\") || strings.HasPrefix(clean, "../") || filepath.IsAbs(clean) || frame.Line < 1 || frame.Line > 10000000 || frame.Column < 1 || frame.Column > 1000000 {
			continue
		}
		out.Frames = append(out.Frames, pluginFailureFrame{File: clean, Line: frame.Line, Column: frame.Column})
		if len(out.Frames) == 8 {
			break
		}
	}
	return out
}

type incidentJournal struct {
	mu                sync.Mutex
	loaded            bool
	entries           []incident
	dropped           uint64
	persistenceErrors uint64
	malformedRecords  uint64
}

func incidentIdentifier(s string) string {
	if len(s) > 128 {
		return "invalid-identifier"
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-_.:/ {}", c)) {
			return "invalid-identifier"
		}
	}
	return s
}
func incidentClass(err error, status int) (string, string, bool) {
	switch {
	case errors.Is(err, context.Canceled):
		return "CANCELED", "info", false
	case errors.Is(err, context.DeadlineExceeded):
		return "TIMEOUT", "warn", true
	case status == 401 || status == 403:
		return "ACCESS_DENIED", "warn", false
	case status == 409:
		return "STATE_CONFLICT", "warn", false
	case status >= 400 && status < 500:
		return "INVALID_REQUEST", "warn", false
	}
	var certificate x509.UnknownAuthorityError
	if errors.As(err, &certificate) {
		return "TLS_CERTIFICATE", "error", false
	}
	if errors.Is(err, os.ErrPermission) {
		return "ACCESS_DENIED", "warn", false
	}
	if errors.Is(err, os.ErrNotExist) {
		return "NOT_FOUND", "warn", false
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return "CONNECTION_REFUSED", "error", true
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return "DNS_FAILED", "error", true
	}
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		return "TIMEOUT", "warn", true
	}
	var syntax *json.SyntaxError
	if errors.As(err, &syntax) {
		return "INVALID_JSON", "error", false
	}
	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "超时"):
		return "TIMEOUT", "warn", true
	case strings.Contains(lower, "json") || strings.Contains(lower, "解析失败"):
		return "INVALID_JSON", "error", false
	case strings.Contains(lower, "eof") || strings.Contains(lower, "connection") || strings.Contains(lower, "连接"):
		return "CONNECTION_FAILED", "error", true
	case strings.Contains(lower, "权限") || strings.Contains(lower, "denied") || strings.Contains(lower, "沙箱"):
		return "ACCESS_DENIED", "warn", false
	case strings.Contains(lower, "变化") || strings.Contains(lower, "conflict"):
		return "STATE_CONFLICT", "warn", false
	default:
		return "OPERATION_FAILED", "error", false
	}
}
func incidentCauses(err error) []string {
	out := []string{}
	var visit func(error, int)
	visit = func(e error, depth int) {
		if e == nil || depth > 5 || len(out) >= 12 {
			return
		}
		out = append(out, fmt.Sprintf("%T", e))
		if joined, ok := e.(interface{ Unwrap() []error }); ok {
			for _, child := range joined.Unwrap() {
				visit(child, depth+1)
			}
		} else {
			visit(errors.Unwrap(e), depth+1)
		}
	}
	visit(err, 0)
	return out
}
func (j *incidentJournal) loadLocked(data string) {
	if j.loaded {
		return
	}
	j.loaded = true
	if data == "" {
		return
	}
	for _, suffix := range []string{".2", ".1", ""} {
		path := filepath.Join(data, "diagnostics", "incidents.jsonl") + suffix
		if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
			j.persistenceErrors++
			continue
		}
		f, err := os.Open(path)
		if err != nil {
			if !os.IsNotExist(err) {
				j.persistenceErrors++
			}
			continue
		}
		scanner := bufio.NewScanner(io.LimitReader(f, incidentFileBytes))
		scanner.Buffer(make([]byte, 4096), 64<<10)
		for scanner.Scan() {
			var e incident
			if json.Unmarshal(scanner.Bytes(), &e) != nil || len(e.ID) != 32 || e.Module == "" {
				j.malformedRecords++
				continue
			}
			j.entries = append(j.entries, e)
			if len(j.entries) > incidentCapacity {
				j.entries = j.entries[1:]
				j.dropped++
			}
		}
		if scanner.Err() != nil {
			j.malformedRecords++
		}
		f.Close()
	}
}
func (j *incidentJournal) append(data string, e incident) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.loadLocked(data)
	j.entries = append(j.entries, e)
	if len(j.entries) > incidentCapacity {
		j.entries = j.entries[1:]
		j.dropped++
	}
	if data == "" {
		return
	}
	if err := j.persist(data, e); err != nil {
		j.persistenceErrors++
		if j.persistenceErrors == 1 {
			log.Print("[WARN] AIDE-DIAGNOSTIC-PERSIST: incident journal unavailable; bounded in-memory diagnostics retained")
		}
	}
}
func (j *incidentJournal) persist(data string, e incident) error {
	dir := filepath.Join(data, "diagnostics")
	if info, err := os.Lstat(dir); err == nil && !info.IsDir() {
		return errors.New("non-directory incident store")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	file := filepath.Join(dir, "incidents.jsonl")
	if st, err := os.Lstat(file); err == nil {
		if !st.Mode().IsRegular() {
			return errors.New("nonregular incident journal")
		}
		if st.Size() >= incidentFileBytes {
			if err := os.Remove(file + ".2"); err != nil && !os.IsNotExist(err) {
				return err
			}
			if err := os.Rename(file+".1", file+".2"); err != nil && !os.IsNotExist(err) {
				return err
			}
			if err := os.Rename(file, file+".1"); err != nil {
				return err
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	f, err := os.OpenFile(file, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = f.Chmod(0600); err != nil {
		return err
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	return err
}
func (a *App) recordIncident(ctx context.Context, e incident, err error, started time.Time) string {
	if err == nil {
		return ""
	}
	if a.executionEnvironment != nil {
		return a.executionEnvironment.pool.parent.recordIncident(ctx, e, err, started)
	}
	scope, _ := ctx.Value(incidentContextKey{}).(incidentScope)
	if e.Request == "" {
		e.Request = scope.Request
	}
	if e.Task == "" {
		e.Task = scope.Task
	}
	if e.Workspace == "" {
		e.Workspace = scope.Workspace
	}
	if e.Request == "" && e.Task != "" {
		a.incidents.mu.Lock()
		for i := len(a.incidents.entries) - 1; i >= 0; i-- {
			previous := a.incidents.entries[i]
			if previous.Task == e.Task && previous.Request != "" {
				e.Request = previous.Request
				if e.Workspace == "" {
					e.Workspace = previous.Workspace
				}
				break
			}
		}
		a.incidents.mu.Unlock()
	}
	e.Request = incidentIdentifier(e.Request)
	e.Task = incidentIdentifier(e.Task)
	e.Session = incidentIdentifier(e.Session)
	e.Workspace = incidentIdentifier(e.Workspace)
	e.Module = incidentIdentifier(e.Module)
	e.Phase = incidentIdentifier(e.Phase)
	e.Tool = incidentIdentifier(e.Tool)
	e.Plugin = incidentIdentifier(e.Plugin)
	e.PluginVersion = incidentIdentifier(e.PluginVersion)
	if e.ID == "" {
		e.ID = crashIncidentID()
	}
	e.Time = time.Now().UTC().Format(time.RFC3339Nano)
	if e.Code == "" {
		e.Code, e.Severity, e.Retryable = incidentClass(err, e.Status)
	}
	e.CauseTypes = incidentCauses(err)
	var pluginError *pluginCallFailure
	if errors.As(err, &pluginError) {
		e.PluginFault = sanitizedPluginFault(pluginError.diagnostic)
	}
	e.Instance = a.startedAt.UTC().Format(time.RFC3339Nano)
	if strings.HasSuffix(e.Phase, "panic") {
		e.Code = "PANIC"
		e.Severity = "error"
		e.Retryable = false
	}
	var pcs [20]uintptr
	frames := runtime.CallersFrames(pcs[:runtime.Callers(2, pcs[:])])
	for len(e.Locations) < 8 {
		frame, more := frames.Next()
		if filepath.Base(frame.File) != "incidents.go" {
			e.Locations = append(e.Locations, fmt.Sprintf("%s:%d %s", filepath.Base(frame.File), frame.Line, frame.Function))
		}
		if !more {
			break
		}
	}
	e.Version = incidentIdentifier(a.version)
	e.Commit = incidentIdentifier(a.buildCommit)
	if !started.IsZero() {
		e.DurationMS = time.Since(started).Milliseconds()
	}
	pluginKey := ""
	if e.PluginFault != nil {
		b, _ := json.Marshal(e.PluginFault)
		pluginKey = string(b)
	}
	digest := sha256.Sum256([]byte(strings.Join([]string{e.Module, e.Phase, e.Plugin, e.Tool, e.Code, pluginKey, strings.Join(e.CauseTypes, ",")}, "\x00")))
	e.Fingerprint = hex.EncodeToString(digest[:])
	a.incidents.append(a.dataPath, e)
	log.Printf("[%s] AIDE-INCIDENT incident=%s request=%s run=%s module=%s phase=%s plugin=%s version=%s tool=%s code=%s fingerprint=%s", strings.ToUpper(e.Severity), e.ID, e.Request, e.Task, e.Module, e.Phase, e.Plugin, e.PluginVersion, e.Tool, e.Code, e.Fingerprint)
	return e.ID
}

// A tool may return a protocol-level failure inside an otherwise successful
// RPC. Observe explicit top-level failure flags without rewriting its result,
// guessing from arbitrary prose, or walking large/private payloads.
func incidentReportedFailure(value any) bool {
	if text, ok := value.(string); ok {
		if len(text) > 64<<10 {
			return false
		}
		var parsed map[string]any
		if json.Unmarshal([]byte(text), &parsed) != nil {
			return false
		}
		value = parsed
	}
	result, ok := value.(map[string]any)
	if !ok {
		return false
	}
	if success, ok := result["ok"].(bool); ok && !success {
		return true
	}
	if status, ok := result["status"].(string); ok && (status == "error" || status == "failed" || status == "crashed") {
		return true
	}
	if detail, ok := result["error"].(string); ok && detail != "" {
		return true
	}
	return false
}

// Record before preserving the existing panic semantics; do not pretend a
// partially executed operation has recovered or replay it automatically.
func (a *App) observeIncidentPanic(ctx context.Context, e incident) {
	if value := recover(); value != nil {
		e.Phase += "-panic"
		e.PanicType = fmt.Sprintf("%T", value)
		a.recordIncident(ctx, e, fmt.Errorf("panic type %T", value), time.Time{})
		panic(value)
	}
}
func (a *App) incidentSnapshot() ([]incident, map[string]uint64) {
	a.incidents.mu.Lock()
	defer a.incidents.mu.Unlock()
	a.incidents.loadLocked(a.dataPath)
	rows := append([]incident{}, a.incidents.entries...)
	return rows, map[string]uint64{"droppedFromMemory": a.incidents.dropped, "persistenceErrors": a.incidents.persistenceErrors, "malformedRecords": a.incidents.malformedRecords}
}
func (a *App) incidentsHandler(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 || v > incidentCapacity {
			fail(w, 400, errors.New("limit must be 1..256"))
			return
		}
		limit = v
	}
	rows, health := a.incidentSnapshot()
	out := []incident{}
	for i := len(rows) - 1; i >= 0 && len(out) < limit; i-- {
		e := rows[i]
		match := true
		for key, value := range map[string]string{"module": e.Module, "pluginId": e.Plugin, "runId": e.Task, "requestId": e.Request, "incidentId": e.ID, "fingerprint": e.Fingerprint, "code": e.Code, "severity": e.Severity} {
			if filter := r.URL.Query().Get(key); filter != "" && filter != value {
				match = false
			}
		}
		if match {
			out = append(out, e)
		}
	}
	jsonOut(w, 200, map[string]any{"entries": out, "health": health, "capacity": incidentCapacity, "retention": "three 4 MiB JSONL segments; recent 256 entries in memory", "privacy": "metadata and error types only; no raw error text, arguments, commands, URLs, payloads or credentials"})
}
func incidentModule(path string) string {
	for prefix, module := range map[string]string{"/api/plugins": "plugins", "/api/file": "files", "/api/sources": "sources", "/api/settings": "settings", "/api/knowledge": "knowledge", "/api/command": "shell", "/api/updates": "updates", "/api/context": "context", "/api/tts": "speech", "/api/messages": "messaging", "/api/sessions": "workflow"} {
		if strings.HasPrefix(path, prefix) {
			return module
		}
	}
	return "http"
}
