package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func routingFixture(t *testing.T) (*App, marketSnapshot) {
	t.Helper()
	a, s, _ := marketFixture(t, "2.0.0")
	s.Catalog.Plugins[0].Routing = &pluginRouteMetadata{Keywords: []string{"fixture", "测试路由"}, Extensions: []string{".demo"}, Summary: "只读测试查询"}
	cacheMarket(t, a, s)
	a.pluginRoutingCatalog = s
	return a, s
}
func routingEnable(t *testing.T, a *App) {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	if e := atomicJSON(filepath.Join(ConfigDir(a.dataPath), "plugin-routing.json"), pluginRoutingPolicy{AutoInstall: true, Generation: 1}); e != nil {
		t.Fatal(e)
	}
}
func routingTask(a *App) *Task {
	return &Task{ID: "routing-test", WorkspaceID: a.wsID(), Prompt: "fixture"}
}
func TestPluginRoutingMetadataAndBoundaries(t *testing.T) {
	if routeContains("planet", "plan") || !routeContains("使用SSH连接", "ssh") || !routeContains("look at x.demo", ".demo") {
		t.Fatal("routing boundaries")
	}
	for _, r := range []*pluginRouteMetadata{{Keywords: []string{"x", "X"}}, {Keywords: []string{"../\n"}}, {Keywords: []string{"x"}, Extensions: []string{"../../foo"}}, {Keywords: []string{"x"}, Summary: strings.Repeat("x", 1025)}} {
		if validatePluginRouting(r) == nil {
			t.Fatal("invalid metadata accepted")
		}
	}
	a, s := routingFixture(t)
	s.Catalog.Plugins = append(s.Catalog.Plugins, marketPlugin{ID: "second", Name: "Second", Routing: &pluginRouteMetadata{Keywords: []string{"fixture"}}})
	a.pluginRoutingCatalog = s
	a.mu.Lock()
	matches := a.routeMatchesLocked("fixture x.demo")
	a.mu.Unlock()
	if len(matches) != 2 || matches[0].ID != "market-fixture" || len(matches[0].Reasons) < 2 {
		t.Fatalf("matches %#v", matches)
	}
}
func TestPluginRoutingPolicyAndDefaultOff(t *testing.T) {
	a, _ := routingFixture(t)
	a.pluginRoutingRefresh = time.Now()
	res := request(a, "GET", "/api/plugin-routing", nil)
	requireStatus(t, res, 200)
	if strings.Contains(res.Body.String(), `"autoInstall":true`) {
		t.Fatal("default enabled")
	}
	requireStatus(t, request(a, "PUT", "/api/plugin-routing", map[string]any{"autoInstall": true, "generation": 0}), 200)
	requireStatus(t, request(a, "PUT", "/api/plugin-routing", map[string]any{"autoInstall": false, "generation": 0}), 409)
	requireStatus(t, request(a, "PUT", "/api/plugin-routing", map[string]any{"autoInstall": false, "generation": 1}), 200)
	a.mu.Lock()
	p := a.routingPolicyLocked()
	a.mu.Unlock()
	if p.AutoInstall || p.Generation != 2 {
		t.Fatal(p)
	}
	if e := os.WriteFile(filepath.Join(ConfigDir(a.dataPath), "plugin-routing.json"), []byte(`{"autoInstall":true, BROKEN`), 0600); e != nil {
		t.Fatal(e)
	}
	a.mu.Lock()
	p = a.routingPolicyLocked()
	a.mu.Unlock()
	if p.AutoInstall {
		t.Fatal("corrupt policy authorized")
	}
}
func TestPluginRoutingInstallAuthorizationAndDynamicTools(t *testing.T) {
	a, _ := routingFixture(t)
	task := routingTask(a)
	out := a.executePluginRouting(context.Background(), task, "fixture", "market-fixture")
	if !strings.Contains(out, "未授权") || len(a.pluginRegistry.Plugins) != 0 {
		t.Fatal(out)
	}
	routingEnable(t, a)
	out = a.executePluginRouting(context.Background(), task, "fixture", "market-fixture")
	if !strings.Contains(out, `"status":"installed"`) {
		t.Fatal(out)
	}
	a.mu.Lock()
	tools := a.routedPluginSchemasLocked(task, a.contextTools(), "unrelated")
	a.mu.Unlock()
	has := false
	for _, schema := range tools {
		has = has || pluginSchemaName(schema) == "market_fixture_tool"
	}
	if !has {
		t.Fatal("installed tool not loaded")
	}
	a.mu.Lock()
	a.pluginRegistry.Plugins[0].Enabled = false
	tools = a.routedPluginSchemasLocked(task, tools, "unrelated")
	a.mu.Unlock()
	for _, schema := range tools {
		if pluginSchemaName(schema) == "market_fixture_tool" {
			t.Fatal("disabled tool retained")
		}
	}
	out = a.executePluginRouting(context.Background(), task, "fixture", "market-fixture")
	if !strings.Contains(out, "不自动") && !strings.Contains(out, "只能安装") {
		t.Fatal(out)
	}
	if a.pluginRegistry.Plugins[0].Enabled {
		t.Fatal("re-enabled disabled plugin")
	}
}
func TestPluginRoutingToolReductionAndDeny(t *testing.T) {
	a, _ := routingFixture(t)
	a.pluginRegistry.Plugins = []PluginManifest{{ID: "first", Name: "First", Enabled: true}, {ID: "second", Name: "Second", Enabled: true}}
	a.pluginSurface = []byte(`{"plugins":[{"id":"first","tools":[{"name":"first_tool","executable":true,"description":"long description","parameters":{"type":"object"}}]},{"id":"second","tools":[{"name":"second_tool","executable":true}]}]}`)
	task := routingTask(a)
	a.mu.Lock()
	all := a.contextTools()
	emptyPhase := a.routedPluginSchemasLocked(task, nil, "first")
	limitedPhase := a.routedPluginSchemasLocked(task, []any{map[string]any{"type": "function", "function": map[string]any{"name": "read_file"}}}, "first")

	tools := a.routedPluginSchemasLocked(task, all, "first")
	hint := a.toolListHint()
	a.mu.Unlock()
	if len(emptyPhase) != 0 || len(limitedPhase) != 1 {
		t.Fatal("restricted phase acquired tools", emptyPhase, limitedPhase)
	}
	names := map[string]bool{}
	for _, s := range tools {
		names[pluginSchemaName(s)] = true
	}
	if !names["first_tool"] || names["second_tool"] || !names["route_plugins"] || strings.Contains(hint, "second_tool") {
		t.Fatal(names, hint)
	}
	task.AgentToolsSet = true
	task.AgentTools = []string{"second_tool"}
	a.mu.Lock()
	tools = a.routedPluginSchemasLocked(task, all, "first")
	a.mu.Unlock()
	if len(tools) != 1 || pluginSchemaName(tools[0]) != "second_tool" {
		t.Fatal(tools)
	}
	a.settings.DisabledTools = []string{"second_tool", "route_plugins"}
	a.mu.Lock()
	tools = a.routedPluginSchemasLocked(task, all, "first")
	a.mu.Unlock()
	if len(tools) != 0 {
		t.Fatal("deny bypass", tools)
	}
}
func TestPluginRoutingInstallRevocationAndScopes(t *testing.T) {
	for _, kind := range []string{"revoke", "workspace", "readonly", "cancel", "locked-generation", "bad-package", "restricted-agent"} {
		t.Run(kind, func(t *testing.T) {
			a, s := routingFixture(t)
			routingEnable(t, a)
			task := routingTask(a)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			base := a.pluginUpdateClient.Transport
			if kind == "restricted-agent" {
				task.AgentToolsSet = true
				task.AgentTools = []string{"route_plugins"}
			}
			a.pluginUpdateClient = &http.Client{Transport: pluginUpgradeTransport(func(r *http.Request) (*http.Response, error) {
				a.mu.Lock()
				switch kind {
				case "revoke":
					_ = atomicJSON(filepath.Join(ConfigDir(a.dataPath), "plugin-routing.json"), pluginRoutingPolicy{Generation: 2})
				case "workspace":
					a.wsRevision++
				case "readonly":
					a.settings.SandboxMode = "read-only"
				case "cancel":
					cancel()
				case "locked-generation":
					st := a.loadLockStateLocked()
					st.Gen++
					_ = atomicJSON(lockStatePath(a.dataPath), st)
				}
				a.mu.Unlock()
				if kind == "bad-package" {
					return upgradeResponse([]byte("invalid")), nil
				}
				return base.RoundTrip(r)
			})}
			out := a.executePluginRouting(ctx, task, "fixture", s.Catalog.Plugins[0].ID)
			if strings.Contains(out, `"status":"installed"`) || len(a.pluginRegistry.Plugins) != 0 {
				t.Fatal("unsafe install", out)
			}
			if _, e := os.Stat(filepath.Join(a.pluginsPath, s.Catalog.Plugins[0].ID)); e == nil {
				t.Fatal("package directory remains")
			}
		})
	}
}
func TestPluginRoutingPrivateAndNonmatchNeverInstall(t *testing.T) {
	a, s, _, _ := privateFixture(t)
	routingEnable(t, a)
	a.pluginRoutingCatalog = s
	for _, query := range []string{"market-fixture", "unrelated"} {
		out := a.executePluginRouting(context.Background(), routingTask(a), query, "market-fixture")
		if strings.Contains(out, `"status":"installed"`) || len(a.pluginRegistry.Plugins) != 0 {
			t.Fatal(out)
		}
	}
}

