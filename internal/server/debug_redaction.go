package server

import "net/http"

// Free-form errors may contain provider payloads, file text or credentials.
// Preserve only the presence of an error on the diagnostic surface.
func debugRedactedError(message string) string {
	if message != "" {
		return "[redacted]"
	}
	return ""
}

func (a *App) debugRunEvents(w http.ResponseWriter, r *http.Request) {
	a.serveRunEvents(w, r, true)
}

// Construct a new allowlisted value: future fields/events stay private by
// default. Diagnostic clients can observe progress without model/file content.
func debugStreamMetadata(ev streamEvent) (streamEvent, bool) {
	switch ev.Event {
	case "step", "tool", "intent", "status", "done", "compaction":
	default:
		return streamEvent{}, false
	}
	status := ""
	switch ev.Status {
	case "pending", "queued", "running", "awaiting_clarification", "complete", "completed", "done", "failed", "cancelled", "skipped", "paused", "error":
		status = ev.Status
	}
	return streamEvent{
		Event: ev.Event, Status: status, Tool: ev.Tool,
		Error: debugRedactedError(ev.Error), Round: ev.Round, OK: ev.OK,
		StartedAt: ev.StartedAt, BeforeTokens: ev.BeforeTokens,
		AfterTokens: ev.AfterTokens, Folded: ev.Folded,
	}, true
}
