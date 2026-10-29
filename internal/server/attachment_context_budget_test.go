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

func TestAttachmentContextResourceBoundary(t *testing.T) {
	a := testApp(t)
	const name = "boundary.txt"
	const footer = "\n</untrusted-file>\n"
	header := fmt.Sprintf("\n<untrusted-file root=%q path=%q>\n", "workspace", name)
	body := strings.Repeat("x", maxAttachmentTextBytes/8-len(header)-len(footer))
	file := filepath.Join(a.workPath, name)
	if err := os.WriteFile(file, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	atts := make([]Attachment, 8)
	for i := range atts {
		atts[i] = Attachment{Root: "workspace", Path: name}
	}
	text, _, _, err := a.attachmentContext(atts)
	if err != nil || len(text) != maxAttachmentTextBytes {
		t.Fatalf("exact resource boundary rejected: bytes=%d, err=%v", len(text), err)
	}
	text = ""
	if err := os.WriteFile(file, []byte(body+"x"), 0600); err != nil {
		t.Fatal(err)
	}
	text, images, versions, err := a.attachmentContext(atts)
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("%d MiB", maxAttachmentTextBytes>>20)) || !strings.Contains(err.Error(), "分段读取") || text != "" || images != nil || versions != nil {
		t.Fatalf("over-resource input must reject before assembly with range-read guidance: %v", err)
	}
}

// #601: the same bytes must reach preview and the first real request. A local
// synthetic provider observes requests; no account, inference or user file is used.
func TestAttachmentContextUsesModelBudget(t *testing.T) {
	for _, tc := range []struct {
		name, body string
	}{
		{"ascii-old-boundary", strings.Repeat("a", 200000)},
		{"chinese-old-boundary", strings.Repeat("字", 66667)},
		{"multiple-large-files", strings.Repeat("data\n", 50000)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := testApp(t)
			var calls atomic.Int32
			observed := make(chan []Message, 4)
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var input struct {
					Messages []Message `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
					t.Error(err)
				}
				calls.Add(1)
				for _, message := range input.Messages {
					if message.Role == "user" && strings.Contains(message.Content, "<untrusted-file") {
						observed <- input.Messages
						break
					}
				}
				jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: "fixture done"}}}})
			}))
			defer provider.Close()
			a.settings = Settings{BaseURL: provider.URL, Model: "large", Models: []ModelRef{{ID: "large", ContextWindow: 1000000}, {ID: "small", ContextWindow: 8192}}}
			s := createSession(t, a)
			atts := []Attachment{{Root: "workspace", Path: "one.txt"}, {Root: "workspace", Path: "two.txt"}}
			for _, att := range atts {
				if err := os.WriteFile(filepath.Join(a.workPath, att.Path), []byte(tc.body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			contextText, _, _, err := a.attachmentContext(atts)
			if err != nil {
				t.Fatal(err)
			}
			input := map[string]any{"sessionId": s.ID, "mode": "chat", "prompt": "inspect synthetic attachments", "attachments": atts}
			w := request(a, "POST", "/api/context-preview", input)
			requireStatus(t, w, 200)
			var preview ContextPreview
			if err := json.Unmarshal(w.Body.Bytes(), &preview); err != nil {
				t.Fatal(err)
			}
			if preview.OverLimit || preview.Breakdown.Attachments.Bytes < 2*len(tc.body) || preview.Breakdown.AttachmentFiles != 2 {
				t.Fatalf("large model preview did not include both files: %+v", preview.Breakdown)
			}
			for _, att := range atts {
				if !strings.Contains(preview.Messages[len(preview.Messages)-1].Content, tc.body) {
					t.Fatalf("preview omitted %s", att.Path)
				}
			}
			requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs", input), 202)
			waitTaskDone(t, a, s.ID)
			select {
			case messages := <-observed:
				found := false
				for _, message := range messages {
					if message.Role == "user" && strings.Contains(message.Content, contextText) {
						found = true
					}
				}
				if !found || !strings.Contains(preview.Messages[len(preview.Messages)-1].Content, contextText) {
					t.Fatal("preview/start attachment content differs")
				}
			default:
				t.Fatal("accepted run never reached synthetic provider")
			}
			before := calls.Load()
			small := createSession(t, a)
			input["sessionId"] = small.ID
			input["modelSelection"] = SessionModelSelection{Model: "small", Strategy: "manual", Profile: "default", ReasoningEffort: "auto"}
			w = request(a, "POST", "/api/context-preview", input)
			requireStatus(t, w, 200)
			if err := json.Unmarshal(w.Body.Bytes(), &preview); err != nil {
				t.Fatal(err)
			}
			if !preview.OverLimit || preview.ContextWindow != 8192 {
				t.Fatal("small model must report its actual window overflow")
			}
			w = request(a, "POST", "/api/sessions/"+small.ID+"/runs", input)
			requireStatus(t, w, 400)
			if !strings.Contains(w.Body.String(), "上下文") || strings.Contains(w.Body.String(), "195") {
				t.Fatal("overflow must explain model context budget")
			}
			if calls.Load() != before {
				t.Fatal("over-limit fresh session called provider")
			}
		})
	}
}

func TestAttachmentContextUnreadableDoesNotSilentlyOmit(t *testing.T) {
	a := testApp(t)
	if err := os.WriteFile(filepath.Join(a.workPath, "good.txt"), []byte(strings.Repeat("x", 250000)), 0600); err != nil {
		t.Fatal(err)
	}
	text, images, versions, err := a.attachmentContext([]Attachment{{Root: "workspace", Path: "good.txt"}, {Root: "workspace", Path: "missing.txt"}})
	if err == nil || !strings.Contains(err.Error(), "missing.txt") || text != "" || images != nil || versions != nil {
		t.Fatalf("must reject unreadable attachment without partial success: %v", err)
	}
}

func TestAttachmentContextLargeDocumentsKeepTruncationNotice(t *testing.T) {
	a := testApp(t)
	useSourceOfficeScripts(t)
	data, err := officeCreateBytes("docx", map[string]any{"blocks": []any{map[string]any{"type": "paragraph", "text": strings.Repeat("字", maxDocExtractChars+1) + "TAIL"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.workPath, "large.docx"), data, 0600); err != nil {
		t.Fatal(err)
	}
	text, images, versions, err := a.attachmentContext([]Attachment{{Root: "workspace", Path: "large.docx"}, {Root: "workspace", Path: "large.docx"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(text) <= 200000 || strings.Count(text, "字") < 2*maxDocExtractChars || strings.Count(text, "文档过长已截断") != 2 || strings.Contains(text, "TAIL") || len(images) != 0 || len(versions) != 0 {
		t.Fatal("large extracted documents must retain visible per-document truncation notices")
	}
}
