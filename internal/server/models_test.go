package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMultiModelSettings(t *testing.T) {
	a := testApp(t)
	// 添加多模型并标记当前
	w := request(a, "PUT", "/api/settings", map[string]any{
		"baseURL":     "https://api.example.com",
		"models":      []any{map[string]any{"id": "m1", "name": "模型一", "contextWindow": 128000}, map[string]any{"id": "m2"}},
		"activeModel": "m2",
	})
	requireStatus(t, w, 200)
	w = request(a, "GET", "/api/config", nil)
	requireStatus(t, w, 200)
	var cfg map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &cfg)
	if cfg["activeModel"] != "m2" || cfg["model"] != "m2" {
		t.Fatalf("active model mismatch: %v", cfg)
	}
	models := cfg["models"].([]any)
	if len(models) != 2 {
		t.Fatalf("want 2 models, got %d", len(models))
	}
	first := models[0].(map[string]any)
	if first["name"] != "模型一" || first["contextWindow"] != float64(128000) {
		t.Fatalf("model fields wrong: %v", first)
	}
	second := models[1].(map[string]any)
	if second["name"] != "m2" || second["contextWindow"] != float64(65536) {
		t.Fatalf("defaults not applied: %v", second)
	}
	// Capability declarations and their provenance survive settings normalization and config output.
	w = request(a, "PUT", "/api/settings", map[string]any{
		"models": []any{map[string]any{
			"id": "m1", "capabilities": map[string]string{"tool_calls": "supported", "vision": "unsupported"},
			"capabilitiesSource": "provider docs", "capabilitiesChecked": "2026-10-06",
		}}, "activeModel": "m1",
	})
	requireStatus(t, w, 200)
	w = request(a, "GET", "/api/config", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"tool_calls":"supported"`) || !strings.Contains(w.Body.String(), `"vision":"unsupported"`) || !strings.Contains(w.Body.String(), `"capabilitiesSource":"provider docs"`) {
		t.Fatalf("capability metadata did not round-trip: %s", w.Body.String())
	}
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{
		"models": []any{map[string]any{"id": "m1", "capabilities": map[string]string{"vision": "maybe"}}}, "activeModel": "m1",
	}), 400)
	// 越界校验
	for _, bad := range []map[string]any{
		{"baseURL": "https://api.example.com", "models": []any{map[string]any{"id": ""}}, "activeModel": "m2"},
		{"baseURL": "https://api.example.com", "models": []any{map[string]any{"id": "x", "contextWindow": 100}}, "activeModel": "x"},
		{"baseURL": "https://api.example.com", "models": []any{map[string]any{"id": "x"}}, "activeModel": "ghost"},
		{"baseURL": "https://api.example.com", "models": []any{map[string]any{"id": "x"}, map[string]any{"id": "x"}}, "activeModel": "x"},
	} {
		requireStatus(t, request(a, "PUT", "/api/settings", bad), 400)
	}
	// 只切 activeModel（不送 baseURL/key/models）→ 用已存值补全
	w = request(a, "PUT", "/api/settings", map[string]any{"activeModel": "m1"})
	requireStatus(t, w, 200)
	w = request(a, "GET", "/api/config", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"activeModel":"m1"`) {
		t.Fatalf("partial update failed: %s", w.Body.String())
	}
	// 未配置任何模型时仍拒绝
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{"baseURL": "https://api.example.com", "models": []any{}, "activeModel": ""}), 400)
}

func TestReasoningEffortCanBeUpdatedFromSettings(t *testing.T) {
	a := testApp(t)
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{
		"baseURL": "https://api.example.com", "models": []any{map[string]any{"id": "reasoning-test"}}, "activeModel": "reasoning-test",
	}), http.StatusOK)
	for _, effort := range []string{"high", "medium", "off", "auto"} {
		w := request(a, "PUT", "/api/settings", map[string]any{"reasoningEffort": effort})
		requireStatus(t, w, http.StatusOK)
		w = request(a, "GET", "/api/config", nil)
		requireStatus(t, w, http.StatusOK)
		var cfg map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
			t.Fatal(err)
		}
		if cfg["reasoningEffort"] != effort {
			t.Fatalf("reasoning effort did not persist: want %q, config=%v", effort, cfg["reasoningEffort"])
		}
	}
}

