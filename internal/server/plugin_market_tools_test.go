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
	"sync/atomic"
	"testing"
	"time"
)

func marketBrowseResult(t *testing.T, raw string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("invalid result %q: %v", raw, err)
	}
	return out
}
func TestMarketBrowseCleanInstallSurfacesAndNoMutation(t *testing.T) {
	a, _, _ := marketFixture(t, "2.0.0")
	if len(a.pluginRegistry.Plugins) != 0 {
		t.Fatal("not a clean plugin registry")
	}
	a.mu.Lock()
	mainTools := a.routedPluginSchemasLocked(routingTask(a), a.contextTools(), "what is available in the market?")
	a.mu.Unlock()
	for _, tools := range [][]any{mainTools, a.assistantAgentTools(false), a.assistantAgentTools(true)} {
		found := false
		for _, schema := range tools {
			found = found || pluginSchemaName(schema) == "browse_plugin_market"
		}
		if !found {
			t.Fatal("missing host discovery with zero installed plugins")
		}
	}
	// Even enabling the independent automatic-install option cannot turn a browse
	// into an installation, and discovery must not activate any task routes.
	routingEnable(t, a)
	task := routingTask(a)
	raw := a.executeToolCall(context.Background(), readCall("browse_plugin_market", `{"action":"list"}`), task, nil)
	out := marketBrowseResult(t, raw)
	if out["error"] != nil || out["readOnly"] != true || out["catalogTotal"] != float64(1) {
		t.Fatal(raw)
	}
	if len(a.pluginRegistry.Plugins) != 0 || len(task.PluginRoutes) != 0 || len(task.PluginPlans) != 0 {
		t.Fatal("discovery mutated plugin/task state")
	}
	rows := out["plugins"].([]any)
	row := rows[0].(map[string]any)
	if row["installed"] != false || row["enabled"] != false || row["version"] != "2.0.0" {
		t.Fatal(row)
	}
	dec := assistantDecision{}
	got := marketBrowseResult(t, a.execAssistantToolWithContext(context.Background(), readCall("browse_plugin_market", `{"action":"detail","id":"market-fixture"}`), &dec))
	if got["error"] != nil || len(a.pluginRegistry.Plugins) != 0 || dec.Action != "" {
		t.Fatal(got, dec)
	}
}
func TestMarketBrowseSearchPagingDetailsAndLocalStatus(t *testing.T) {
	a, s, _ := marketFixture(t, "2.0.0")
	p := s.Catalog.Plugins[0]
	p.Description = "Generate MP3 audio"
	p.Permissions = "network access"
	p.Readme = "Untrusted listing data"
	p.Routing = &pluginRouteMetadata{Keywords: []string{"mp3", "语音"}, Summary: "voice files"}
	s.Catalog.Plugins[0] = p
	second := p
	second.ID = "second"
	second.Package = "packages/second/2.0.0.zip"
	second.Name = "Second"
	second.MinAideVersion = "99.0.0 RC1"
	s.Catalog.Plugins = append(s.Catalog.Plugins, second)
	cacheMarket(t, a, s)
	a.pluginRegistry.Plugins = []PluginManifest{{ID: p.ID, Name: p.Name, Version: "1.0.0", Enabled: false}}
	out := marketBrowseResult(t, a.browsePluginMarket(context.Background(), `{"action":"search","query":"生成语音","limit":1}`))
	if out["total"] != float64(2) || out["hasMore"] != true || out["nextOffset"] != float64(1) {
		t.Fatal(out)
	}
	first := out["plugins"].([]any)[0].(map[string]any)
	if first["installed"] != true || first["enabled"] != false || first["updateAvailable"] != true || first["installedVersion"] != "1.0.0" {
		t.Fatal(first)
	}
	next := marketBrowseResult(t, a.browsePluginMarket(context.Background(), `{"action":"list","offset":1,"limit":1}`))
	row := next["plugins"].([]any)[0].(map[string]any)
	if row["id"] != "second" || row["compatible"] != false || row["incompatibility"] == "" || next["hasMore"] != false {
		t.Fatal(next)
	}
	detail := marketBrowseResult(t, a.browsePluginMarket(context.Background(), `{"action":"detail","id":"market-fixture"}`))
	row = detail["plugins"].([]any)[0].(map[string]any)
	if row["permissions"] != "network access" || row["readme"] != "Untrusted listing data" || detail["untrustedMetadata"] != true {
		t.Fatal(detail)
	}
	for _, key := range []string{"package", "downloadURL", "settings", "licenseKey"} {
		if _, ok := row[key]; ok {
			t.Fatal("unnecessary data exposed", key)
		}
	}
	empty := marketBrowseResult(t, a.browsePluginMarket(context.Background(), `{"action":"detail","id":"unknown"}`))
	if empty["status"] != "not_found" {
		t.Fatal(empty)
	}
	filtered := marketBrowseResult(t, a.browsePluginMarket(context.Background(), `{"action":"list","category":"unmatched"}`))
	if filtered["total"] != float64(0) {
		t.Fatal(filtered)
	}
}
func TestMarketBrowseRejectsMutationAndInvalidArguments(t *testing.T) {
	a, _, _ := marketFixture(t, "2.0.0")
	for _, raw := range []string{`{"action":"install","id":"market-fixture"}`, `{"action":"list","installId":"market-fixture"}`, `{"action":"list","refresh":"yes"}`, `{"action":"list","offset":-1}`, `{"action":"list","offset":201}`, `{"action":"list","limit":41}`, `{"action":"list","limit":1.5}`, `{"action":"search","query":""}`, `{"action":"detail","id":"../escape"}`, `{"action":"list"} {}`, `null`, strings.Repeat(" ", 8193)} {
		if out := marketBrowseResult(t, a.browsePluginMarket(context.Background(), raw)); out["error"] == nil {
			t.Fatal("accepted", raw)
		}
	}
	if len(a.pluginRegistry.Plugins) != 0 {
		t.Fatal("installed through query")
	}
}
func TestMarketBrowseDenyAndRestrictedPhases(t *testing.T) {
	a, _, _ := marketFixture(t, "2.0.0")
	a.settings.DisabledTools = []string{"browse_plugin_market"}
	for _, tools := range [][]any{a.contextTools(), a.assistantAgentTools(false)} {
		for _, schema := range tools {
			if pluginSchemaName(schema) == "browse_plugin_market" {
				t.Fatal("disabled tool advertised")
			}
		}
	}
	for _, raw := range []string{a.executeToolCall(context.Background(), readCall("browse_plugin_market", `{"action":"list"}`), routingTask(a), nil), a.execAssistantTool(readCall("browse_plugin_market", `{"action":"list"}`), &assistantDecision{})} {
		if !strings.Contains(raw, "禁用") {
			t.Fatal(raw)
		}
	}
	if out := marketBrowseResult(t, a.browsePluginMarket(context.Background(), `{"action":"list"}`)); out["error"] == nil {
		t.Fatal("handler bypassed deny")
	}
	a.settings.DisabledTools = nil
	task := routingTask(a)
	task.AgentToolsSet = true
	task.AgentTools = []string{"read_file"}
	if raw := a.executeToolCall(context.Background(), readCall("browse_plugin_market", `{"action":"list"}`), task, nil); !strings.Contains(raw, "禁用") {
		t.Fatal("task restriction bypassed", raw)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if tools := a.routedPluginSchemasLocked(routingTask(a), nil, "market"); len(tools) != 0 {
		t.Fatal("tool-free phase gained tools")
	}
}
func TestMarketBrowseOfflineFallbackAndRefresh(t *testing.T) {
	a, s, _ := marketFixture(t, "2.0.0")
	s.Fetched = 0
	cacheMarket(t, a, s)
	var calls atomic.Int32
	a.pluginUpdateClient = &http.Client{Transport: pluginUpgradeTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "" {
			t.Error("credentials forwarded")
		}
		return nil, errors.New("offline fixture")
	})}
	out := marketBrowseResult(t, a.browsePluginMarket(context.Background(), `{"action":"list"}`))
	if out["stale"] != true || calls.Load() != 0 || out["total"] != float64(1) {
		t.Fatal(out, calls.Load())
	}
	out = marketBrowseResult(t, a.browsePluginMarket(context.Background(), `{"action":"list","refresh":true}`))
	if out["stale"] != true || out["refreshError"] == "" || out["total"] != float64(1) {
		t.Fatal(out)
	}
	if err := os.Remove(filepath.Join(a.dataPath, "cache/plugin-market/catalog.json")); err != nil {
		t.Fatal(err)
	}
	// No cache and no installed plugins: the shipped public index is sufficient.
	out = marketBrowseResult(t, a.browsePluginMarket(context.Background(), `{"action":"list"}`))
	if out["error"] != nil || out["catalogTotal"].(float64) < 1 || out["stale"] != true {
		t.Fatal(out)
	}
}
func TestMarketBrowseRefreshSharesValidatedRoutingIndex(t *testing.T) {
	a, s, _ := marketFixture(t, "2.0.0")
	s.Revision = strings.Repeat("b", 40)
	body, _ := json.Marshal(s.Catalog)
	a.pluginUpdateClient = &http.Client{Transport: pluginUpgradeTransport(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "/git/ref/") {
			return upgradeResponse([]byte(`{"object":{"sha":"` + s.Revision + `"}}`)), nil
		}
		if strings.HasSuffix(r.URL.Path, "/catalog.json") {
			return upgradeResponse(body), nil
		}
		t.Error("query attempted a package download", r.URL)
		return nil, errors.New("unexpected package request")
	})}
	out := marketBrowseResult(t, a.browsePluginMarket(context.Background(), `{"action":"list","refresh":true}`))
	if out["revision"] != s.Revision || out["stale"] != false || a.pluginRoutingCatalog.Revision != s.Revision || len(a.pluginRegistry.Plugins) != 0 {
		t.Fatal(out)
	}
}
func TestMarketBrowseAgentLoopsWithZeroPlugins(t *testing.T) {
	for _, assistant := range []bool{false, true} {
		t.Run(map[bool]string{false: "main", true: "assistant"}[assistant], func(t *testing.T) {
			a, _, _ := marketFixture(t, "2.0.0")
			var rounds atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Messages []Message `json:"messages"`
					Tools    []any     `json:"tools"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				// Main chat also requests a title without tools, concurrently.
				if len(body.Tools) == 0 {
					jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "Market query"}}}})
					return
				}
				found := false
				for _, schema := range body.Tools {
					found = found || pluginSchemaName(schema) == "browse_plugin_market"
				}
				if !found {
					t.Error("tool missing from real model request")
				}
				if rounds.Add(1) == 1 {
					jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "", "tool_calls": []ToolCall{readCall("browse_plugin_market", `{"action":"list"}`)}}}}})
					return
				}
				matched := false
				for _, m := range body.Messages {
					if m.Role == "tool" && strings.Contains(m.Content, `"id":"market-fixture"`) {
						matched = true
					}
				}
				if !matched {
					t.Error("catalog result did not reach next model round")
				}
				jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "The market contains Market fixture 2.0.0; it is not installed."}}}})
			}))
			defer provider.Close()
			cfg := Settings{BaseURL: provider.URL, Model: "test", SandboxMode: "read-only"}
			a.settings = cfg
			if assistant {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				dec, err := a.runAssistantAgenticLoop(ctx, cfg, "市场有哪些插件？", "", "text")
				if err != nil || !strings.Contains(dec.Reply, "Market fixture") {
					t.Fatal(dec, err)
				}
			} else {
				session := createSession(t, a)
				requireStatus(t, request(a, "POST", "/api/sessions/"+session.ID+"/runs", map[string]any{"mode": "chat", "prompt": "市场有哪些插件？"}), 202)
				waitTaskDone(t, a, session.ID)
			}
			if rounds.Load() < 2 || len(a.pluginRegistry.Plugins) != 0 {
				t.Fatal("no tool loop or unexpected installation", rounds.Load())
			}
		})
	}
}

func TestMarketBrowseLiveOfficialCatalog(t *testing.T) {
	if os.Getenv("AIDE_TEST_LIVE_MARKET") != "1" {
		t.Skip("opt-in read-only official metadata acceptance")
	}
	a := testApp(t)
	if len(a.pluginRegistry.Plugins) != 0 {
		t.Fatal("live fixture must start without plugins")
	}
	out := marketBrowseResult(t, a.browsePluginMarket(context.Background(), `{"action":"search","query":"deepseek support","refresh":true}`))
	if out["error"] != nil || out["stale"] != false || out["refreshError"] != "" {
		t.Fatal(out)
	}
	found := false
	for _, value := range out["plugins"].([]any) {
		p := value.(map[string]any)
		found = found || p["id"] == "deepseek-supporter"
		if p["installed"] != false {
			t.Fatal("unexpected installed plugin")
		}
	}
	if !found || len(a.pluginRegistry.Plugins) != 0 {
		t.Fatal("official entry not discovered, or query mutated registry", out)
	}
	t.Logf("official catalog revision %s; %v entries; DeepSeekSupporter discovered with empty registry", out["revision"], out["catalogTotal"])
}
