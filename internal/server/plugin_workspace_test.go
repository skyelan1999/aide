package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPluginCurrentWorkspaceFilesPythonAndCWD(t *testing.T) {
	a := testApp(t)
	code := `module.exports={apply(ctx){ctx.tool({name:'workspace_probe',scheduling:{resources:[{root:'workspace',path:'probe.txt',access:'read'}]},handler:async(args,api)=>({text:api.readFile('probe.txt'),cwd:process.cwd(),root:api.workspaceRoot,python:await api.runPython('probe.py')})});}};`
	requireStatus(t, request(a, "POST", "/api/plugins", map[string]any{"id": "workspace-probe", "name": "probe", "code": code}), 201)
	script := filepath.Join(a.pluginsPath, "workspace-probe", "probe.py")
	if err := os.WriteFile(script, []byte("import json,os\nprint(json.dumps({'cwd':os.getcwd(),'text':open('probe.txt').read()}))\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.workPath, "probe.txt"), []byte("WRONG INSTALL ROOT"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"project-a", "project-b"} {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "probe.txt"), []byte(want), 0600)
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()
		a.mu.Lock()
		a.workspace = root
		a.wsRevision++
		a.mu.Unlock()
		raw, err := a.callPluginToolContext(context.Background(), "workspace-probe", "workspace_probe", map[string]any{})
		if err != nil {
			t.Fatal(err)
		}
		r := raw.(map[string]any)
		if r["text"] != want || r["cwd"] != dir || r["root"] != dir {
			t.Fatalf("wrong workspace %#v", r)
		}
		if r["python"] != `{"cwd": "`+dir+`", "text": "`+want+`"}`+"\n" {
			t.Fatalf("wrong Python root %#v", r)
		}
	}
}

func TestPluginWorkspaceRootNeverFallsBackForSSH(t *testing.T) {
	a := testApp(t)
	a.mu.Lock()
	defer a.mu.Unlock()
	a.wsConfig.Workspace.Mode = "ssh"
	if a.pluginWorkspaceLocked() != "" {
		t.Fatal("remote workspace fell back to installation")
	}
}

func TestPluginCurrentWorkspaceRejectsLateResult(t *testing.T) {
	a := testApp(t)
	code := `module.exports={apply(ctx){ctx.tool({name:'workspace_late',scheduling:{resources:[]},handler:async(args,api)=>{require('fs').writeFileSync(args.marker,'started');await new Promise(r=>setTimeout(r,400));return api.readFile('probe.txt');}});}};`
	requireStatus(t, request(a, "POST", "/api/plugins", map[string]any{"id": "workspace-late", "name": "late", "code": code}), 201)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "probe.txt"), []byte("old project"), 0600)
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	a.mu.Lock()
	a.workspace = root
	a.wsRevision++
	a.mu.Unlock()
	marker := filepath.Join(t.TempDir(), "started")
	done := make(chan error, 1)
	go func() {
		_, e := a.callPluginToolContext(context.Background(), "workspace-late", "workspace_late", map[string]any{"marker": marker})
		done <- e
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, e := os.Stat(marker); e == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("plugin did not start")
		}
		time.Sleep(time.Millisecond)
	}
	a.mu.Lock()
	a.wsRevision++
	a.mu.Unlock()
	if err := <-done; err == nil {
		t.Fatal("accepted result after workspace revision changed")
	}
}
