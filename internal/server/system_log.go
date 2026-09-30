package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

const systemLogCapacity = 5000

type systemLogEntry struct {
	Time    string `json:"time"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

type systemLogBuffer struct {
	mu      sync.RWMutex
	entries []systemLogEntry
	next    int // 下一条覆盖写入的位置
}

var (
	systemLogs        = &systemLogBuffer{entries: make([]systemLogEntry, 0, systemLogCapacity)}
	logSecretPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(["']?\b(?:api[_-]?key|access[_-]?token|refresh[_-]?token|authorization|password|passwd|secret)\b["']?\s*[:=]\s*["']?)([^"'\s,;}]+)`),
		regexp.MustCompile(`(?i)(\bBearer\s+)[A-Za-z0-9._~+/=-]+`),
		regexp.MustCompile(`(?i)(https?://[^\s?#]+\?)[^\s#]+`),
	}
)

func init() {
	// Capture the existing stdlib logs (including startup) while retaining Docker/terminal output.
	log.SetOutput(&systemLogWriter{dst: os.Stderr})
}

type systemLogWriter struct{ dst *os.File }

func (w *systemLogWriter) Write(p []byte) (int, error) {
	_, _ = w.dst.Write(p)
	for _, line := range strings.Split(strings.TrimRight(string(p), "\r\n"), "\n") {
		if line == "" {
			continue
		}
		systemLogs.add(line)
	}
	return len(p), nil
}

func (b *systemLogBuffer) add(line string) {
	line = redactSystemLog(line)
	entry := systemLogEntry{Time: time.Now().Format(time.RFC3339Nano), Level: inferSystemLogLevel(line), Message: line}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.entries) < systemLogCapacity {
		b.entries = append(b.entries, entry)
		if len(b.entries) == systemLogCapacity {
			b.next = 0
		}
	} else {
		b.entries[b.next] = entry
		b.next = (b.next + 1) % systemLogCapacity
	}
}

func redactSystemLog(line string) string {
	for _, pattern := range logSecretPatterns {
		line = pattern.ReplaceAllString(line, `${1}[REDACTED]`)
	}
	return line
}

func inferSystemLogLevel(line string) string {
	lower := strings.ToLower(line)
	if strings.Contains(lower, "[error]") || strings.Contains(lower, "[fatal]") || strings.Contains(lower, "panic:") || strings.Contains(lower, " error:") || strings.Contains(lower, "错误") || strings.Contains(lower, "失败") || strings.Contains(lower, "异常") {
		return "error"
	}
	if strings.Contains(lower, "[warn]") || strings.Contains(lower, "[warning]") || strings.Contains(lower, " warning:") || strings.Contains(lower, "警告") || strings.Contains(lower, "超时") || strings.Contains(lower, "降级") {
		return "warn"
	}
	if strings.Contains(lower, "[debug]") || strings.Contains(lower, "[trace]") {
		return "debug"
	}
	return "info"
}

func (b *systemLogBuffer) snapshot(level string, limit int, start, end *time.Time) []systemLogEntry {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]systemLogEntry, 0, len(b.entries))
	for i := 0; i < len(b.entries) && len(out) < limit; i++ {
		index := len(b.entries) - 1 - i
		if len(b.entries) == systemLogCapacity {
			index = (b.next - 1 - i + systemLogCapacity) % systemLogCapacity
		}
		entry := b.entries[index]
		if level != "all" && entry.Level != level {
			continue
		}
		if start != nil || end != nil {
			loggedAt, err := time.Parse(time.RFC3339Nano, entry.Time)
			if err != nil || (start != nil && loggedAt.Before(*start)) || (end != nil && loggedAt.After(*end)) {
				continue
			}
		}
		out = append(out, entry)
	}
	return out
}

func (a *App) systemLogsHandler(w http.ResponseWriter, r *http.Request) {
	level := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("level")))
	if level == "" {
		level = "all"
	}
	if level != "all" && level != "debug" && level != "info" && level != "warn" && level != "error" {
		http.Error(w, "level must be all, debug, info, warn, or error", http.StatusBadRequest)
		return
	}
	limit := 500
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if _, err := fmt.Sscanf(raw, "%d", &limit); err != nil || limit < 1 {
			http.Error(w, "limit must be a positive integer", http.StatusBadRequest)
			return
		}
		if limit > systemLogCapacity {
			limit = systemLogCapacity
		}
	}
	var start, end *time.Time
	if raw := strings.TrimSpace(r.URL.Query().Get("start")); raw != "" {
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			http.Error(w, "start must be an RFC3339 date-time", http.StatusBadRequest)
			return
		}
		start = &parsed
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("end")); raw != "" {
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			http.Error(w, "end must be an RFC3339 date-time", http.StatusBadRequest)
			return
		}
		end = &parsed
	}
	if start != nil && end != nil && start.After(*end) {
		http.Error(w, "start must not be after end", http.StatusBadRequest)
		return
	}
	entries := systemLogs.snapshot(level, limit, start, end)
	if r.URL.Query().Get("download") == "1" {
		w.Header().Set("Content-Disposition", `attachment; filename="aide-system-logs.jsonl"`)
		w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
		enc := json.NewEncoder(w)
		for _, entry := range entries {
			if err := enc.Encode(entry); err != nil {
				return
			}
		}
		return
	}
	jsonOut(w, http.StatusOK, map[string]any{"entries": entries, "count": len(entries), "capacity": systemLogCapacity, "level": level})
}
