package tts

// #36 克隆音色 Provider 单测：用 httptest 起一个假克隆服务，验证请求 schema 与降级。

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCloneNotConfigured(t *testing.T) {
	p := newClone(Config{})
	if p.Available() {
		t.Fatal("空 BaseURL 应不可用")
	}
	_, err := p.Synth(context.Background(), "你好", SynthOpts{Voice: "v1"})
	if err != ErrCloneNotConfigured {
		t.Fatalf("未配置应返回 ErrCloneNotConfigured，got %v", err)
	}
}

func TestCloneOpenAICompat(t *testing.T) {
	var gotBody map[string]any
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/audio/speech" {
			t.Errorf("路径应为 /audio/speech，got %s", r.URL.Path)
		}
		_ = decodeJSON(r, &gotBody)
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("ID3fake-mp3-bytes"))
	}))
	defer srv.Close()

	p := newClone(Config{
		CloneBaseURL: srv.URL,
		CloneAPIKey:  "secret-token",
		CloneVoiceID: "voice-abc",
		CloneBackend: "openai",
	})
	rc, err := p.Synth(context.Background(), "你好世界", SynthOpts{Rate: 1.2})
	if err != nil {
		t.Fatalf("Synth 失败：%v", err)
	}
	defer rc.Close()
	b, _ := io.ReadAll(rc)
	if !strings.HasPrefix(string(b), "ID3fake") {
		t.Fatalf("音频流不符：%q", b)
	}
	if gotBody["input"] != "你好世界" {
		t.Errorf("input 未透传：%v", gotBody)
	}
	if gotBody["voice"] != "voice-abc" {
		t.Errorf("voice 应为 voice-abc，got %v", gotBody["voice"])
	}
	if gotBody["response_format"] != "mp3" {
		t.Errorf("response_format 应为 mp3")
	}
	if gotAuth != "Bearer secret-token" {
		t.Errorf("Authorization 头不符：%q", gotAuth)
	}
}

func TestCloneUnreachable(t *testing.T) {
	p := newClone(Config{CloneBaseURL: "http://127.0.0.1:1"}) // 必拒连
	_, err := p.Synth(context.Background(), "你好", SynthOpts{})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("不可达应包装 ErrUnavailable，got %v", err)
	}
}

func TestCloneHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()
	p := newClone(Config{CloneBaseURL: srv.URL})
	_, err := p.Synth(context.Background(), "你好", SynthOpts{})
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("5xx 应包装 ErrUnavailable，got %v", err)
	}
}

func TestChainCloneFallbackToEdge(t *testing.T) {
	// clone 配了但服务 500 → 链应继续到 edge（edge 不可用再报聚合）
	// 这里只验证 clone 入链：Available() 为 true 的 clone 排第一。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()
	cfg := Config{
		Provider:     "clone",
		CloneBaseURL: srv.URL,
		CloneVoiceID: "v1",
	}
	chain := buildChain(cfg)
	if len(chain) < 1 || chain[0].Name() != "clone" {
		t.Fatalf("provider=clone 时链首应为 clone，got %v", chainNames(chain))
	}
}

func TestChainAutoDoesNotIncludeClone(t *testing.T) {
	// auto/edge 链不得包含 clone，避免影响 aide 主聊天机械 TTS
	cfg := Config{Provider: "auto", CloneBaseURL: "http://x"}
	for _, e := range buildChain(cfg) {
		if e.Name() == "clone" {
			t.Fatal("auto 链不应包含 clone")
		}
	}
}

func chainNames(es []TTSProvider) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Name()
	}
	return out
}

func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}
