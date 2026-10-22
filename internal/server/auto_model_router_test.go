package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func routerFixture(t *testing.T, a *App) {
	t.Helper()
	a.pluginsPath = filepath.Join(a.workPath, "plugins")
	dir := filepath.Join(a.pluginsPath, "router-fixture")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	c := routerCatalog{Protocol: 1, Revision: "fixture-1", CheckedAt: "2026-10-22", Sources: []routerSource{{ID: "fixture", URL: "https://example.invalid/model-card", Assessment: "Synthetic fixture only"}}, Rules: []routerRule{{Role: "coding", Keywords: []string{"code", "implement"}}, {Role: "writing", Keywords: []string{"write", "document"}}}}
	for _, id := range []string{"code-model", "write-model"} {
		weights := map[string]int{"coding": 1, "reasoning": 1, "writing": 1, "general": 3, "fast": 3}
		if id == "code-model" {
			weights["coding"] = 5
			weights["reasoning"] = 5
		} else {
			weights["writing"] = 5
		}
		c.Models = append(c.Models, routerProfile{ID: id, Aliases: []string{id}, ContextWindow: 65536, Tools: true, Structured: true, Weights: weights, Source: "fixture"})
	}
	raw, _ := json.Marshal(c)
	if err := os.WriteFile(filepath.Join(dir, "capabilities.json"), raw, 0644); err != nil {
		t.Fatal(err)
	}
	a.pluginRegistry.Plugins = []PluginManifest{{ID: "router-fixture", Enabled: true, ModelRouter: &PluginModelRouter{1, "capabilities.json"}}}
}
func routerSettings(t *testing.T, a *App, urlA, urlB string) Settings {
	t.Helper()
	a.settings = Settings{Model: "code-model", BaseURL: urlA, Models: []ModelRef{{ID: "code-model", ProviderID: "a", ContextWindow: 65536}, {ID: "write-model", ProviderID: "b", ContextWindow: 65536}}, ModelProviders: []ModelProvider{{ID: "a", BaseURL: urlA, ActiveKeyID: "fixture"}, {ID: "b", BaseURL: urlB, ActiveKeyID: "fixture"}}}
	for _, id := range []string{"a", "b"} {
		if err := a.vault.Put(modelProviderVaultID(id, "fixture"), VaultTypeModelAPIKey, "fixture", []byte("router-key-"+id), ""); err != nil {
			t.Fatal(err)
		}
	}
	cfg, _ := settingsForSelectedModel(a.settings, "code-model")
	cfg.APIKey = "router-key-a"
	return cfg
}
func TestAutoModelRouterOfflineGuards(t *testing.T) {
	a := testApp(t)
	routerFixture(t, a)
	cfg := routerSettings(t, a, "http://a.invalid", "http://b.invalid")
	task := &Task{Prompt: "write document", ModelSelection: &SessionModelSelection{ModelRouter: "auto"}}
	input := []Message{{Role: "user", Content: "write document"}}
	params := ProfileParams{MaxTokens: 1024}
	a.mu.Lock()
	chosen, d := a.routeModelLocked(task, cfg, input, params, nil, "chat")
	a.mu.Unlock()
	if chosen.Model != "write-model" || chosen.BaseURL != "http://b.invalid" || chosen.APIKey != "router-key-b" || d.Role != "writing" {
		t.Fatalf("route/key isolation failed: %+v", d)
	}
	// Host capability opt-outs and model windows override optimistic catalog data.
	cfg.Models[1].ContextWindow = 1024
	a.mu.Lock()
	chosen, _ = a.routeModelLocked(task, cfg, input, params, nil, "chat")
	a.mu.Unlock()
	if chosen.Model != "code-model" {
		t.Fatal("small context accepted")
	}
	cfg.Models[1].ContextWindow = 65536
	cfg.Models[1].Capabilities = map[string]string{"tool_calls": "unsupported"}
	a.mu.Lock()
	chosen, _ = a.routeModelLocked(task, cfg, input, params, []any{map[string]any{"type": "function"}}, "chat")
	a.mu.Unlock()
	if chosen.Model != "code-model" {
		t.Fatal("host tool opt-out ignored")
	}
	a.pluginRegistry.Plugins[0].Enabled = false
	if got := a.routedModel(task, cfg, input, params, nil, "chat"); got.Model != cfg.Model {
		t.Fatal("disabled plugin routed")
	}
	a.pluginRegistry.Plugins[0].Enabled = true
	// Unknown near-match must not acquire a known profile by substring/fuzzy name.
	cfg.Models[1].Capabilities = nil
	cfg.Models[1].ID = "not-write-model-preview"
	if got := a.routedModel(task, cfg, input, params, nil, "chat"); got.Model != "code-model" {
		t.Fatal("unknown name matched")
	}
	raw, _ := json.Marshal(task.ModelRoutes)
	if strings.Contains(string(raw), "router-key") {
		t.Fatal("key leaked to route history")
	}
	dir := filepath.Join(a.pluginsPath, "router-fixture")
	if err := os.Remove(filepath.Join(dir, "capabilities.json")); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.json")
	os.WriteFile(outside, []byte(`{}`), 0600)
	if err := os.Symlink(outside, filepath.Join(dir, "capabilities.json")); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	catalog, _ := a.modelRouterLocked()
	a.mu.Unlock()
	if catalog != nil {
		t.Fatal("outside symlink accepted")
	}
}
func TestAutoModelRouterParallelProviders(t *testing.T) {
	a, s, task := parallelFixture(t, 2)
	routerFixture(t, a)
	task.Prompt = "code and write document"
	task.ModelSelection = &SessionModelSelection{ModelRouter: "auto"}
	started := make(chan string, 2)
	release := make(chan struct{})
	var active, maximum atomic.Int32
	var mu sync.Mutex
	seen := map[string]int{}
	provider := func(model, key string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Model    string    `json:"model"`
				Messages []Message `json:"messages"`
				Tools    []any     `json:"tools"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			if body.Model != model || r.Header.Get("Authorization") != "Bearer "+key {
				t.Error("provider/model/key crossed")
			}
			if len(body.Tools) != 0 {
				t.Error("analysis worker received tools")
			}
			last := body.Messages[len(body.Messages)-1].Content
			if strings.HasPrefix(last, "[parallel planner]") {
				parallelReply(w, `{"tasks":["implement code","write document"]}`)
				return
			}
			mu.Lock()
			seen[model]++
			mu.Unlock()
			n := active.Add(1)
			for old := maximum.Load(); n > old; old = maximum.Load() {
				if maximum.CompareAndSwap(old, n) {
					break
				}
			}
			started <- model
			select {
			case <-release:
			case <-r.Context().Done():
				active.Add(-1)
				return
			}
			active.Add(-1)
			parallelReply(w, "working notes")
		}))
	}
	pa, pb := provider("code-model", "router-key-a"), provider("write-model", "router-key-b")
	defer pa.Close()
	defer pb.Close()
	cfg := routerSettings(t, a, pa.URL, pb.URL)
	done := make(chan error, 1)
	go func() {
		_, err := a.prepareParallel(context.Background(), s, task, cfg, []Message{{Role: "user", Content: task.Prompt}}, ProfileParams{MaxTokens: 4096})
		done <- err
	}()
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			close(release)
			t.Fatal("independent endpoints did not overlap")
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if maximum.Load() != 2 || seen["code-model"] != 1 || seen["write-model"] != 1 {
		t.Fatalf("wrong fanout: %v max=%d", seen, maximum.Load())
	}
	if task.Parallel.Parts[0].Model != "code-model" || task.Parallel.Parts[1].Model != "write-model" || task.Usage.Total != 60 {
		t.Fatalf("missing model/usage history: %+v usage=%+v", task.Parallel, task.Usage)
	}
}
func TestAutoModelRouterSequentialAndSelection(t *testing.T) {
	a := testApp(t)
	routerFixture(t, a)
	cfg := routerSettings(t, a, "http://a.invalid", "http://b.invalid")
	s := createSession(t, a)
	choice := SessionModelSelection{Strategy: "auto", Profile: "default", Model: "code-model", ReasoningEffort: "off", ModelRouter: "auto"}
	requireStatus(t, request(a, "PUT", "/api/sessions/"+s.ID+"/model-selection", choice), 200)
	if got := getSelectionSession(t, a, s.ID); got.ModelSelection.ModelRouter != "auto" {
		t.Fatal("auto selection not persisted")
	}
	task := &Task{Prompt: "write document", Parallelism: 1, ModelSelection: &choice}
	for _, item := range []struct{ stage, model string }{{"plan", "code-model"}, {"chat", "write-model"}, {"review", "code-model"}} {
		out := a.routedModel(task, cfg, []Message{{Role: "user", Content: task.Prompt}}, ProfileParams{MaxTokens: 1024}, nil, item.stage)
		if out.Model != item.model {
			t.Fatalf("stage %s selected %s", item.stage, out.Model)
		}
	}
	a.pluginRegistry.Plugins[0].Enabled = false
	choice.ModelRouter = ""
	requireStatus(t, request(a, "PUT", "/api/sessions/"+s.ID+"/model-selection", choice), 200)
	choice.ModelRouter = "auto"
	requireStatus(t, request(a, "PUT", "/api/sessions/"+s.ID+"/model-selection", choice), 400)
}

func TestAutoModelRouterCancellation(t *testing.T) {
	a, s, task := parallelFixture(t, 2)
	routerFixture(t, a)
	task.ModelSelection = &SessionModelSelection{ModelRouter: "auto"}
	task.Prompt = "implement code and write document"
	started := make(chan struct{}, 2)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []Message `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if strings.HasPrefix(body.Messages[len(body.Messages)-1].Content, "[parallel planner]") {
			parallelReply(w, `{"tasks":["implement code","write document"]}`)
			return
		}
		started <- struct{}{}
		<-r.Context().Done()
	}))
	defer provider.Close()
	cfg := routerSettings(t, a, provider.URL, provider.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := a.prepareParallel(ctx, s, task, cfg, []Message{{Role: "user", Content: task.Prompt}}, ProfileParams{MaxTokens: 4096})
		done <- err
	}()
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			cancel()
			t.Fatal("workers did not start")
		}
	}
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("cancellation lost: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("router leaked cancelled workers")
	}
	if task.Parallel.Phase != "interrupted" {
		t.Fatal("cancelled route reported completed")
	}
}
func TestAutoModelRouterInvalidCatalogs(t *testing.T) {
	for _, decl := range []*PluginModelRouter{{2, "capabilities.json"}, {1, "../capabilities.json"}, {1, "/tmp/catalog.json"}, {1, "capabilities.json"}} {
		if err := validatePluginModelRouter(decl, map[string][]byte{"capabilities.json": []byte(`{"protocol":1,"models":[]}`)}); err == nil {
			t.Fatal("invalid extension accepted", decl)
		}
	}
	if _, err := validateRouterCatalog(make([]byte, (128<<10)+1)); err == nil {
		t.Fatal("oversized catalog accepted")
	}
}
func TestAutoModelRouterSequentialHTTP(t *testing.T) {
	a := testApp(t)
	routerFixture(t, a)
	var active, maximum atomic.Int32
	var mu sync.Mutex
	var calls []string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		n := active.Add(1)
		for old := maximum.Load(); n > old; old = maximum.Load() {
			if maximum.CompareAndSwap(old, n) {
				break
			}
		}
		want := "router-key-a"
		if body.Model == "write-model" {
			want = "router-key-b"
		}
		if r.Header.Get("Authorization") != "Bearer "+want {
			t.Error("sequential model credential crossed")
		}
		mu.Lock()
		calls = append(calls, body.Model)
		mu.Unlock()
		active.Add(-1)
		parallelReply(w, "step result")
	}))
	defer provider.Close()
	cfg := routerSettings(t, a, provider.URL, provider.URL)
	task := &Task{Prompt: "write document", Parallelism: 1, ModelSelection: &SessionModelSelection{ModelRouter: "auto"}}
	input := []Message{{Role: "user", Content: task.Prompt}}
	params := ProfileParams{MaxTokens: 1024}
	for _, stage := range []string{"plan", "chat", "review"} {
		next := a.routedModel(task, cfg, input, params, nil, stage)
		if _, err := a.parallelCall(context.Background(), task, next, input, params, stage, nil); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Join(calls, ",") != "code-model,write-model,code-model" || maximum.Load() != 1 || task.Usage.Total != 60 {
		t.Fatalf("sequential dispatch mismatch: %v max=%d", calls, maximum.Load())
	}
}

