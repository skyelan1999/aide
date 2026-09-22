package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSystemProfilesImmutability(t *testing.T) {
	a := testApp(t)
	// 尝试覆盖系统配置：必须被拒绝
	w := request(a, "PUT", "/api/profiles", map[string]any{
		"strategy":      "manual",
		"activeProfile": "default",
		"profiles": []map[string]any{{
			"id":   "default",
			"name": "篡改",
			"params": map[string]any{"temperature": 0},
		}},
	})
	requireStatus(t, w, 400)
	// 系统配置必须始终可见且不可变
	w = request(a, "GET", "/api/profiles", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"id":"default"`) || !strings.Contains(w.Body.String(), `"id":"precise"`) || !strings.Contains(w.Body.String(), `"id":"creative"`) {
		t.Fatal("system profiles missing")
	}
}

func TestProfileValidation(t *testing.T) {
	a := testApp(t)
	makeBody := func(params map[string]any, strategy, active string) map[string]any {
		return map[string]any{"strategy": strategy, "activeProfile": active, "profiles": []any{map[string]any{"id": "u1", "name": "自定义", "params": params}}}
	}
	for _, extra := range []map[string]any{
		{"temperature": 3},
		{"top_p": 1.5},
		{"max_tokens": 9000},
		{"frequency_penalty": -3},
		{"presence_penalty": 3},
		{"response_format": "xml"},
		{"stop": []string{strings.Repeat("s", 65)}},
	} {
		requireStatus(t, request(a, "PUT", "/api/profiles", makeBody(extra, "manual", "u1")), 400)
	}
	// 系统 id 不可占用
	requireStatus(t, request(a, "PUT", "/api/profiles", map[string]any{"strategy": "manual", "activeProfile": "default", "profiles": []any{map[string]any{"id": "precise", "name": "x", "params": map[string]any{}}}}), 400)
	// 非法策略与未知 activeProfile
	requireStatus(t, request(a, "PUT", "/api/profiles", makeBody(map[string]any{}, "magic", "u1")), 400)
	requireStatus(t, request(a, "PUT", "/api/profiles", makeBody(map[string]any{}, "manual", "ghost")), 400)
	// 合法保存
	requireStatus(t, request(a, "PUT", "/api/profiles", makeBody(map[string]any{}, "manual", "u1")), 200)
}

func TestUserProfilePersistence(t *testing.T) {
	a := testApp(t)
	body := map[string]any{
		"strategy":      "auto",
		"activeProfile": "coder",
		"profiles": []map[string]any{{
			"id":   "coder",
			"name": "代码模式",
			"params": map[string]any{
				"temperature": 0.3, "max_tokens": 8000, "response_format": "json_object", "stop": []string{"END"},
			},
		}},
	}
	requireStatus(t, request(a, "PUT", "/api/profiles", body), 200)
	b, err := os.ReadFile(filepath.Join(a.workPath, "profiles.json"))
	if err != nil || !strings.Contains(string(b), `"coder"`) {
		t.Fatalf("profiles.json not persisted: %s %v", b, err)
	}
	// 重建 App：状态从工程目录恢复
	a2, err := New(a.workPath, a.reference.Name(), a.dataPath)
	if err != nil {
		t.Fatal(err)
	}
	defer a2.Close()
	w := request(a2, "GET", "/api/profiles", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"strategy":"auto"`) || !strings.Contains(w.Body.String(), `"coder"`) {
		t.Fatalf("state not restored: %s", w.Body.String())
	}
}

func TestAutoRoutingPolicy(t *testing.T) {
	a := testApp(t)
	// 先建一个用户配置供路由命中
	requireStatus(t, request(a, "PUT", "/api/profiles", map[string]any{
		"strategy": "auto", "activeProfile": "default",
		"profiles": []map[string]any{{"id": "review", "name": "审查", "params": map[string]any{"temperature": 0.1}}},
	}), 200)
	policy := `{"version":1,"rules":[
		{"when":{"mode":"workflow"},"use":"precise"},
		{"when":{"promptContains":["审查","REVIEW"]},"use":"review"}
	],"default":"default"}`
	if err := os.WriteFile(filepath.Join(a.workPath, "routing-policy.json"), []byte(policy), 0644); err != nil {
		t.Fatal(err)
	}
	id, params, err := a.resolveProfile("auto", "", "帮我审查这段代码", "chat")
	if err != nil || id != "review" || params.Temperature == nil || *params.Temperature != 0.1 {
		t.Fatalf("keyword routing: %s %+v %v", id, params, err)
	}
	id, _, _ = a.resolveProfile("auto", "", "帮我写个功能", "workflow")
	if id != "precise" {
		t.Fatalf("mode routing: %s", id)
	}
	id, _, _ = a.resolveProfile("auto", "", "闲聊", "chat")
	if id != "default" {
		t.Fatalf("fallback routing: %s", id)
	}
	// 删除 json 策略后走 md 兜底
	if err := os.Remove(filepath.Join(a.workPath, "routing-policy.json")); err != nil {
		t.Fatal(err)
	}
	md := "# 策略说明\n按关键词路由。\n```json\n{\"rules\":[{\"when\":{\"promptContains\":[\"文案\"]},\"use\":\"creative\"}],\"default\":\"default\"}\n```\n"
	if err := os.WriteFile(filepath.Join(a.workPath, "routing-policy.md"), []byte(md), 0644); err != nil {
		t.Fatal(err)
	}
	id, _, _ = a.resolveProfile("auto", "", "帮我写个文案", "chat")
	if id != "creative" {
		t.Fatalf("md fallback routing: %s", id)
	}
	// 全部缺失 → default
	_ = os.Remove(filepath.Join(a.workPath, "routing-policy.md"))
	id, _, _ = a.resolveProfile("auto", "", "x", "chat")
	if id != "default" {
		t.Fatalf("no-policy fallback: %s", id)
	}
	// 手动策略 + 未知 profile → 报错
	if _, _, err := a.resolveProfile("manual", "ghost", "x", "chat"); err == nil {
		t.Fatal("unknown manual profile accepted")
	}
}