func TestSettingsMigrationFromSingleModel(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "data")
	if err := os.MkdirAll(data, 0700); err != nil {
		t.Fatal(err)
	}
	// 旧格式：只有 baseURL/model/apiKey
	old := `{"baseURL":"https://api.example.com","model":"legacy-model","apiKey":"secret"}`
	if err := os.WriteFile(filepath.Join(data, "settings.json"), []byte(old), 0600); err != nil {
		t.Fatal(err)
	}
	a, err := New(filepath.Join(root, "work"), filepath.Join(root, "ref"), data)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	w := request(a, "GET", "/api/config", nil)
	requireStatus(t, w, 200)
	body := w.Body.String()
	if !strings.Contains(body, `"model":"legacy-model"`) || !strings.Contains(body, `"activeModel":"legacy-model"`) || !strings.Contains(body, `"id":"legacy-model"`) {
		t.Fatalf("migration failed: %s", body)
	}
	if !strings.Contains(body, `"hasKey":true`) {
		t.Fatal("key lost during migration")
	}
}

func TestLegacySingleModelSettingsWriteUsesDefaultProvider(t *testing.T) {
	a := testApp(t)
	w := request(a, "PUT", "/api/settings", map[string]any{
		"baseURL": "https://api.example.com/v1",
		"model":   "legacy-chat",
	})
	requireStatus(t, w, 200)

	w = request(a, "GET", "/api/config", nil)
	requireStatus(t, w, 200)
	var cfg struct {
		ActiveModel    string          `json:"activeModel"`
		Models         []ModelRef      `json:"models"`
		ModelProviders []ModelProvider `json:"modelProviders"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.ActiveModel != "legacy-chat" || len(cfg.Models) != 1 || cfg.Models[0].ProviderID != "default" {
		t.Fatalf("legacy model was not attached to the default provider: %+v", cfg)
	}
	if len(cfg.ModelProviders) != 1 || cfg.ModelProviders[0].ID != "default" || cfg.ModelProviders[0].BaseURL != "https://api.example.com/v1" {
		t.Fatalf("legacy provider configuration was not preserved: %+v", cfg.ModelProviders)
	}
}

func TestModelProviderReasoningFormatSettingsRoundTrip(t *testing.T) {
	a := testApp(t)
	providerURL := "http://127.0.0.1:8787/v1"
	w := request(a, "PUT", "/api/settings", map[string]any{
		"baseURL": providerURL,
		"modelProviders": []any{map[string]any{
			"id": "codex", "name": "Codex Bridge", "baseURL": providerURL,
			"protocol": "openai-compatible", "reasoningEffortFormat": "codex-bridge",
			"keys": []any{map[string]any{"id": "primary", "label": "Primary"}}, "activeKeyId": "primary",
		}},
		"models":      []any{map[string]any{"id": "gpt-6-luna", "providerId": "codex"}},
		"activeModel": "gpt-6-luna",
	})
	requireStatus(t, w, 200)

	w = request(a, "GET", "/api/config", nil)
	requireStatus(t, w, 200)
	var cfg struct {
		ModelProviders []ModelProvider `json:"modelProviders"`
		Models         []ModelRef      `json:"models"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.ModelProviders) != 1 || cfg.ModelProviders[0].ReasoningEffortFormat != "codex-bridge" {
		t.Fatalf("provider reasoning format did not round-trip through /api/config: %+v", cfg.ModelProviders)
	}
	if len(cfg.Models) != 1 || cfg.Models[0].ProviderID != "codex" {
		t.Fatalf("model did not retain selected provider: %+v", cfg.Models)
	}
	settings, err := a.selectionSettingsLocked(SessionModelSelection{Strategy: "auto", Profile: "default", Model: "gpt-6-luna", ReasoningEffort: "high"})
	if err != nil {
		t.Fatal(err)
	}
	if !providerUsesOpenAIReasoningEffort(settings) {
		t.Fatal("selected model settings did not retain the provider reasoning format")
	}
	for _, effort := range []string{"minimal", "ultra"} {
		settings, err := a.selectionSettingsLocked(SessionModelSelection{Strategy: "auto", Profile: "default", Model: "gpt-6-luna", ReasoningEffort: effort})
		if err != nil {
			t.Fatalf("session selection rejected Codex Bridge effort %q: %v", effort, err)
		}
		body := buildChatBody(settings, nil, ProfileParams{}, nil, false)
		if body["reasoning_effort"] != effort {
			t.Fatalf("session selection failed to pass Codex Bridge effort %q: %#v", effort, body)
		}
	}
}

