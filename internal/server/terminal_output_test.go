package server

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTerminalOutputStreamScopeAndCancellation(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node daemon required")
	}
	a := testApp(t)
	dir := filepath.Join(a.pluginsPath, "terminal")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	const worker = `module.exports={apply(ctx){ ctx.tool({name:'terminal_session_poll',description:'read output fixture',parameters:{type:'object',additionalProperties:true},handler:async(args,api)=>{
      if(!args.outputWatch)return {ordinary:true};
      if(api.terminalContext?.workbench!==true||api.terminalContext?.outputWatch!==true) throw Error('invalid private context');
      if(args.offset===0)return {output:{id:args.id,start:0,data:'YWJj',offset:3,reset:false,more:false},credential:'do-not-forward'};
      await new Promise(resolve=>setTimeout(resolve,180));
      return {output:{id:args.id,start:args.offset,data:'',offset:args.offset,reset:false,more:false}};
    }}); }};`
	for filename, contents := range map[string]string{"index.js": worker, "workbench.html": "<!doctype html><title>Terminal test</title>"} {
		if err := os.WriteFile(filepath.Join(dir, filename), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	a.mu.Lock()
	a.pluginRegistry.Plugins = append(a.pluginRegistry.Plugins, PluginManifest{ID: "terminal", Name: "Terminal", Main: "index.js", Enabled: true, Daemon: true, PackageSHA256: strings.Repeat("a", 64), Workbench: &PluginWorkbench{Protocol: 1, Title: "Terminal", Entry: "workbench.html", Tools: []string{terminalOutputTool}, TerminalOutput: 1}})
	a.mu.Unlock()
	if err := a.daemons.StartWithPath("terminal", filepath.Join(dir, "index.js")); err != nil {
		t.Fatal(err)
	}
	waitUntil := time.Now().Add(5 * time.Second)
	for {
		_, err := a.daemons.Call("terminal", terminalOutputTool, nil)
		if err == nil {
			break
		}
		if time.Now().After(waitUntil) {
			t.Fatal("daemon did not become ready", err)
		}
		time.Sleep(15 * time.Millisecond)
	}
	opened := workbenchRequest(a, "POST", "/api/plugins/terminal/workbench/open", true)
	requireStatus(t, opened, 200)
	var ticket struct {
		URL    string
		Bridge struct {
			InstanceID     string
			TerminalOutput bool
		}
	}
	if json.Unmarshal(opened.Body.Bytes(), &ticket) != nil || !ticket.Bridge.TerminalOutput {
		t.Fatal("missing stream capability")
	}
	key := ticket.Bridge.InstanceID
	requireStatus(t, workbenchRequest(a, "GET", ticket.URL, false), 200)
	bridgeURL := "/api/plugins/terminal/workbench/" + key
	requireStatus(t, request(a, "POST", bridgeURL+"/call", map[string]any{"protocol": 1, "method": "tool.call", "requestId": "private_watch", "tool": terminalOutputTool, "arguments": map[string]any{"outputWatch": true}}), 403)
	endpoint := bridgeURL + "/terminal-output?session=" + strings.Repeat("b", 24) + "&offset=0"
	requireStatus(t, workbenchRequest(a, "GET", endpoint, false), 401)
	requireStatus(t, workbenchRequest(a, "GET", endpoint+"&offset=1", true), 400)

	a.mu.Lock()
	b := a.pluginWorkbenchBridges[key]
	b.Busy = true
	a.mu.Unlock()
	server := httptest.NewServer(a.Handler())
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", server.URL+endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/x-ndjson") {
		t.Fatalf("stream unavailable: %d", resp.StatusCode)
	}
	scanner := bufio.NewScanner(resp.Body)
	if !scanner.Scan() || scanner.Text() != `{"heartbeat":true}` || !scanner.Scan() || !strings.Contains(scanner.Text(), `"data":"YWJj"`) || strings.Contains(scanner.Text(), "do-not-forward") {
		t.Fatalf("invalid output stream %q (%v)", scanner.Text(), scanner.Err())
	}
	requireStatus(t, workbenchRequest(a, "GET", endpoint, true), 409)
	a.mu.Lock()
	b.Busy = false
	a.mu.Unlock()
	renewed := request(a, "POST", bridgeURL+"/renew", nil)
	requireStatus(t, renewed, 200)
	if scanner.Scan() || scanner.Err() != nil {
		t.Fatalf("old stream did not stop on rotation: %v", scanner.Err())
	}
	requireStatus(t, workbenchRequest(a, "GET", endpoint, true), 403)
	if _, err := a.daemons.Call("terminal", terminalOutputTool, nil); err != nil {
		t.Fatal("cancelled output reader stopped daemon", err)
	}
}

func TestTerminalOutputRejectsUnrelatedOrInvalidFrames(t *testing.T) {
	id := strings.Repeat("a", 24)
	good := `{"output":{"id":"` + id + `","start":7,"data":"YQ==","offset":8}}`
	if _, err := decodeTerminalOutput([]byte(good), id, 7); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{strings.Replace(good, id, strings.Repeat("b", 24), 1), strings.Replace(good, `"start":7`, `"start":6`, 1), strings.Replace(good, "YQ==", "%%==", 1), strings.Replace(good, `"offset":8`, `"offset":9`, 1)} {
		if _, err := decodeTerminalOutput([]byte(raw), id, 7); err == nil {
			t.Fatalf("accepted invalid output: %s", raw)
		}
	}
}
