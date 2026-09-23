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

func TestTaskRecordsModel(t *testing.T) {
	a := testApp(t)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: "ok"}}}})
	}))
	defer provider.Close()
	a.settings = Settings{BaseURL: provider.URL, Model: "deepseek-chat", Models: []ModelRef{{ID: "deepseek-chat", ContextWindow: defaultContextWindow}}, ActiveModel: "deepseek-chat"}
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
	a.settings.APIKey = "old-secret"
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
			if a.settings.BaseURL != "http://old-provider.invalid" || a.settings.APIKey != "old-secret" {
				t.Fatal("discovery persisted draft")
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
