package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func bridgeFixture(t *testing.T) (*App, string) {
	a := workbenchFixture(t)
	dir := filepath.Join(a.pluginsPath, "workbench-fixture")
	code := `module.exports={apply(ctx){const add=(name,handler,resources=[])=>ctx.tool({name,description:name,parameters:{type:'object',properties:{},additionalProperties:true},scheduling:{resources},handler});add('bridge_echo',args=>({value:args.value}));add('bridge_write',(args,api)=>api.proposeWrite(args.path,args.content),[{root:'workspace',argument:'path',access:'write'}]);add('bridge_badwrite',(args,api)=>api.proposeWrite(args.path,args.content));add('bridge_wait',async args=>{require('fs').writeFileSync(args.marker,'started');await new Promise(resolve=>setTimeout(resolve,10000));return {finished:true};});}};`
	if e := os.WriteFile(filepath.Join(dir, "index.js"), []byte(code), 0644); e != nil {
		t.Fatal(e)
	}
	for i := range a.pluginRegistry.Plugins {
		p := &a.pluginRegistry.Plugins[i]
		if p.ID == "workbench-fixture" {
			p.Workbench.Tools = []string{"bridge_echo", "bridge_write", "bridge_wait", "bridge_badwrite"}
		}
	}
	a.runPluginHost(context.Background())
	if a.pluginOwnerOf("bridge_echo") != "workbench-fixture" {
		t.Fatal("fixture tools not registered")
	}
	w := workbenchRequest(a, "POST", "/api/plugins/workbench-fixture/workbench/open", true)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var out struct {
		URL    string `json:"url"`
		Bridge struct {
			Instance string `json:"instanceId"`
		} `json:"bridge"`
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	if out.Bridge.Instance == "" {
		t.Fatal("missing bridge capability")
	}
	if w = workbenchRequest(a, "GET", out.URL, false); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	return a, out.Bridge.Instance
}
func bridgeCall(a *App, key, id, tool string, args map[string]any) *httptest.ResponseRecorder {
	return request(a, "POST", "/api/plugins/workbench-fixture/workbench/"+key+"/call", map[string]any{"protocol": 1, "method": "tool.call", "requestId": id, "tool": tool, "arguments": args})
}
func TestPluginWorkbenchBridgeOwnToolsReplayAndPolicy(t *testing.T) {
	a, key := bridgeFixture(t)
	w := bridgeCall(a, key, "r1", "bridge_echo", map[string]any{"value": "ok"})
	requireStatus(t, w, 200)
	var out struct {
		Status string         `json:"status"`
		Result map[string]any `json:"result"`
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	if out.Status != "result" || out.Result["value"] != "ok" {
		t.Fatal(w.Body.String())
	}
	requireStatus(t, bridgeCall(a, key, "r1", "bridge_echo", nil), 409)
	for _, tool := range []string{"read_file", "other_plugin_tool", "bridge_unlisted"} {
		requireStatus(t, bridgeCall(a, key, "bad_"+tool, tool, nil), 403)
	}
	requireStatus(t, bridgeCall(a, key, "source", "bridge_echo", map[string]any{"source": "hidden"}), 403)
	a.mu.Lock()
	a.settings.DisabledTools = append(a.settings.DisabledTools, "bridge_echo")
	a.mu.Unlock()
	requireStatus(t, bridgeCall(a, key, "disabled", "bridge_echo", nil), 403)
}
func TestPluginWorkbenchBridgeProposalStaysPendingUntilHostApproval(t *testing.T) {
	a, key := bridgeFixture(t)
	w := bridgeCall(a, key, "write1", "bridge_write", map[string]any{"path": "bridge-test.txt", "content": "candidate"})
	requireStatus(t, w, 200)
	var out struct {
		Status   string `json:"status"`
		Proposal string `json:"proposalId"`
		Session  string `json:"sessionId"`
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	if out.Status != "proposal_pending" || out.Proposal == "" || out.Session == "" {
		t.Fatal(w.Body.String())
	}
	path := filepath.Join(a.workPath, "bridge-test.txt")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("bridge wrote before approval")
	}
	query := func(proposal string) *httptest.ResponseRecorder {
		return request(a, "POST", "/api/plugins/workbench-fixture/workbench/"+key+"/call", map[string]any{"protocol": 1, "method": "proposal.status", "requestId": "query", "proposalId": proposal})
	}
	requireStatus(t, query("not-owned"), 404)
	requireStatus(t, query(out.Proposal), 200)
	requireStatus(t, request(a, "POST", "/api/sessions/"+out.Session+"/runs/"+out.Proposal+"/apply", nil), 200)
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "candidate" {
		t.Fatal("host approval failed", err)
	}
	w = query(out.Proposal)
	requireStatus(t, w, 200)
	var status struct {
		Result struct {
			Applied bool `json:"applied"`
		} `json:"result"`
	}
	json.Unmarshal(w.Body.Bytes(), &status)
	if !status.Result.Applied {
		t.Fatal(w.Body.String())
	}
	requireStatus(t, bridgeCall(a, key, "write-existing", "bridge_write", map[string]any{"path": "bridge-test.txt", "content": "unattached"}), 400)
}
func TestPluginWorkbenchBridgeScopeCloseAndPackageRevocation(t *testing.T) {
	for _, change := range []string{"workspace", "disabled", "page", "closed", "package"} {
		t.Run(change, func(t *testing.T) {
			a, key := bridgeFixture(t)
			a.mu.Lock()
			switch change {
			case "workspace":
				a.wsRevision++
			case "disabled":
				for i := range a.pluginRegistry.Plugins {
					if a.pluginRegistry.Plugins[i].ID == "workbench-fixture" {
						a.pluginRegistry.Plugins[i].Enabled = false
					}
				}
			case "page":
				os.WriteFile(filepath.Join(a.pluginsPath, "workbench-fixture/ui.html"), []byte("changed"), 0644)
			case "package":
				for i := range a.pluginRegistry.Plugins {
					if a.pluginRegistry.Plugins[i].ID == "workbench-fixture" {
						a.pluginRegistry.Plugins[i].PackageSHA256 = "changed"
					}
				}
			}
			a.mu.Unlock()
			if change == "closed" {
				requireStatus(t, request(a, "DELETE", "/api/plugins/workbench-fixture/workbench/"+key, nil), 200)
			}
			requireStatus(t, bridgeCall(a, key, "after", "bridge_echo", nil), 403)
		})
	}
}
func TestPluginWorkbenchBridgeManifestTools(t *testing.T) {
	for _, tools := range [][]string{{"same", "same"}, {"../bad"}, {"with-dash"}} {
		if validatePluginWorkbench(&PluginWorkbench{Protocol: 1, Entry: "ui.html", Title: "x", Tools: tools}) == nil {
			t.Fatal("invalid tools accepted", tools)
		}
	}
}

func TestPluginWorkbenchBridgeCancelsDispatchedCallOnScopeChange(t *testing.T) {
	a, key := bridgeFixture(t)
	marker := filepath.Join(a.workPath, "bridge-started")
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- bridgeCall(a, key, "waiting", "bridge_wait", map[string]any{"marker": marker}) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fixture did not dispatch")
		}
		time.Sleep(20 * time.Millisecond)
	}
	a.mu.Lock()
	a.wsRevision++
	a.mu.Unlock()
	select {
	case w := <-done:
		if w.Code == 200 {
			t.Fatal("stale result accepted", w.Body.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("scope change failed to cancel handler")
	}
}
func TestPluginWorkbenchBridgeRejectsReadOnlySandbox(t *testing.T) {
	a, key := bridgeFixture(t)
	a.mu.Lock()
	a.settings.SandboxMode = "read-only"
	a.mu.Unlock()
	requireStatus(t, bridgeCall(a, key, "readonly", "bridge_write", map[string]any{"path": "no.txt", "content": "no"}), 403)
}

func TestPluginWorkbenchBridgeRejectsProposalFromReadOnlyContract(t *testing.T) {
	a, key := bridgeFixture(t)
	w := bridgeCall(a, key, "badcontract", "bridge_badwrite", map[string]any{"path": "must-not-write.txt", "content": "bad"})
	requireStatus(t, w, 400)
	if _, err := os.Stat(filepath.Join(a.workPath, "must-not-write.txt")); !os.IsNotExist(err) {
		t.Fatal("write escaped scheduling contract")
	}
}

func TestPluginWorkbenchBridgeRenewRotatesAndPreservesScope(t *testing.T) {
	a, key := bridgeFixture(t)
	requireStatus(t, bridgeCall(a, key, "initial", "bridge_echo", nil), 200)
	old := a.pluginWorkbenchBridges[key]
	old.Proposals["saved_proposal"] = "saved_session"
	old.Seen = map[string]bool{}
	for i := 0; i < 128; i++ {
		old.Seen[fmt.Sprintf("request_%d", i)] = true
	}
	requireStatus(t, bridgeCall(a, key, "exhausted", "bridge_echo", nil), 409)
	route := "/api/plugins/workbench-fixture/workbench/" + key + "/renew"
	requireStatus(t, workbenchRequest(a, "POST", route, false), 401)
	w := workbenchRequest(a, "POST", route, true)
	requireStatus(t, w, 200)
	var renewed struct {
		Protocol int    `json:"protocol"`
		Key      string `json:"instanceId"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &renewed); err != nil {
		t.Fatal(err)
	}
	if renewed.Protocol != 1 || len(renewed.Key) != 64 || renewed.Key == key {
		t.Fatal(renewed)
	}
	requireStatus(t, bridgeCall(a, key, "stale", "bridge_echo", nil), 403)
	requireStatus(t, bridgeCall(a, renewed.Key, "initial", "bridge_echo", map[string]any{"value": "after renewal"}), 200)
	requireStatus(t, bridgeCall(a, renewed.Key, "initial", "bridge_echo", nil), 409)
	if a.pluginWorkbenchBridges[renewed.Key].Proposals["saved_proposal"] != "saved_session" {
		t.Fatal("lost proposal ownership")
	}
	// Busy capabilities and invalid scopes cannot be renewed, nor can expired ones revive.
	for _, kind := range []string{"busy", "expiry", "workspace", "lock", "package", "disable"} {
		t.Run(kind, func(t *testing.T) {
			a, key := bridgeFixture(t)
			b := a.pluginWorkbenchBridges[key]
			switch kind {
			case "busy":
				b.Busy = true
			case "expiry":
				b.Expires = time.Now().Add(-time.Second)
			case "workspace":
				b.Revision++
			case "lock":
				os.MkdirAll(filepath.Dir(lockStatePath(a.dataPath)), 0700)
				os.WriteFile(lockStatePath(a.dataPath), []byte(`{"locked":true,"gen":4}`), 0600)
			case "package":
				b.Package = "changed"
			case "disable":
				a.pluginRegistry.Plugins[len(a.pluginRegistry.Plugins)-1].Enabled = false
			}
			w := workbenchRequest(a, "POST", "/api/plugins/workbench-fixture/workbench/"+key+"/renew", true)
			expected := 403
			if kind == "busy" {
				expected = 409
			}
			requireStatus(t, w, expected)
			if a.pluginWorkbenchBridges[key] != b {
				t.Fatal("invalid renewal changed capability")
			}
		})
	}
}

func TestPluginWorkbenchDetachFinishesOneCallButCannotEscapeRevocation(t *testing.T) {
	for _, change := range []string{"disconnect", "scope", "close"} {
		t.Run(change, func(t *testing.T) {
			a, key := bridgeFixture(t)
			file := filepath.Join(a.pluginsPath, "workbench-fixture/index.js")
			code, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			code = []byte(strings.ReplaceAll(string(code), "setTimeout(resolve,10000)", "setTimeout(resolve,700)"))
			if err = os.WriteFile(file, code, 0644); err != nil {
				t.Fatal(err)
			}
			a.mu.Lock()
			for i := range a.pluginRegistry.Plugins {
				p := &a.pluginRegistry.Plugins[i]
				if p.ID == "workbench-fixture" {
					p.Daemon = true
					p.Workbench.CloseBehavior = "detach"
				}
			}
			a.mu.Unlock()
			if err = a.daemons.Start("workbench-fixture"); err != nil {
				t.Fatal(err)
			}
			requireStatus(t, request(a, "DELETE", "/api/plugins/workbench-fixture/workbench/"+key, nil), 200)
			opened := workbenchRequest(a, "POST", "/api/plugins/workbench-fixture/workbench/open", true)
			requireStatus(t, opened, 200)
			var instance struct {
				URL    string `json:"url"`
				Bridge struct {
					Key string `json:"instanceId"`
				} `json:"bridge"`
			}
			if err = json.Unmarshal(opened.Body.Bytes(), &instance); err != nil {
				t.Fatal(err)
			}
			key = instance.Bridge.Key
			requireStatus(t, workbenchRequest(a, "GET", instance.URL, false), 200)
			marker := filepath.Join(a.workPath, "detached-started")
			body, _ := json.Marshal(map[string]any{"protocol": 1, "method": "tool.call", "requestId": "detached", "tool": "bridge_wait", "arguments": map[string]any{"marker": marker}})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			r := httptest.NewRequest("POST", "/api/plugins/workbench-fixture/workbench/"+key+"/call", bytes.NewReader(body)).WithContext(ctx)
			r.Header.Set("Authorization", "Bearer "+a.token)
			w := httptest.NewRecorder()
			done := make(chan struct{})
			go func() { a.Handler().ServeHTTP(w, r); close(done) }()
			deadline := time.Now().Add(5 * time.Second)
			for {
				if _, err = os.Stat(marker); err == nil {
					break
				}
				select {
				case <-done:
					t.Fatalf("not dispatched: %d %s", w.Code, w.Body.String())
				default:
				}
				if time.Now().After(deadline) {
					t.Fatal("not dispatched before deadline")
				}
				time.Sleep(10 * time.Millisecond)
			}
			cancel() // closing/reloading the browser must not destroy a resident daemon
			switch change {
			case "scope":
				a.mu.Lock()
				a.wsRevision++
				a.mu.Unlock()
			case "close":
				requireStatus(t, request(a, "DELETE", "/api/plugins/workbench-fixture/workbench/"+key, nil), 200)
			}
			select {
			case <-done:
			case <-time.After(4 * time.Second):
				t.Fatal("call did not finish")
			}
			if change == "disconnect" {
				requireStatus(t, w, 200)
				if _, err = a.callPluginTool("workbench-fixture", "bridge_echo", map[string]any{"value": "still running"}); err != nil {
					t.Fatal("detached call stopped daemon", err)
				}
			} else if w.Code == 200 {
				t.Fatal("revoked call accepted", w.Body.String())
			}
		})
	}
}
