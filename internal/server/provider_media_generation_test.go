package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMediaGenerationIntent(t *testing.T) {
	for text, want := range map[string]string{"帮我生成一个三秒的庆贺视频，当前仅用于测试，具体文案你自定": "video", "生成一个3秒的庆贺视频，仅用于接口测试。": "video",
		"生成 MP3，内容：你好，测试成功。": "speech", "请帮我画一个阿狸的图片": "image", "你语音是不是可以生成mp3": "", "帮我写生成视频的代码": "", "生成图片识别的测试方案": "", "分析视频": "", "generate an image of a fox": "image"} {
		if got := generationIntent(text); got != want {
			t.Errorf("%q: %s want %s", text, got, want)
		}
	}
	for _, raw := range []string{"https://example.com/x.mp3", "https://dashscope-result-bj.oss-cn-beijing.aliyuncs.com.evil.invalid/a.mp3", "http://127.0.0.1/a.mp3", "https://user:secret@dashscope-result-bj.oss-cn-beijing.aliyuncs.com/a.mp3"} {
		if _, err := mediaDownloadURL(raw); err == nil {
			t.Errorf("unsafe download accepted: %s", raw)
		}
	}
	if got, err := mediaDownloadURL("http://dashscope-result-bj.oss-cn-beijing.aliyuncs.com/a.mp3"); err != nil || !strings.HasPrefix(got, "https://") {
		t.Fatal(got, err)
	}
	if mediaFileMatches([]byte("RIFFxxxxWAVE"), "mp3") {
		t.Fatal("WAV mislabeled as MP3")
	}
	if duration, err := mediaVideoDuration("生成三秒视频"); duration != 3 || err != nil {
		t.Fatal(duration, err)
	}
	if _, err := mediaVideoDuration("生成 60 秒视频"); err == nil {
		t.Fatal("duration not bounded")
	}
}
func TestNativeMediaProtocolsAndResume(t *testing.T) {
	posts, queries, saves := 0, 0, 0
	mode := "video"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-key" {
			t.Error("wrong key")
		}
		if r.Method == "GET" {
			queries++
			jsonOut(w, 200, map[string]any{"output": map[string]any{"task_status": "SUCCEEDED", "video_url": "https://dashscope-result-sh.oss-accelerate.aliyuncs.com/test.mp4"}})
			return
		}
		posts++
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if mode == "video" {
			p := body["parameters"].(map[string]any)
			if p["duration"] != float64(3) || p["resolution"] != "720P" || p["prompt_extend"] != false || r.Header.Get("X-DashScope-Async") != "enable" {
				t.Errorf("bad video request %#v", body)
			}
			jsonOut(w, 200, map[string]any{"output": map[string]any{"task_id": "fixture-job"}})
		} else {
			in := body["input"].(map[string]any)
			if in["text"] != "你好" || in["format"] != "mp3" || r.URL.Path != "/api/v1/services/audio/tts/SpeechSynthesizer" {
				t.Errorf("bad speech request %#v", body)
			}
			jsonOut(w, 200, map[string]any{"output": map[string]any{"audio": map[string]any{"url": "https://dashscope-result-bj.oss-cn-beijing.aliyuncs.com/test.mp3"}}})
		}
	}))
	defer server.Close()
	cfg := Settings{BaseURL: server.URL + "/compatible-mode/v1", Model: "wan2.7-t2v", APIKey: "fixture-key"}
	state := MediaGeneration{Kind: "video", Model: cfg.Model, Identity: mediaIdentity(cfg), Format: "mp4"}
	save := func() error {
		saves++
		if saves == 1 && state.State != "submitting" {
			t.Error("did not persist intent before POST")
		}
		return nil
	}
	if err := generateNativeMedia(context.Background(), cfg, "生成三秒视频", &state, save, nil, server.Client(), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if posts != 1 || queries != 1 || state.JobID != "fixture-job" {
		t.Fatal(posts, queries, state)
	}
	state.ResultURL = "" // Resume the same remote job, never create it again.
	if err := generateNativeMedia(context.Background(), cfg, "生成三秒视频", &state, save, nil, server.Client(), time.Millisecond); err != nil || posts != 1 || queries != 2 {
		t.Fatal(err, posts, queries)
	}
	state = MediaGeneration{Kind: "video", Model: cfg.Model, Identity: mediaIdentity(cfg), State: "submitting"}
	if err := generateNativeMedia(context.Background(), cfg, "生成三秒视频", &state, save, nil, server.Client(), time.Millisecond); err == nil || posts != 1 {
		t.Fatal("ambiguous submission repeated")
	}
	cfg.Model = "qwen-audio-3.0-tts-flash"
	mode = "speech"
	state = MediaGeneration{Kind: "speech", Model: cfg.Model, Identity: mediaIdentity(cfg), Format: "mp3"}
	if err := generateNativeMedia(context.Background(), cfg, "生成MP3，内容：你好", &state, save, nil, server.Client(), time.Millisecond); err != nil || posts != 2 {
		t.Fatal(err, posts)
	}
}
func TestNativeMediaCancellationAndPersistenceFailure(t *testing.T) {
	posts := 0
	ctx, cancel := context.WithCancel(context.Background())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts++
			jsonOut(w, 200, map[string]any{"output": map[string]any{"task_id": "fixture-job"}})
		} else {
			cancel()
			jsonOut(w, 200, map[string]any{"output": map[string]any{"task_status": "RUNNING"}})
		}
	}))
	defer server.Close()
	cfg := Settings{BaseURL: server.URL, Model: "wan2.7-t2v", APIKey: "fixture"}
	state := MediaGeneration{Kind: "video", Model: cfg.Model, Identity: mediaIdentity(cfg)}
	if err := generateNativeMedia(ctx, cfg, "视频", &state, func() error { return nil }, nil, server.Client(), time.Hour); !errors.Is(err, context.Canceled) || state.JobID == "" || posts != 1 {
		t.Fatal(err, state, posts)
	}
	state = MediaGeneration{Kind: "video", Model: cfg.Model, Identity: mediaIdentity(cfg)}
	if err := generateNativeMedia(context.Background(), cfg, "视频", &state, func() error { return errors.New("disk full") }, nil, server.Client(), time.Millisecond); err == nil || posts != 1 {
		t.Fatal("POST despite failed checkpoint", err, posts)
	}
}
func TestAutoMediaRouteOutputWeights(t *testing.T) {
	a := testApp(t)
	routerFixture(t, a)
	path := filepath.Join(a.pluginsPath, "router-fixture", "capabilities.json")
	raw, _ := os.ReadFile(path)
	var catalog routerCatalog
	json.Unmarshal(raw, &catalog)
	for _, id := range []string{"qwen-image-3.0", "qwen-audio-3.0-tts-flash", "wan2.7-t2v"} {
		output := generationModelOutput(id)
		catalog.Models = append(catalog.Models, routerProfile{ID: id, Aliases: []string{id}, Output: output, ContextWindow: 65536, Weights: map[string]int{"general": 1, "coding": 1, "reasoning": 1, "writing": 1, "fast": 3, output: 5}, Source: "fixture"})
	}
	raw, _ = json.Marshal(catalog)
	os.WriteFile(path, raw, 0644)
	cfg := routerSettings(t, a, "https://dashscope.aliyuncs.com/compatible-mode/v1", "https://dashscope.aliyuncs.com/compatible-mode/v1")
	for _, id := range []string{"qwen-image-3.0", "qwen-audio-3.0-tts-flash", "wan2.7-t2v"} {
		cfg.Models = append(cfg.Models, ModelRef{ID: id, ProviderID: "b"})
	}
	for prompt, want := range map[string]string{"生成一张图片": "qwen-image-3.0", "生成MP3，内容：你好": "qwen-audio-3.0-tts-flash", "生成三秒视频": "wan2.7-t2v"} {
		task := &Task{Prompt: prompt, Mode: "chat", ModelSelection: &SessionModelSelection{ModelRouter: "auto"}}
		a.mu.Lock()
		selected, d := a.routeModelLocked(task, cfg, nil, ProfileParams{}, nil, "initial")
		a.mu.Unlock()
		if selected.Model != want || d.Score != 503 || len(d.Candidates) != 1 {
			t.Fatal(selected.Model, d)
		}
	}
	task := &Task{Prompt: "生成三秒视频", ModelSelection: &SessionModelSelection{ModelRouter: "auto"}}
	cfg.Models = cfg.Models[:2]
	selected, _ := a.routeModelLocked(task, cfg, nil, ProfileParams{}, nil, "initial")
	if selected.generationError == "" || !mediaTask(selected) {
		t.Fatal("silent text fallback")
	}
}

