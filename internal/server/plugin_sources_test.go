package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func pluginNativeSourceFixture(t *testing.T, a *App) bool {
	t.Helper()
	bundle := os.Getenv("AIDE_TEST_COMPARE_BUNDLE")
	if bundle == "" {
		return false
	}
	raw, e := os.ReadFile(bundle)
	if e != nil {
		t.Fatal(e)
	}
	manifest, files, e := parsePluginBundle(raw)
	if e != nil {
		t.Fatal(e)
	}
	for name, b := range files {
		p := filepath.Join(a.pluginsPath, manifest.ID, name)
		os.MkdirAll(filepath.Dir(p), 0700)
		if e := os.WriteFile(p, b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	a.mu.Lock()
	manifest.Enabled = true
	a.pluginRegistry.Plugins = append(a.pluginRegistry.Plugins, manifest)
	a.mu.Unlock()
	a.runPluginHost(context.Background())
	return true
}

func TestPluginSourceNativeComparison(t *testing.T) {
	a := testApp(t)
	if !pluginNativeSourceFixture(t, a) {
		t.Skip("set AIDE_TEST_COMPARE_BUNDLE to a local private package")
	}
	os.WriteFile(filepath.Join(a.workspace.Name(), "a.txt"), []byte("port=8080\n"), 0600)
	os.WriteFile(filepath.Join(a.reference.Name(), "b.txt"), []byte("port=8090\n"), 0600)
	args := map[string]any{"left": "a.txt", "right": "b.txt", "rightSource": "context"}
	call := func(op string, args map[string]any) map[string]any {
		t.Helper()
		raw, e := a.callPluginToolContext(context.Background(), "compare", "compare_"+op, args)
		if e != nil {
			t.Fatal(op, e)
		}
		return raw.(map[string]any)
	}
	first := call("files", args)
	if first["byteEqual"] != false || first["sources"] == nil {
		t.Fatal(first)
	}
	second := call("files", args)
	if first["comparisonId"] != second["comparisonId"] {
		t.Fatal("unstable snapshot identity")
	}
	report := call("report", map[string]any{"request": map[string]any{"operation": "files", "arguments": args}, "format": "review", "expectedComparisonId": first["comparisonId"]})
	if !strings.Contains(report["content"].(string), "context") {
		t.Fatal("report source missing")
	}
	evidence := call("report", map[string]any{"request": map[string]any{"operation": "files", "arguments": args}, "format": "evidence", "expectedComparisonId": first["comparisonId"]})
	if evidence["encoding"] != "base64" {
		t.Fatal(evidence)
	}
	encoded, e := base64.StdEncoding.DecodeString(evidence["content"].(string))
	if e != nil || len(encoded) == 0 {
		t.Fatal("empty evidence")
	}
	bundle := filepath.Join(t.TempDir(), "evidence.zip")
	os.WriteFile(bundle, encoded, 0600)
	engine := filepath.Join(a.pluginsPath, "compare", "native", "compare-"+runtime.GOOS+"-"+runtime.GOARCH)
	os.Chmod(engine, 0700)
	verify := filepath.Join(a.pluginsPath, "compare", "scripts", "verify_evidence.py")
	if out, e := exec.Command("python3", verify, bundle, "--sha256", evidence["sha256"].(string), "--engine", engine).CombinedOutput(); e != nil {
		t.Fatal("independent evidence replay", e, string(out))
	}
	call("read", map[string]any{"path": "b.txt", "fileSource": "context"})
	os.Mkdir(filepath.Join(a.workspace.Name(), "pair"), 0700)
	os.Mkdir(filepath.Join(a.reference.Name(), "pair"), 0700)
	os.WriteFile(filepath.Join(a.workspace.Name(), "pair", "a.txt"), []byte("left"), 0600)
	os.WriteFile(filepath.Join(a.reference.Name(), "pair", "a.txt"), []byte("right"), 0600)
	call("directories", map[string]any{"left": "pair", "right": "pair", "rightSource": "context", "maxFiles": 20})
	os.WriteFile(filepath.Join(a.reference.Name(), "b.txt"), []byte("changed"), 0600)
	if _, e := a.callPluginToolContext(context.Background(), "compare", "compare_report", map[string]any{"request": map[string]any{"operation": "files", "arguments": args}, "format": "evidence", "expectedComparisonId": first["comparisonId"]}); e == nil {
		t.Fatal("export accepted modified source bytes")
	}
}

func TestPluginSourceSnapshotsAndRejection(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	os.WriteFile(filepath.Join(a.workspace.Name(), "a.txt"), []byte("old\n"), 0600)
	os.WriteFile(filepath.Join(a.reference.Name(), "b.txt"), []byte("new\n"), 0600)
	os.Mkdir(filepath.Join(a.reference.Name(), "folder"), 0700)
	os.WriteFile(filepath.Join(a.reference.Name(), "folder", "b.txt"), []byte("new\n"), 0600)
	os.Symlink("b.txt", filepath.Join(a.reference.Name(), "alias.txt"))
	os.Symlink("folder", filepath.Join(a.reference.Name(), "aliasdir"))
	a.mu.Lock()
	areas := a.pluginSourceAreasLocked()
	a.mu.Unlock()
	stage := func(in pluginSourceRequest) (any, error) {
		var n int64
		return a.pluginSourceRequest(ctx, areas, t.TempDir(), &n, map[string]bool{}, in)
	}
	r, e := stage(pluginSourceRequest{Operation: "stage", Source: "context", Path: "b.txt", Slot: "left"})
	if e != nil {
		t.Fatal(e)
	}
	value := r.(map[string]any)
	b, e := os.ReadFile(filepath.Join(value["workspace"].(string), value["path"].(string)))
	if e != nil || string(b) != "new\n" {
		t.Fatal(string(b), e)
	}
	for _, in := range []pluginSourceRequest{
		{Operation: "stage", Source: "context", Path: "../a.txt", Slot: "left"},
		{Operation: "stage", Source: "context", Path: "alias.txt", Slot: "left"},
		{Operation: "stage", Source: "context", Path: "aliasdir", Slot: "left", Recursive: true},
		{Operation: "stage", Source: "context", Path: "b.txt", Slot: "left", MaxBytes: 1},
		{Operation: "stage", Source: "context", Path: ".", Slot: "left", Recursive: true},
		{Operation: "stage", Source: "source:missing", Path: "b.txt", Slot: "left"},
	} {
		if _, e := stage(in); e == nil {
			t.Fatalf("accepted unsafe/incomplete input: %#v", in)
		}
	}
	os.Mkdir(filepath.Join(a.reference.Name(), "folder", "nested"), 0700)
	os.WriteFile(filepath.Join(a.reference.Name(), "folder", "nested", "c.txt"), []byte("nested"), 0600)
	if _, e := stage(pluginSourceRequest{Operation: "stage", Source: "context", Path: "folder", Slot: "left", Recursive: true, MaxFiles: 1}); e == nil {
		t.Fatal("file budget ignored")
	}
	if _, e := stage(pluginSourceRequest{Operation: "stage", Source: "context", Path: ".", Slot: "left", Recursive: true, Exclude: []string{"alias.txt", "aliasdir"}}); e != nil {
		t.Fatal("bounded directory snapshot", e)
	}
	a.mu.Lock()
	a.wsRevision++
	a.mu.Unlock()
	if _, e := stage(pluginSourceRequest{Operation: "browse", Path: "."}); e == nil {
		t.Fatal("stale origin accepted")
	}
}

func TestPluginSourceSocketCleanupAndVisibility(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	a.mu.Lock()
	a.sourceRegistry.Sources = append(a.sourceRegistry.Sources, Source{ID: "hidden", Name: "Hidden", Type: "local", Enabled: true, AIHiddenWorkspaces: []string{a.wsID()}})
	a.mu.Unlock()
	sock, closeSources, current, e := a.startPluginSources(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer closeSources()
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", sock)
	}}
	defer transport.CloseIdleConnections()
	res, e := (&http.Client{Transport: transport}).Post("http://source/", "application/json", strings.NewReader(`{"operation":"sources"}`))
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	var out map[string]any
	if json.NewDecoder(res.Body).Decode(&out) != nil || res.StatusCode != 200 {
		t.Fatal("socket failed")
	}
	raw, _ := json.Marshal(out)
	if bytes.Contains(raw, []byte("hidden")) || bytes.Contains(raw, []byte(a.reference.Name())) {
		t.Fatalf("leaked config: %s", raw)
	}
	if !current() {
		t.Fatal("unexpected source invalidation")
	}
	a.mu.Lock()
	for i := range a.sourceRegistry.Sources {
		if a.sourceRegistry.Sources[i].ID == contextSource {
			a.sourceRegistry.Sources[i].AIHiddenWorkspaces = []string{a.wsID()}
		}
	}
	areas := a.pluginSourceAreasLocked()
	a.mu.Unlock()
	for _, ar := range areas {
		if ar.root == "context" || ar.source == contextSource {
			t.Fatal("hidden reference accessible through alias")
		}
	}

	a.mu.Lock()
	a.wsRevision++
	a.mu.Unlock()
	if current() {
		t.Fatal("old source set accepted")
	}
	closeSources()
	if _, e := os.Stat(filepath.Dir(sock)); !os.IsNotExist(e) {
		t.Fatal("snapshot directory retained", e)
	}
}

func TestPluginSourceHostAPI(t *testing.T) {
	a := testApp(t)
	os.WriteFile(filepath.Join(a.reference.Name(), "probe.txt"), []byte("reference bytes"), 0600)
	code := `module.exports={apply(ctx){ctx.tool({name:'source_probe',handler:async(args,api)=>{const s=await api.sources({operation:'stage',source:'context',path:'probe.txt',slot:'path'});return {text:require('fs').readFileSync(require('path').join(s.workspace,s.path),'utf8'),root:s.workspace,sources:await api.sources({operation:'sources'})}}});}};`
	requireStatus(t, request(a, "POST", "/api/plugins", map[string]any{"id": "source-probe", "name": "probe", "code": code}), 201)
	raw, e := a.callPluginToolContext(context.Background(), "source-probe", "source_probe", map[string]any{})
	if e != nil {
		t.Fatal(e)
	}
	result := raw.(map[string]any)
	if result["text"] != "reference bytes" {
		t.Fatal(result)
	}
	if _, e := os.Stat(result["root"].(string)); !os.IsNotExist(e) {
		t.Fatal("snapshot was not cleaned")
	}
}

func TestSSHLivePluginSourceSnapshots(t *testing.T) {
	host := os.Getenv("AIDE_SSH_FIXTURE_A")
	if host == "" {
		t.Skip("isolated SSH fixture required")
	}
	if os.Getenv("AIDE_SSH_TEST_ISOLATED") != "1" {
		t.Fatal("isolation required")
	}
	a := testApp(t)
	requireStatus(t, request(a, "PUT", "/api/workspace-config", map[string]any{"workspace": map[string]any{"mode": "ssh", "host": host, "port": 22, "username": "fixture", "auth": "password", "path": "/home/fixture"}, "password": "fixture-pass"}), 200)
	ctx := context.Background()
	if e := a.ensureSSHSession(ctx); e != nil {
		t.Fatal(e)
	}
	var out, stderr bytes.Buffer
	run := "compare-source-" + newID()
	command := "mkdir -p /home/fixture/compare-source/folder; printf 'remote original\\n' > /home/fixture/compare-source/folder/a.txt; printf hidden > '/home/fixture/compare-source/folder/.hidden  file.txt'; ln -s folder /home/fixture/compare-source/link"
	command = strings.ReplaceAll(command, "compare-source", run)
	if status, e := a.execRemote(ctx, command, &out, &stderr); e != nil || status != 0 {
		t.Fatal(status, e, stderr.String())
	}
	a.mu.Lock()
	areas := a.pluginSourceAreasLocked()
	a.mu.Unlock()
	var n int64
	root := t.TempDir()
	value, e := a.pluginSourceRequest(ctx, areas, root, &n, map[string]bool{}, pluginSourceRequest{Operation: "stage", Source: "workspace", Path: run + "/folder", Slot: "left", Recursive: true})
	if e != nil {
		t.Fatal(e)
	}
	r := value.(map[string]any)
	b, e := os.ReadFile(filepath.Join(root, r["path"].(string), "a.txt"))
	if e != nil || string(b) != "remote original\n" {
		t.Fatal(string(b), e)
	}
	if b, e := os.ReadFile(filepath.Join(root, r["path"].(string), ".hidden  file.txt")); e != nil || string(b) != "hidden" {
		t.Fatal("remote hidden/spaced file lost", string(b), e)
	}
	src := Source{ID: "plugin-remote-ref", Name: "SSH reference fixture", Type: "sftp", Enabled: true, Config: SourceConfig{Host: host, Port: 22, Username: "fixture", Auth: "password", Path: "/home/fixture/" + run + "/folder"}}
	a.mu.Lock()
	a.sourceRegistry.Sources = append(a.sourceRegistry.Sources, src)
	a.storeSourceCredentialLocked(src.ID, "fixture-pass", "")
	a.mu.Unlock()
	if e := a.ensureSourceSession(ctx, src); e != nil {
		t.Fatal("independent SFTP", e)
	}
	if pluginNativeSourceFixture(t, a) {
		os.WriteFile(filepath.Join(a.reference.Name(), "b.txt"), []byte("local reference\n"), 0600)
		args := map[string]any{"left": run + "/folder/a.txt", "right": "b.txt", "rightSource": "context"}
		raw, e := a.callPluginToolContext(ctx, "compare", "compare_files", args)
		if e != nil {
			t.Fatal("SSH native comparison", e)
		}
		result := raw.(map[string]any)
		if result["byteEqual"] != false || result["sources"] == nil {
			t.Fatal(result)
		}
		if _, e := a.callPluginToolContext(ctx, "compare", "compare_report", map[string]any{"request": map[string]any{"operation": "files", "arguments": args}, "format": "evidence", "expectedComparisonId": result["comparisonId"]}); e != nil {
			t.Fatal("SSH evidence export", e)
		}
		if raw, e := a.callPluginToolContext(ctx, "compare", "compare_files", map[string]any{"left": run + "/folder/a.txt", "right": "a.txt", "rightSource": "source:" + src.ID}); e != nil || raw.(map[string]any)["byteEqual"] != true {
			t.Fatal("workspace SSH to independent SFTP", raw, e)
		}
		if _, e := a.callPluginToolContext(ctx, "compare", "compare_directories", map[string]any{"left": run + "/folder", "right": ".", "rightSource": "source:" + src.ID}); e != nil {
			t.Fatal("remote directories", e)
		}
		for _, kind := range []string{"images", "documents"} {
			name := "media.png"
			var left, right bytes.Buffer
			if kind == "images" {
				im := image.NewNRGBA(image.Rect(0, 0, 4, 4))
				png.Encode(&left, im)
				im.Set(1, 1, color.NRGBA{255, 0, 0, 255})
				png.Encode(&right, im)
			} else {
				name = "media.docx"
				for i, buf := range []*bytes.Buffer{&left, &right} {
					z := zip.NewWriter(buf)
					cw, _ := z.Create("[Content_Types].xml")
					cw.Write([]byte(`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`))
					w, _ := z.Create("word/document.xml")
					w.Write([]byte("<document><p><t>" + []string{"old", "new"}[i] + "</t></p></document>"))
					z.Close()
				}
			}
			if e := a.sftpWrite("/home/fixture/"+run+"/folder/"+name, left.Bytes()); e != nil {
				t.Fatal(e)
			}
			os.WriteFile(filepath.Join(a.reference.Name(), name), right.Bytes(), 0600)
			args := map[string]any{"left": run + "/folder/" + name, "right": name, "rightSource": "context"}
			raw, e := a.callPluginToolContext(ctx, "compare", "compare_"+kind, args)
			if e != nil {
				t.Fatal(kind, e)
			}
			v := raw.(map[string]any)
			if kind == "images" && v["changedPixels"] != float64(1) || kind == "documents" && v["changedRows"] != float64(1) {
				t.Fatal("remote media metric", kind, v)
			}
			if _, e := a.callPluginToolContext(ctx, "compare", "compare_report", map[string]any{"request": map[string]any{"operation": kind, "arguments": args}, "format": "evidence", "expectedComparisonId": v["comparisonId"]}); e != nil {
				t.Fatal("remote media report", kind, e)
			}
		}

	}
	for _, p := range []string{run + "/link", run + "/link/child"} {
		if _, e := a.pluginSourceRequest(ctx, areas, root, &n, map[string]bool{}, pluginSourceRequest{Operation: "browse", Source: "workspace", Path: p}); e == nil {
			t.Fatal("followed remote symlink", p)
		}
	}
}

func TestPluginWorkspaceCacheKeyIsDurableAndScoped(t *testing.T) {
	ar := knowledgeArea{root: "workspace", scope: "ssh|project|host", kind: "ssh", origin: "process-one"}
	ar.ws.Workspace.Mode = "ssh"
	ar.ws.Workspace.Host = "host"
	ar.ws.Workspace.Port = 22
	ar.ws.Workspace.Username = "first"
	ar.ws.Workspace.Path = "project"
	key := pluginSourceLabel(ar)["workspaceKey"]
	if key == nil {
		t.Fatal("missing durable key")
	}
	ar.origin = "process-two"
	ar.epoch++
	ar.generation++
	if pluginSourceLabel(ar)["workspaceKey"] != key {
		t.Fatal("runtime restart invalidated cache identity")
	}
	ar.ws.Workspace.Username = "second"
	if pluginSourceLabel(ar)["workspaceKey"] == key {
		t.Fatal("SSH users share cache identity")
	}
	ar.ws.Workspace.Username = "first"
	ar.ws.Workspace.Port = 2222
	if pluginSourceLabel(ar)["workspaceKey"] == key {
		t.Fatal("SSH ports share cache identity")
	}
	ar.root = "source"
	ar.source = "ref"
	if _, ok := pluginSourceLabel(ar)["workspaceKey"]; ok {
		t.Fatal("non-workspace source exposes workspace key")
	}
}
