package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func writeBudgetAttachment(t *testing.T, a *App, name, body string) Attachment {
	t.Helper()
	if err := os.WriteFile(filepath.Join(a.workPath, name), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return Attachment{Root: "workspace", Path: name}
}

func TestAttachmentBudgetBeyondLegacy195KiB(t *testing.T) {
	for _, tc := range []struct{ name, body string }{{"ascii", strings.Repeat("a", 230000)}, {"unicode", strings.Repeat("中", 90000)}} {
		t.Run(tc.name, func(t *testing.T) {
			a := testApp(t)
			marker := "ATTACHMENT-END-73186"
			att := writeBudgetAttachment(t, a, "large.txt", tc.body+marker)
			text, _, versions, err := a.attachmentContext([]Attachment{att})
			if err != nil {
				t.Fatal("valid large attachment rejected", err)
			}
			if !strings.Contains(text, tc.body+marker) || versions["large.txt"].BaseHash == "" {
				t.Fatal("attachment content or version silently lost")
			}
			var delivered atomic.Bool
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Messages []Message `json:"messages"`
				}
				_ = json.NewDecoder(r.Body).Decode(&req)
				for _, m := range req.Messages {
					if strings.Contains(m.Content, tc.body+marker) {
						delivered.Store(true)
					}
				}
				jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: "Synthetic attachment received."}, "finish_reason": "stop"}}})
			}))
			defer provider.Close()
			a.settings = Settings{BaseURL: provider.URL, APIKey: "synthetic", Model: "attachment-fixture", Models: []ModelRef{{ID: "attachment-fixture", ContextWindow: 262144}}}
			s := createSession(t, a)
			payload := map[string]any{"sessionId": s.ID, "mode": "chat", "prompt": "Read the synthetic attachment.", "attachments": []Attachment{att}}
			p := request(a, "POST", "/api/context-preview", payload)
			requireStatus(t, p, 200)
			var preview ContextPreview
			if json.Unmarshal(p.Body.Bytes(), &preview) != nil || preview.OverLimit {
				t.Fatal("large-window preview incorrectly blocked", p.Body.String())
			}
			started := request(a, "POST", "/api/sessions/"+s.ID+"/runs", payload)
			requireStatus(t, started, 202)
			waitTaskDone(t, a, s.ID)
			a.mu.Lock()
			status := a.sessions[s.ID].Runs[0].Status
			a.mu.Unlock()
			if status != "completed" || !delivered.Load() {
				t.Fatal("full attachment did not reach the synthetic provider", status)
			}
		})
	}
}
func TestAttachmentBudgetMultipleFilesAndCapacity(t *testing.T) {
	a := testApp(t)
	atts := []Attachment{}
	for i := 0; i < 8; i++ {
		atts = append(atts, writeBudgetAttachment(t, a, fmt.Sprintf("part-%d.txt", i), strings.Repeat("段", 10000)+fmt.Sprintf("PART-END-%d", i)))
	}
	text, _, _, err := a.attachmentContext(atts)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		if !strings.Contains(text, fmt.Sprintf("PART-END-%d", i)) {
			t.Fatal("missing file", i)
		}
	}
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) }))
	defer provider.Close()
	a.settings = Settings{BaseURL: provider.URL, APIKey: "synthetic", Model: "small", Models: []ModelRef{{ID: "small", ContextWindow: 4096}}}
	s := createSession(t, a)
	payload := map[string]any{"sessionId": s.ID, "mode": "chat", "prompt": "Read the files.", "attachments": atts}
	w := request(a, "POST", "/api/context-preview", payload)
	requireStatus(t, w, 200)
	var preview ContextPreview
	if json.Unmarshal(w.Body.Bytes(), &preview) != nil || !preview.OverLimit {
		t.Fatal("small-window preview must report capacity")
	}
	// Existing history must also stay untouched when the current input alone cannot fit.
	a.mu.Lock()
	a.sessions[s.ID].Messages = []Message{{Role: "user", Content: "KEEP-HISTORY"}}
	a.sessions[s.ID].Compact = "KEEP-SUMMARY"
	a.mu.Unlock()
	w = request(a, "POST", "/api/sessions/"+s.ID+"/runs", payload)
	requireStatus(t, w, 400)
	a.mu.Lock()
	unchanged := len(a.sessions[s.ID].Messages) == 1 && a.sessions[s.ID].Messages[0].Content == "KEEP-HISTORY" && a.sessions[s.ID].Compact == "KEEP-SUMMARY" && len(a.sessions[s.ID].Runs) == 0
	a.mu.Unlock()
	if !unchanged {
		t.Fatal("rejection modified history or started a task")
	}
	if !strings.Contains(w.Body.String(), "上下文预算") || strings.Contains(w.Body.String(), "195 KB") {
		t.Fatal("capacity error is not model-specific", w.Body.String())
	}
	if calls.Load() != 0 {
		t.Fatal("oversize current input must not invoke model or compactor")
	}
	atts = append(atts, atts[0])
	payload["attachments"] = atts
	requireStatus(t, request(a, "POST", "/api/context-preview", payload), 400)
}
func TestAttachmentBudgetProcessingGuardDoesNotReturnPartialInput(t *testing.T) {
	a := testApp(t)
	att := writeBudgetAttachment(t, a, "oversized.txt", strings.Repeat("x", maxAttachmentTextBytes))
	text, images, versions, err := a.attachmentContext([]Attachment{att})
	if err == nil || !strings.Contains(err.Error(), "处理预算") {
		t.Fatal("missing separate processing guard", err)
	}
	if text != "" || len(images) != 0 || len(versions) != 0 {
		t.Fatal("partial attachment leaked after rejection")
	}
}
