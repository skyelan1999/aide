package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestQwenImageWireProtocol(t *testing.T) {
	var count atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		if r.URL.Path != "/compatible-mode/v1/images/generations" || r.Method != "POST" || r.Header.Get("Authorization") != "Bearer synthetic-key" || r.Header.Get("X-DashScope-Async") != "" {
			t.Errorf("wrong image protocol: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if len(body) != 4 || body["prompt"] != "a fox" || body["n"] != float64(1) || body["size"] != "1024x1024" {
			t.Errorf("wrong image body: %v", body)
		}
		_, _ = w.Write([]byte(`{"data":[{"url":"https://example.com/generated.png?signature=demo"}],"usage":{"output_image_count":1}}`))
	}))
	defer provider.Close()
	var snapshot []byte
	out, err := generateQwenImage(context.Background(), Settings{BaseURL: provider.URL + "/compatible-mode/v1/", Model: "qwen-image-3.0", APIKey: "synthetic-key"}, "a fox", func(b []byte) { snapshot = b })
	if err != nil || !strings.Contains(out, "![生成图片](<https://example.com/generated.png?signature=demo>)") || count.Load() != 1 || !strings.Contains(string(snapshot), `"prompt":"a fox"`) {
		t.Fatalf("image result: %q %v calls=%d", out, err, count.Load())
	}
}

func TestQwenImageNativeProtocol(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/services/aigc/multimodal-generation/generation" {
			t.Errorf("native path=%s", r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["input"] == nil || body["parameters"] == nil || body["prompt"] != nil {
			t.Errorf("native body=%v", body)
		}
		_, _ = w.Write([]byte(`{"output":{"choices":[{"message":{"content":[{"image":"https://example.com/fox.png"}]}}]},"usage":{"output_image_count":1}}`))
	}))
	defer provider.Close()
	out, err := generateQwenImage(context.Background(), Settings{BaseURL: provider.URL + "/api/v1", Model: "qwen-image-3.0"}, "fox", nil)
	if err != nil || !strings.Contains(out, "fox.png") {
		t.Fatalf("native result=%s err=%v", out, err)
	}
}

func TestQwenImageErrorsDoNotRetryOrLeak(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"rate", 429, `{"error":{"message":"synthetic-secret"}}`},
		{"empty", 200, `{"data":[]}`},
		{"unsafe", 200, `{"data":[{"url":"javascript:alert(1)"}]}`},
		{"redirect", 302, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var count atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count.Add(1)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer provider.Close()
			_, err := generateQwenImage(context.Background(), Settings{BaseURL: provider.URL, Model: "qwen-image-3.0", APIKey: "synthetic-secret"}, "fox", nil)
			if err == nil || strings.Contains(err.Error(), "synthetic-secret") || count.Load() != 1 {
				t.Fatalf("error=%v calls=%d", err, count.Load())
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := generateQwenImage(ctx, Settings{BaseURL: "http://127.0.0.1:1", Model: "qwen-image-3.0"}, "fox", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation=%v", err)
	}
}

func TestQwenImageChatSingleRequest(t *testing.T) {
	var count atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		if !strings.HasSuffix(r.URL.Path, "/images/generations") {
			t.Errorf("unexpected auxiliary request: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":[{"url":"https://example.com/generated.png"}]}`))
	}))
	defer provider.Close()
	a := testApp(t)
	a.settings = Settings{BaseURL: provider.URL, Model: "qwen-image-3.0"}
	ts := httptest.NewServer(a.Handler())
	defer ts.Close()
	w := request(a, "POST", "/api/sessions", map[string]string{})
	requireStatus(t, w, 201)
	var s Session
	_ = json.Unmarshal(w.Body.Bytes(), &s)
	w = request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]string{"mode": "chat", "prompt": "draw a fox"})
	requireStatus(t, w, 202)
	var task Task
	_ = json.Unmarshal(w.Body.Bytes(), &task)
	events := readSSE(t, ts.URL+"/api/sessions/"+s.ID+"/runs/"+task.ID+"/events?access_token="+a.token)
	var text strings.Builder
	for _, event := range events {
		if event.Event == "delta" {
			text.WriteString(event.Text)
		}
	}
	if !strings.Contains(text.String(), "generated.png") || count.Load() != 1 {
		t.Fatalf("calls=%d text=%s", count.Load(), text.String())
	}
	w = request(a, "GET", "/api/sessions/"+s.ID, nil)
	_ = json.Unmarshal(w.Body.Bytes(), &s)
	if s.Runs[0].Status != "paused" || s.Runs[0].MediaGeneration == nil || s.Runs[0].MediaGeneration.Output == "" || s.Runs[0].Usage.Total != 0 || len(s.Runs[0].RequestSnapshots) != 1 {
		t.Fatalf("unexpected image run status=%s usage=%+v snapshots=%d", s.Runs[0].Status, s.Runs[0].Usage, len(s.Runs[0].RequestSnapshots))
	}
}