func TestTTSToolsPreserveAssistantSettings(t *testing.T) {
	a := testApp(t)
	a.settings.TTSProvider = "azure"
	a.settings.TTSVoice = "assistant-voice"
	a.settings.TTSAPIKey = "assistant-only-key"
	task := &Task{Mode: "chat"}
	if _, err := a.ttsGenerationTool(context.Background(), task, map[string]any{"text": "你好"}); err == nil {
		t.Fatal("disabled plugin executed")
	}
	a.pluginSurface = []byte(`{"plugins":[{"id":"tts-tools","tools":[{"name":"tts_generate","executable":true},{"name":"tts_voices","executable":true}]}]}`)
	if _, err := a.ttsGenerationTool(context.Background(), task, map[string]any{"text": "你好", "format": "exe"}); err == nil {
		t.Fatal("invalid codec accepted")
	}
	if _, err := a.ttsGenerationTool(context.Background(), task, map[string]any{"text": "你好", "model": "unknown"}); err == nil {
		t.Fatal("unconfigured model accepted")
	}
	if a.settings.TTSProvider != "azure" || a.settings.TTSVoice != "assistant-voice" || a.settings.TTSAPIKey != "assistant-only-key" {
		t.Fatal("assistant defaults changed")
	}
	a.settings.SandboxMode = "read-only"
	if a.sandboxToolError("tts_generate") == nil || a.sandboxToolError("tts_voices") != nil {
		t.Fatal("TTS write/read sandbox boundaries")
	}
}

func TestGeneratedMediaRawMIMEAndRange(t *testing.T) {
	a := testApp(t)
	dir := filepath.Join(a.workPath, "generated", "speech")
	os.MkdirAll(dir, 0700)
	os.WriteFile(filepath.Join(dir, "qwen-abc123.mp3"), []byte("ID3-test-audio"), 0600)
	r := httptest.NewRequest("GET", "/api/file/raw?root=workspace&path=generated/speech/qwen-abc123.mp3", nil)
	r.Header.Set("Range", "bytes=0-2")
	w := httptest.NewRecorder()
	a.readFileRaw(w, r)
	if w.Code != 206 || w.Body.String() != "ID3" || w.Header().Get("Content-Type") != "audio/mpeg" || w.Header().Get("Accept-Ranges") != "bytes" {
		t.Fatal(w.Code, w.Header(), w.Body.String())
	}
	if kind, _ := generatedMediaRawType("generated/speech/../../secret.mp3"); kind != "" {
		t.Fatal("unsafe media path")
	}
}