func TestAutoModelRouterWireParameters(t *testing.T) {
	temp, top := 0.5, 0.9
	params := ProfileParams{MaxTokens: 2048, Temperature: &temp, TopP: &top, Stop: []string{"STOP"}}
	for _, style := range []string{"deepseek", "openai-reasoning", "openai-compatible"} {
		cfg := Settings{Model: "fixture", ReasoningEffort: "off", routedProfile: &routerProfile{WireFormat: style}}
		body := buildChatBody(cfg, []Message{{Role: "user", Content: "request"}}, params, nil, false)
		if style == "deepseek" {
			if body["thinking"] == nil || body["max_tokens"] != 2048 {
				t.Fatal("DeepSeek wire changed")
			}
			continue
		}
		if body["thinking"] != nil {
			t.Fatal("DeepSeek field leaked to another provider")
		}
		if style == "openai-reasoning" {
			if body["max_completion_tokens"] != 2048 || body["max_tokens"] != nil || body["reasoning_effort"] != "none" || body["temperature"] != nil || body["stop"] != nil {
				t.Fatal("GPT wire invalid", body)
			}
		} else if body["reasoning_effort"] != nil {
			t.Fatal("unknown gateway reasoning extension sent")
		}
	}
	legacy := buildChatBody(Settings{Model: "fixture", ReasoningEffort: "off"}, nil, params, nil, false)
	if legacy["thinking"] == nil || legacy["max_tokens"] != 2048 {
		t.Fatal("manual provider behavior changed")
	}
}

func TestAutoModelRouterDisableBetweenRequests(t *testing.T) {
	a := testApp(t)
	routerFixture(t, a)
	cfg := routerSettings(t, a, "http://a.invalid", "http://b.invalid")
	task := &Task{Prompt: "write document", ModelSelection: &SessionModelSelection{ModelRouter: "auto", Model: "code-model"}}
	input := []Message{{Role: "user", Content: task.Prompt}}
	routed := a.routedModel(task, cfg, input, ProfileParams{MaxTokens: 1024}, nil, "chat")
	if routed.Model != "write-model" {
		t.Fatal("writing model not selected")
	}
	a.pluginRegistry.Plugins[0].Enabled = false
	restored := a.routedModel(task, routed, input, ProfileParams{MaxTokens: 1024}, nil, "chat")
	if restored.Model != "code-model" || restored.APIKey != "router-key-a" || restored.routedProfile != nil {
		t.Fatal("previous automatic result survived disable")
	}
}
