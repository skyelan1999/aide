package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

// This owner-only local engine endpoint is not a plugin or model tool. All
// operations reuse the worker's vault/screen/session/workspace gates and FIFO.
func (a *App) mixSoulMemoryManagement(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	b, err := a.selectedMixSoul()
	if err != nil || b == nil || b.Session != r.PathValue("id") {
		fail(w, 404, errors.New("memory engine unavailable"))
		return
	}
	if !a.mixSoulCurrent(*b) {
		fail(w, 423, errors.New("请先解锁助理会话"))
		return
	}
	var in struct {
		Operation string `json:"operation"`
		RecordID  string `json:"recordId"`
		Text      string `json:"text"`
		Offset    int    `json:"offset"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if err := decode(w, r, &in); err != nil {
		return
	}
	switch in.Operation {
	case "status", "list", "get", "correct", "delete", "undo-merge", "suppress", "allow", "allow-topic":
	default:
		fail(w, 400, errors.New("unsupported memory operation"))
		return
	}
	if len(in.Text) > 6000 || in.Offset < 0 || in.Offset > 20000 {
		fail(w, 400, errors.New("invalid memory operation"))
		return
	}
	result, err := a.mixSoulCall(r.Context(), *b, map[string]any{"phase": "management", "operation": in.Operation, "recordId": in.RecordID, "text": in.Text, "offset": in.Offset, "now": time.Now().UTC().Format(time.RFC3339Nano)})
	if err != nil || !a.mixSoulCurrent(*b) {
		fail(w, 423, errors.New("memory unavailable or scope changed"))
		return
	}
	data := json.RawMessage(result.Instructions)
	if !json.Valid(data) {
		fail(w, 500, errors.New("invalid memory result"))
		return
	}
	jsonOut(w, 200, data)
}