func TestModelListProxy(t *testing.T) {
	a := testApp(t)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			w.WriteHeader(404)
			return
		}
		if r.Header.Get("Authorization") != "Bearer fake-key" {
			t.Error("auth header not forwarded")
		}
		jsonOut(w, 200, map[string]any{"object": "list", "data": []any{
			map[string]any{"id": "deepseek-chat"}, map[string]any{"id": "deepseek-reasoner"},
		}})
	}))
	defer provider.Close()
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{"baseURL": provider.URL, "apiKey": "fake-key", "models": []any{map[string]any{"id": "deepseek-chat"}}, "activeModel": "deepseek-chat"}), 200)
	w := request(a, "GET", "/api/models", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), "deepseek-reasoner") || !strings.Contains(w.Body.String(), "deepseek-chat") {
		t.Fatalf("models list: %s", w.Body.String())
	}
	// 上游失败 → 400 友好提示
	failProvider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer failProvider.Close()
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{"baseURL": failProvider.URL, "activeModel": "deepseek-chat"}), 200)
	w = request(a, "GET", "/api/models", nil)
	requireStatus(t, w, 400)
	if !strings.Contains(w.Body.String(), "不支持") {
		t.Fatalf("error message: %s", w.Body.String())
	}
}

func TestMultipleModelProvidersUseTheirOwnVaultKeys(t *testing.T) {
	keyA, keyB := "provider-a-secret", "provider-b-secret"
	providerA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("provider A unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+keyA {
			t.Errorf("provider A received authorization %q", got)
		}
		jsonOut(w, 200, map[string]any{"data": []any{map[string]any{"id": "model-a"}}})
	}))
	defer providerA.Close()
	providerB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("provider B unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+keyB {
			t.Errorf("provider B received authorization %q", got)
		}
		jsonOut(w, 200, map[string]any{"data": []any{map[string]any{"id": "model-b"}}})
	}))
	defer providerB.Close()

	a := testApp(t)
	w := request(a, "PUT", "/api/settings", map[string]any{
		"baseURL": providerA.URL,
		"modelProviders": []any{
			map[string]any{"id": "default", "name": "Provider A", "baseURL": providerA.URL, "protocol": "openai-compatible", "keys": []any{map[string]any{"id": "legacy", "label": "Primary"}}, "activeKeyId": "legacy"},
			map[string]any{"id": "provider-b", "name": "Provider B", "baseURL": providerB.URL, "protocol": "openai-compatible", "keys": []any{map[string]any{"id": "primary", "label": "Primary"}}, "activeKeyId": "primary"},
		},
		"providerKeyWrites": []any{
			map[string]any{"providerId": "default", "keyId": "legacy", "apiKey": keyA},
			map[string]any{"providerId": "provider-b", "keyId": "primary", "apiKey": keyB},
		},
		"models":      []any{map[string]any{"id": "model-a", "providerId": "default"}, map[string]any{"id": "model-b", "providerId": "provider-b"}},
		"activeModel": "model-b",
	})
	requireStatus(t, w, 200)
	w = request(a, "GET", "/api/config", nil)
	requireStatus(t, w, 200)
	if strings.Contains(w.Body.String(), keyA) || strings.Contains(w.Body.String(), keyB) {
		t.Fatal("provider credentials leaked through /api/config")
	}
	if !strings.Contains(w.Body.String(), `"providerId":"provider-b"`) || !strings.Contains(w.Body.String(), `"hasKey":true`) {
		t.Fatalf("provider/model metadata missing from /api/config: %s", w.Body.String())
	}
	w = request(a, "GET", "/api/models", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), "model-b") {
		t.Fatalf("active provider model discovery failed: %s", w.Body.String())
	}
	w = request(a, "PUT", "/api/settings", map[string]any{"activeModel": "model-a"})
	requireStatus(t, w, 200)
	w = request(a, "GET", "/api/models", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), "model-a") {
		t.Fatalf("switched provider model discovery failed: %s", w.Body.String())
	}
}