func TestProfileParamsReachProvider(t *testing.T) {
	var got map[string]any
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = map[string]any{}
		_ = json.Unmarshal(b, &got)
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: "ok"}}}})
	}))
	defer provider.Close()
	params := ProfileParams{Temperature: fp(0.5), TopP: fp(0.8), MaxTokens: 1234, FrequencyPenalty: fp(0.1), PresencePenalty: fp(-0.2), ResponseFormat: "json_object", Stop: []string{"END"}}
	if _, _, err := complete(context.Background(), Settings{BaseURL: provider.URL, Model: "deepseek-chat"}, nil, params, nil); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{"temperature": 0.5, "top_p": 0.8, "max_tokens": float64(1234), "frequency_penalty": 0.1, "presence_penalty": -0.2} {
		if got[key] != want {
			t.Fatalf("%s = %v want %v", key, got[key], want)
		}
	}
	if fmt, ok := got["response_format"].(map[string]any); !ok || fmt["type"] != "json_object" {
		t.Fatal("response_format missing")
	}
	// reasoner：不支持参数被剔除
	if _, _, err := complete(context.Background(), Settings{BaseURL: provider.URL, Model: "deepseek-reasoner"}, nil, params, nil); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"temperature", "top_p", "frequency_penalty", "presence_penalty"} {
		if _, ok := got[key]; ok {
			t.Fatalf("reasoner must drop %s", key)
		}
	}
	if got["max_tokens"] != float64(1234) {
		t.Fatal("reasoner keeps max_tokens")
	}
}

func TestTaskRecordsStrategyAndProfile(t *testing.T) {
	a := testApp(t)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: "回复"}}}})
	}))
	defer provider.Close()
	a.settings = Settings{BaseURL: provider.URL, Model: "test"}
	w := request(a, "POST", "/api/sessions", map[string]string{})
	requireStatus(t, w, 201)
	var s Session
	_ = json.Unmarshal(w.Body.Bytes(), &s)
	// 非法策略拒绝（此时无运行中任务）
	w = request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{"mode": "chat", "prompt": "hi", "strategy": "magic"})
	requireStatus(t, w, 400)
	// 手动未知 profile 拒绝
	w = request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{"mode": "chat", "prompt": "hi", "strategy": "manual", "profile": "ghost"})
	requireStatus(t, w, 400)
	w = request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{"mode": "chat", "prompt": "hi", "strategy": "auto", "profile": ""})
	requireStatus(t, w, 202)
	var task Task
	_ = json.Unmarshal(w.Body.Bytes(), &task)
	if task.Strategy != "auto" || task.Profile != "default" {
		t.Fatalf("task routing record: %+v", task)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		w = request(a, "GET", "/api/sessions/"+s.ID, nil)
		_ = json.Unmarshal(w.Body.Bytes(), &s)
		if s.Runs[0].Status != "running" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestVersionFromVersionFile(t *testing.T) {
	// 直接测解析器：缺失 / 非法 / 合法
	if v := readVersionFile(filepath.Join(t.TempDir(), "missing.md")); v != "" {
		t.Fatalf("missing file must give empty version, got %q", v)
	}
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.md")
	if err := os.WriteFile(bad, []byte("没有版本号"), 0644); err != nil {
		t.Fatal(err)
	}
	if v := readVersionFile(bad); v != "" {
		t.Fatalf("invalid file must give empty version, got %q", v)
	}
	good := filepath.Join(dir, "version.md")
	if err := os.WriteFile(good, []byte("# aide 版本记录\n\n**当前版本：0.1.0.0 RC1**\n\n## 0.1.0.0 RC1（2026-09-21）\n\n- 初始版本\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if v := readVersionFile(good); v != "0.1.0.0 RC1" {
		t.Fatalf("want 0.1.0.0 RC1, got %q", v)
	}
	// 端到端：New() 加载 version.md → /api/config 返回；缺失时返回空
	root := t.TempDir()
	work := filepath.Join(root, "work")
	if err := os.MkdirAll(work, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "version.md"), []byte("**当前版本：0.2.3.4 RC2**\n"), 0644); err != nil {
		t.Fatal(err)
	}
	a, err := New(work, filepath.Join(root, "ref"), filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	w := request(a, "GET", "/api/config", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"version":"0.2.3.4 RC2"`) {
		t.Fatalf("config missing version: %s", w.Body.String())
	}
	a2 := testApp(t)
	w = request(a2, "GET", "/api/config", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"version":""`) {
		t.Fatalf("missing version.md must yield empty version: %s", w.Body.String())
	}
}
