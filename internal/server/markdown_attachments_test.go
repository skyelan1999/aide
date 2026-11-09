package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"
)

func TestMarkdownReferencePreviewAndTaskReadTail(t *testing.T) {
	a := testApp(t)
	const marker = "MARKDOWN-TAIL-836-VERIFIED"
	body := "# Large Markdown\n" + strings.Repeat("参考内容 abcdefghijklmnop\n", 60000) + marker
	att := writeBudgetAttachment(t, a, "large.md", body)
	text, _, versions, err := a.attachmentContext([]Attachment{att})
	if err != nil || len(text) > 3000 || !strings.Contains(text, `"mode":"file-reference"`) || strings.Contains(text, marker) || len(versions) != 0 {
		t.Fatalf("reference/preview/baseline: bytes=%d versions=%d err=%v", len(text), len(versions), err)
	}
	var initial, tail atomic.Bool
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		for _, m := range req.Messages {
			if m.Role == "tool" && strings.Contains(m.Content, marker) {
				tail.Store(true)
			}
		}
		found := false
		for _, m := range req.Messages {
			if m.Role == "user" && strings.Contains(m.Content, text) {
				found = true
			}
		}
		// Routing/title requests may share this fixture provider. Only issue
		// the read tool from the actual task request carrying the attachment.
		if found && !initial.Swap(true) {
			args, _ := json.Marshal(map[string]any{"path": att.Path, "byteOffset": len(body) - 100, "byteLimit": 100})
			jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": "tail", "type": "function", "function": map[string]any{"name": "read_file", "arguments": string(args)}}}}, "finish_reason": "tool_calls"}}})
			return
		}
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: "tail verified"}, "finish_reason": "stop"}}})
	}))
	defer provider.Close()
	a.settings = Settings{BaseURL: provider.URL, APIKey: "synthetic", Model: "markdown-fixture", Models: []ModelRef{{ID: "markdown-fixture", ContextWindow: 65536}}}
	s := createSession(t, a)
	payload := map[string]any{"sessionId": s.ID, "mode": "chat", "prompt": "Read only the end marker of the attached Markdown.", "attachments": []Attachment{att}}
	w := request(a, "POST", "/api/context-preview", payload)
	requireStatus(t, w, 200)
	var preview ContextPreview
	if json.Unmarshal(w.Body.Bytes(), &preview) != nil || preview.OverLimit || preview.Breakdown.Attachments.Bytes > 3000 {
		t.Fatal("large Markdown blocked", w.Body.String())
	}
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs", payload), 202)
	waitTaskDone(t, a, s.ID)
	if !tail.Load() {
		t.Fatal("model did not receive tail through read_file")
	}
	unchanged, _ := os.ReadFile(filepath.Join(a.workPath, att.Path))
	if string(unchanged) != body {
		t.Fatal("attachment file modified")
	}
}

func TestMarkdownSharedInlineBudgetAndCompleteSmallBaseline(t *testing.T) {
	a := testApp(t)
	atts := []Attachment{}
	for i := 0; i < 8; i++ {
		atts = append(atts, writeBudgetAttachment(t, a, fmt.Sprintf("part-%d.md", i), strings.Repeat("中", 4000)+fmt.Sprintf("PART-%d", i)))
	}
	text, _, versions, err := a.attachmentContext(atts)
	if err != nil || !utf8.ValidString(text) || len(text) > 30000 || len(versions) != 1 || strings.Count(text, `"mode":"file-reference"`) != 7 {
		t.Fatalf("aggregate budget: bytes=%d versions=%d err=%v", len(text), len(versions), err)
	}
	if versions["part-0.md"].Before != strings.Repeat("中", 4000)+"PART-0" {
		t.Fatal("small inline baseline lost")
	}
	if !strings.Contains(text, `"previewByteEnd":768`) {
		t.Fatal("preview byte boundary missing")
	}
	att := writeBudgetAttachment(t, a, "workspace-only.md", strings.Repeat("x", markdownInlineBudget+1))
	att.Source = "unrelated-source"
	text, _, _, err = a.attachmentContext([]Attachment{att})
	if err != nil || strings.Contains(text, "unrelated-source") {
		t.Fatal("workspace reference routed through unrelated source", err)
	}
}

func TestMarkdownReferenceSourceAndContextCanReadBeyondPreview(t *testing.T) {
	a := sourceActionFixture(t)
	src, _ := a.findSource("readonly-actions")
	root, err := a.localSourceRoot(src)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	body := strings.Repeat("Reference sentence.\n", 10000) + "SOURCE-TAIL"
	f, err := root.Create("large.md")
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString(body)
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	att := Attachment{Root: "source", Source: src.ID, Path: "large.md"}
	text, _, versions, err := a.attachmentContext([]Attachment{att})
	if err != nil || len(versions) != 0 || !strings.Contains(text, `"source":"readonly-actions"`) {
		t.Fatal("source reference", err)
	}
	args := fmt.Sprintf(`{"source":%q,"path":"large.md","byteOffset":%d,"byteLimit":100}`, src.ID, len(body)-100)
	got := a.executeToolCall(context.Background(), readCall("read_file", args), &Task{}, nil)
	if !strings.Contains(got, "SOURCE-TAIL") {
		t.Fatal("source tail inaccessible", got)
	}
	if err := os.WriteFile(filepath.Join(a.reference.Name(), "context.md"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	a.sourceRegistry.Sources = append(a.sourceRegistry.Sources, Source{ID: contextSource, Type: "local", Enabled: true})
	text, _, _, err = a.attachmentContext([]Attachment{{Root: "context", Path: "context.md"}})
	if err != nil || !strings.Contains(text, `"source":"`+contextSource+`"`) {
		t.Fatal("context reference", err)
	}
}

func TestMarkdownReferenceValidatesWholeFileAndPaths(t *testing.T) {
	a := testApp(t)
	for i, suffix := range []string{"\x00", "\xff"} {
		att := writeBudgetAttachment(t, a, fmt.Sprintf("bad-%d.md", i), strings.Repeat("x", markdownInlineBudget+100)+suffix)
		if text, _, versions, err := a.attachmentContext([]Attachment{att}); err == nil || text != "" || versions != nil {
			t.Fatal("invalid tail accepted")
		}
	}
	for _, p := range []string{"../outside.md", "/tmp/outside.md"} {
		if _, _, _, err := a.attachmentContext([]Attachment{{Root: "workspace", Path: p}}); err == nil {
			t.Fatal("unsafe path accepted")
		}
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "outside.md"), filepath.Join(a.workPath, "escape.md")); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := a.attachmentContext([]Attachment{{Root: "workspace", Path: "escape.md"}}); err == nil {
		t.Fatal("symlink escape accepted")
	}
}