func TestTaskRecordsModel(t *testing.T) {
	a := testApp(t)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: "ok"}}}})
	}))
	defer provider.Close()
	a.mu.Lock()
	a.settings = Settings{BaseURL: provider.URL, Model: "deepseek-chat", Models: []ModelRef{{ID: "deepseek-chat", ContextWindow: defaultContextWindow}}, ActiveModel: "deepseek-chat"}
	a.mu.Unlock()
	w := request(a, "POST", "/api/sessions", map[string]string{})
	requireStatus(t, w, 201)
	var s Session
	_ = json.Unmarshal(w.Body.Bytes(), &s)
	w = request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{"mode": "chat", "prompt": "hi"})
	requireStatus(t, w, 202)
	var task Task
	_ = json.Unmarshal(w.Body.Bytes(), &task)
	if task.Model != "deepseek-chat" {
		t.Fatalf("task model not recorded: %+v", task)
	}
	// 等待异步 execute 协程结束，避免 testApp cleanup 调 Close() 时与仍在运行的
	// background()/summarizeTopic 协程竞争 bgWg/bgCtx（data race）。
	waitTaskDone(t, a, s.ID)
}

func TestModelDiscoveryDraft(t *testing.T) {
	var auth string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		auth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"draft-model"}]}`))
	}))
	defer provider.Close()
	a := testApp(t)
	a.settings.BaseURL = "http://old-provider.invalid"
	a.mu.Lock()
	a.storeModelAPIKeyPlaintextLocked("old-secret") // key 存加密 vault
	a.mu.Unlock()
	for _, tc := range []struct {
		name, base, key string
		clear           bool
		want            string
	}{
		{"new address no old key", provider.URL, "", false, ""},
		{"draft key", provider.URL, "new-secret", false, "Bearer new-secret"},
		{"clear key", provider.URL, "ignored", true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := request(a, "POST", "/api/models", map[string]any{"baseURL": tc.base, "apiKey": tc.key, "clearKey": tc.clear})
			requireStatus(t, w, 200)
			if auth != tc.want {
				t.Fatal("incorrect credential selection")
			}
			if !strings.Contains(w.Body.String(), "draft-model") {
				t.Fatal("missing discovered model")
			}
			if a.settings.BaseURL != "http://old-provider.invalid" || !a.hasModelAPIKey() {
				t.Fatal("discovery should not clobber saved baseURL/key")
			}
		})
	}
	a.settings.BaseURL = provider.URL
	w := request(a, "POST", "/api/models", map[string]any{"baseURL": provider.URL + "/"})
	requireStatus(t, w, 200)
	if auth != "Bearer old-secret" {
		t.Fatal("same endpoint should retain saved key")
	}
	w = request(a, "POST", "/api/models", map[string]any{"baseURL": "file:///etc/passwd"})
	requireStatus(t, w, 400)
}