func TestPluginRoutingDefaultIndexDownload(t *testing.T) {
	a, s := routingFixture(t)
	a.pluginRoutingCatalog = marketSnapshot{}
	s.Revision = strings.Repeat("b", 40)
	var calls atomic.Int32
	b, _ := json.Marshal(s.Catalog)
	a.pluginUpdateClient = &http.Client{Transport: pluginUpgradeTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if strings.Contains(r.URL.Path, "/git/ref/") {
			return upgradeResponse([]byte(`{"object":{"sha":"` + s.Revision + `"}}`)), nil
		}
		if strings.HasSuffix(r.URL.Path, "/catalog.json") {
			return upgradeResponse(b), nil
		}
		return nil, errors.New("unexpected request")
	})}
	requireStatus(t, request(a, "GET", "/api/plugin-routing", nil), 200)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		rev := a.pluginRoutingCatalog.Revision
		a.mu.Unlock()
		if rev == s.Revision {
			if calls.Load() != 2 {
				t.Fatal(calls.Load())
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("default background index download missing")
}
func TestPluginRoutingModelRoundLoadsDiscoveredTools(t *testing.T) {
	a, _ := routingFixture(t)
	routingEnable(t, a)
	provider := newToolProvider(t, []func() (string, []ToolCall){func() (string, []ToolCall) {
		return "", []ToolCall{readCall("route_plugins", `{"query":"fixture","installId":"market-fixture"}`)}
	}, func() (string, []ToolCall) { return "", []ToolCall{readCall("market_fixture_tool", `{}`)} }, func() (string, []ToolCall) { return "安装并查询完成", nil }})
	defer provider.Close()
	a.settings = Settings{BaseURL: provider.URL, Model: "test", SandboxMode: "workspace-write"}
	s := createSession(t, a)
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{"mode": "chat", "prompt": "fixture"}), 202)
	waitTaskDone(t, a, s.ID)
	a.mu.Lock()
	defer a.mu.Unlock()
	live := a.sessions[s.ID]
	task := live.Runs[len(live.Runs)-1]
	if len(task.ToolUses) < 2 {
		t.Fatalf("no tool continuation: %#v", task.ToolUses)
	}
	found := false
	for _, snap := range task.RequestSnapshots {
		for _, schema := range snap.Tools {
			if pluginSchemaName(schema) == "market_fixture_tool" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("provider never received newly installed schema")
	}
	if !strings.Contains(task.ToolUses[1].Result, "ok") {
		t.Fatal(task.ToolUses[1])
	}
}
func TestPluginRoutingCachedSearchDuringNetworkRefresh(t *testing.T) {
	a, _ := routingFixture(t)
	a.marketMu.Lock()
	done := make(chan int, 1)
	go func() { done <- request(a, "GET", "/api/plugin-routing/search?query=fixture", nil).Code }()
	select {
	case code := <-done:
		a.marketMu.Unlock()
		if code != 200 {
			t.Fatal(code)
		}
	case <-time.After(time.Second):
		a.marketMu.Unlock()
		<-done
		t.Fatal("local matching waited for network refresh mutex")
	}
	task := routingTask(a)
	for _, id := range []string{"one", "two", "three", "four", "five", "six", "seven"} {
		activatePluginRoute(task, id)
	}
	if len(task.PluginRoutes) != 6 || task.PluginRoutes[0] != "two" || task.PluginRoutes[5] != "seven" {
		t.Fatal("route window", task.PluginRoutes)
	}
}
func TestPluginRoutingBrowser(t *testing.T) {
	if os.Getenv("AIDE_PLUGIN_ROUTING_BROWSER") != "1" {
		t.Skip("opt-in actual browser")
	}
	a, _ := routingFixture(t)
	a.pluginRoutingRefresh = time.Now()
	if e := os.MkdirAll(filepath.Join(a.dataPath, "auth"), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(a.dataPath, "auth/access-token"), []byte(a.token), 0600); e != nil {
		t.Fatal(e)
	}
	mux := http.NewServeMux()
	mux.Handle("/", a.Handler())
	mux.Handle("/store/", http.StripPrefix("/store/", http.FileServer(http.Dir("/workspace/aide-pkg-shop/public"))))
	server := httptest.NewServer(mux)
	defer server.Close()
	script, _ := filepath.Abs("../../scripts/browser-plugin-routing.cjs")
	cmd := exec.Command("node", script)
	cmd.Env = append(os.Environ(), "AIDE_TEST_URL="+server.URL, "AIDE_TEST_DATA="+a.dataPath)
	out, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("%v\n%s", e, out)
	}
	t.Log(string(out))
}
