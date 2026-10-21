package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fileHandlerFixture(t *testing.T) (*App, string) {
	t.Helper()
	a := testApp(t)
	entries := map[string]string{"manifest.json": `{"id":"file-fixture","name":"File fixture","version":"1.0.0","main":"index.js","workbench":{"protocol":1,"entry":"viewer.html","title":"File preview","fileHandler":{"extensions":[".demo",".md",".stl"],"editable":true,"priority":5}}}`, "index.js": `module.exports={apply(ctx){ctx.slot({id:'file-preview',name:'File preview'});}};`, "viewer.html": "<!doctype html><title>fixture</title>", "assets/empty.wasm": string([]byte{0, 97, 115, 109, 1, 0, 0, 0})}
	requireStatus(t, pluginBundleRequest(t, a, entries), 201)
	os.WriteFile(filepath.Join(a.workPath, "drawing.demo"), []byte("original"), 0640)
	os.Chmod(filepath.Join(a.workPath, "drawing.demo"), 0640)
	key := openDocumentFixture(t, a, "file-fixture", pluginDocumentSelection{Path: "drawing.demo", Root: "workspace"})
	return a, key
}
func openDocumentFixture(t *testing.T, a *App, id string, s pluginDocumentSelection) string {
	t.Helper()
	out := request(a, "POST", "/api/plugins/"+id+"/workbench/open", map[string]any{"document": s})
	requireStatus(t, out, 200)
	var result struct {
		URL    string `json:"url"`
		Bridge struct {
			Key string `json:"instanceId"`
		} `json:"bridge"`
	}
	if e := json.Unmarshal(out.Body.Bytes(), &result); e != nil {
		t.Fatal(e)
	}
	page := workbenchRequest(a, "GET", result.URL, false)
	requireStatus(t, page, 200)
	csp := page.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "'wasm-unsafe-eval'") || strings.Contains(csp, "'unsafe-eval'") || !strings.Contains(csp, "connect-src 'none'") {
		t.Fatal("file preview CSP boundary", csp)
	}
	if result.Bridge.Key == "" {
		t.Fatal("document without tools needs bridge")
	}
	return result.Bridge.Key
}
func documentCall(a *App, key, id, method string, args map[string]any) *httptest.ResponseRecorder {
	return request(a, "POST", "/api/plugins/file-fixture/workbench/"+key+"/call", map[string]any{"protocol": 1, "requestId": id, "method": method, "arguments": args})
}
func pluginDocumentTestResult(t *testing.T, a *App, key, id, method string, args map[string]any) map[string]any {
	t.Helper()
	out := documentCall(a, key, id, method, args)
	requireStatus(t, out, 200)
	var result struct {
		Result map[string]any `json:"result"`
	}
	if e := json.Unmarshal(out.Body.Bytes(), &result); e != nil {
		t.Fatal(e)
	}
	return result.Result
}
func prepareDocumentSave(t *testing.T, a *App, key, version string, data []byte) {
	t.Helper()
	pluginDocumentTestResult(t, a, key, "begin", "document.save.begin", map[string]any{"size": len(data), "version": version})
	for off := 0; off < len(data); off += pluginDocumentChunk {
		end := off + pluginDocumentChunk
		if end > len(data) {
			end = len(data)
		}
		pluginDocumentTestResult(t, a, key, fmt.Sprintf("chunk_%d", off), "document.save.chunk", map[string]any{"offset": off, "data": base64.StdEncoding.EncodeToString(data[off:end])})
	}
	pluginDocumentTestResult(t, a, key, "ready", "document.save.request", nil)
}
func TestPluginFileHandlerManifestResolution(t *testing.T) {
	a, _ := fileHandlerFixture(t)
	for _, h := range []*PluginFileHandler{{Extensions: []string{"*"}}, {Extensions: []string{".STL"}}, {Extensions: []string{"../x"}}, {Extensions: []string{".x", ".x"}}, {Extensions: []string{".x"}, Priority: 101}} {
		if validatePluginFileHandler(h) == nil {
			t.Fatal("invalid extension accepted", h)
		}
	}
	out := request(a, "GET", "/api/plugins/file-handlers?path=drawing.demo&root=workspace", nil)
	requireStatus(t, out, 200)
	if !strings.Contains(out.Body.String(), "file-fixture") {
		t.Fatal(out.Body.String())
	}
	requireStatus(t, request(a, "PUT", "/api/plugins/file-fixture", map[string]any{"enabled": false}), 200)
	out = request(a, "GET", "/api/plugins/file-handlers?path=drawing.demo&root=workspace", nil)
	if strings.Contains(out.Body.String(), "file-fixture") {
		t.Fatal("disabled handler still resolves")
	}
}
func TestPluginDocumentRangeAssetsAndScope(t *testing.T) {
	a, key := fileHandlerFixture(t)
	info := pluginDocumentTestResult(t, a, key, "info", "document.info", nil)
	if info["editable"] != true || info["version"] != hash([]byte("original")) {
		t.Fatal(info)
	}
	data := pluginDocumentTestResult(t, a, key, "range", "document.read", map[string]any{"offset": 1, "length": 3})
	if data["data"] != base64.StdEncoding.EncodeToString([]byte("rig")) {
		t.Fatal(data)
	}
	os.WriteFile(filepath.Join(a.workPath, "texture.bin"), []byte{0, 1, 255}, 0600)
	asset := pluginDocumentTestResult(t, a, key, "asset", "document.read", map[string]any{"asset": "texture.bin"})
	if asset["data"] != "AAH/" {
		t.Fatal(asset)
	}
	for i, p := range []string{"../secrets", "/etc/passwd", "https://evil/file", "a/../../b", ".git/config"} {
		requireStatus(t, documentCall(a, key, fmt.Sprintf("escape_%d", i), "document.read", map[string]any{"asset": p}), 409)
	}
	requireStatus(t, documentCall(a, key, "too_big", "document.read", map[string]any{"length": pluginDocumentChunk + 1}), 409)
	requireStatus(t, documentCall(a, key, "asset", "document.read", nil), 409)
	os.Symlink(a.workPath, filepath.Join(a.workPath, "linked"))
	requireStatus(t, documentCall(a, key, "symlink", "document.read", map[string]any{"asset": "linked/texture.bin"}), 409)
	requireStatus(t, request(a, "PUT", "/api/plugins/file-fixture", map[string]any{"enabled": false}), 200)
	requireStatus(t, documentCall(a, key, "revoked", "document.info", nil), 403)
}
func TestPluginDocumentPackageAssetsStayInOwnPackage(t *testing.T) {
	a, key := fileHandlerFixture(t)
	result := pluginDocumentTestResult(t, a, key, "wasm", "document.package.read", map[string]any{"asset": "assets/empty.wasm"})
	if result["data"] != "AGFzbQEAAAA=" {
		t.Fatal(result)
	}
	for i, p := range []string{"index.js", "../manifest.json", "assets/../index.js", "assets/.private/key", "/etc/passwd"} {
		requireStatus(t, documentCall(a, key, fmt.Sprintf("invalid_asset_%d", i), "document.package.read", map[string]any{"asset": p}), 409)
	}
}
func TestPluginDocumentBinarySaveVersionAndCleanup(t *testing.T) {
	a, key := fileHandlerFixture(t)
	data := bytes.Repeat([]byte{0, 255, 42}, 400000)
	prepareDocumentSave(t, a, key, hash([]byte("original")), data)
	before, _ := os.ReadFile(filepath.Join(a.workPath, "drawing.demo"))
	if string(before) != "original" {
		t.Fatal("staging wrote original")
	}
	result := pluginDocumentTestResult(t, a, key, "user_commit", "document.save.commit", nil)
	if result["saved"] != true || result["version"] != hash(data) {
		t.Fatal(result)
	}
	after, _ := os.ReadFile(filepath.Join(a.workPath, "drawing.demo"))
	if !bytes.Equal(data, after) {
		t.Fatal("binary save differs")
	}
	st, _ := os.Stat(filepath.Join(a.workPath, "drawing.demo"))
	if st.Mode().Perm() != 0640 {
		t.Fatal("mode changed", st.Mode())
	}
	files, _ := os.ReadDir(filepath.Join(a.dataPath, "plugin-document-staging"))
	if len(files) != 0 {
		t.Fatal("stage not removed")
	}
}
func TestPluginDocumentConflictingSavePreservesFile(t *testing.T) {
	a, key := fileHandlerFixture(t)
	prepareDocumentSave(t, a, key, hash([]byte("original")), []byte("plugin change"))
	os.WriteFile(filepath.Join(a.workPath, "drawing.demo"), []byte("external change"), 0640)
	requireStatus(t, documentCall(a, key, "conflict", "document.save.commit", nil), 409)
	actual, _ := os.ReadFile(filepath.Join(a.workPath, "drawing.demo"))
	if string(actual) != "external change" {
		t.Fatal("external edit lost")
	}
	pluginDocumentTestResult(t, a, key, "abort", "document.save.abort", nil)
}
func TestPluginDocumentReadOnlyReferenceAndKnowledge(t *testing.T) {
	a, _ := fileHandlerFixture(t)
	sourceRoot := a.reference.Name()
	os.WriteFile(filepath.Join(sourceRoot, "model.demo"), []byte("reference"), 0600)
	a.mu.Lock()
	a.sourceRegistry.Sources = append(a.sourceRegistry.Sources, Source{ID: "viewer-ro", Type: "local", Enabled: true, RW: false, Config: SourceConfig{Path: "/context"}})
	a.mu.Unlock()
	key := openDocumentFixture(t, a, "file-fixture", pluginDocumentSelection{Path: "model.demo", Root: "context", Source: "viewer-ro"})
	info := pluginDocumentTestResult(t, a, key, "info", "document.info", nil)
	if info["editable"] != false {
		t.Fatal("read-only reference editable")
	}
	requireStatus(t, documentCall(a, key, "write", "document.save.begin", map[string]any{"size": 1, "version": hash([]byte("reference"))}), 403)
	out := request(a, "POST", "/api/plugins/file-fixture/workbench/open", map[string]any{"document": pluginDocumentSelection{Path: "drawing.demo", Root: "workspace", Origin: "indexed", KnowledgeWorkspace: "other"}})
	requireStatus(t, out, 403)
}
func TestPluginDocumentCloseAndWorkspaceRevocation(t *testing.T) {
	a, key := fileHandlerFixture(t)
	pluginDocumentTestResult(t, a, key, "begin", "document.save.begin", map[string]any{"size": 1, "version": hash([]byte("original"))})
	requireStatus(t, request(a, "DELETE", "/api/plugins/file-fixture/workbench/"+key, nil), 200)
	files, _ := os.ReadDir(filepath.Join(a.dataPath, "plugin-document-staging"))
	if len(files) != 0 {
		t.Fatal("close leaked draft")
	}
	key = openDocumentFixture(t, a, "file-fixture", pluginDocumentSelection{Path: "drawing.demo", Root: "workspace"})
	a.mu.Lock()
	a.wsRevision++
	a.mu.Unlock()
	requireStatus(t, documentCall(a, key, "wrong_workspace", "document.info", nil), 403)
}

func TestPluginDocumentRenewPreservesDraftAndRevokesOldKey(t *testing.T) {
	a, key := fileHandlerFixture(t)
	pluginDocumentTestResult(t, a, key, "begin", "document.save.begin", map[string]any{"size": 3, "version": hash([]byte("original"))})
	out := request(a, "POST", "/api/plugins/file-fixture/workbench/"+key+"/renew", map[string]any{})
	requireStatus(t, out, 200)
	var next struct {
		Key string `json:"instanceId"`
	}
	json.Unmarshal(out.Body.Bytes(), &next)
	requireStatus(t, documentCall(a, key, "stale", "document.info", nil), 403)
	pluginDocumentTestResult(t, a, next.Key, "chunk", "document.save.chunk", map[string]any{"offset": 0, "data": "YWJj"})
	pluginDocumentTestResult(t, a, next.Key, "ready", "document.save.request", nil)
	pluginDocumentTestResult(t, a, next.Key, "commit", "document.save.commit", nil)
	got, _ := os.ReadFile(filepath.Join(a.workPath, "drawing.demo"))
	if string(got) != "abc" {
		t.Fatal("renewal lost draft")
	}
}
func TestPluginDocumentReferenceRevocationPreservesOriginal(t *testing.T) {
	a, _ := fileHandlerFixture(t)
	os.WriteFile(filepath.Join(a.reference.Name(), "reference.demo"), []byte("ref"), 0600)
	a.mu.Lock()
	a.sourceRegistry.Sources = append(a.sourceRegistry.Sources, Source{ID: "write-ref", Type: "local", Enabled: true, RW: true, Config: SourceConfig{Path: "/context"}})
	a.mu.Unlock()
	key := openDocumentFixture(t, a, "file-fixture", pluginDocumentSelection{Path: "reference.demo", Root: "context", Source: "write-ref"})
	prepareDocumentSave(t, a, key, hash([]byte("ref")), []byte("modified"))
	a.mu.Lock()
	for i := range a.sourceRegistry.Sources {
		if a.sourceRegistry.Sources[i].ID == "write-ref" {
			a.sourceRegistry.Sources[i].RW = false
		}
	}
	a.mu.Unlock()
	requireStatus(t, documentCall(a, key, "revoked", "document.save.commit", nil), 403)
	got, _ := os.ReadFile(filepath.Join(a.reference.Name(), "reference.demo"))
	if string(got) != "ref" {
		t.Fatal("revoked write changed original")
	}
}

func TestPluginDocumentLargeOffsetRange(t *testing.T) {
	a, key := fileHandlerFixture(t)
	f, e := os.OpenFile(filepath.Join(a.workPath, "drawing.demo"), os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	const offset int64 = 5 << 30
	if e = f.Truncate(offset + 3); e != nil {
		t.Fatal(e)
	}
	if _, e = f.WriteAt([]byte("end"), offset); e != nil {
		t.Fatal(e)
	}
	f.Close()
	info := pluginDocumentTestResult(t, a, key, "large_info", "document.info", map[string]any{"metadataOnly": true})
	if info["size"] != float64(offset+3) || info["version"] != nil {
		t.Fatal(info)
	}
	got := pluginDocumentTestResult(t, a, key, "large_range", "document.read", map[string]any{"offset": offset, "length": 3})
	if got["data"] != "ZW5k" || got["eof"] != true {
		t.Fatal(got)
	}
}
func TestPluginDocumentSFTPAtomicSave(t *testing.T) {
	a, _ := fileHandlerFixture(t)
	remote := t.TempDir()
	bin, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	stub := filepath.Join(t.TempDir(), "owned-ssh")
	script := "#!/bin/sh\nfor arg do if [ \"$arg\" = sftp ]; then exec env AIDE_TRANSFER_SFTP_HELPER=1 " + shellQuote(bin) + " -test.run='^TestTransferSFTPProcess$'; fi; done\nexit 0\n"
	if e = os.WriteFile(stub, []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
	a.sshBin = stub
	a.wsConfig.Workspace.Mode = "ssh"
	a.wsConfig.Workspace.Host = "owned.invalid"
	a.wsConfig.Workspace.Path = remote
	t.Cleanup(func() { a.sshBin = "" })
	os.WriteFile(filepath.Join(remote, "drawing.demo"), []byte("remote original"), 0640)
	key := openDocumentFixture(t, a, "file-fixture", pluginDocumentSelection{Path: "drawing.demo", Root: "workspace"})
	data := bytes.Repeat([]byte{1, 0, 255}, 200000)
	prepareDocumentSave(t, a, key, hash([]byte("remote original")), data)
	pluginDocumentTestResult(t, a, key, "save_remote", "document.save.commit", nil)
	got, _ := os.ReadFile(filepath.Join(remote, "drawing.demo"))
	if !bytes.Equal(got, data) {
		t.Fatal("remote bytes differ")
	}
}

func TestPluginDocumentAppCloseCancelsInFlightRead(t *testing.T) {
	a, key := fileHandlerFixture(t)
	f, e := os.OpenFile(filepath.Join(a.workPath, "drawing.demo"), os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.Truncate(5 << 30); e != nil {
		t.Fatal(e)
	}
	f.Close()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- documentCall(a, key, "slow_hash", "document.info", nil) }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		a.mu.Lock()
		b := a.pluginWorkbenchBridges[key]
		busy := b != nil && b.Busy
		a.mu.Unlock()
		if busy {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("read did not begin")
		}
		time.Sleep(time.Millisecond)
	}
	a.Close()
	select {
	case result := <-done:
		if result.Code == 200 {
			t.Fatal("read was not revoked")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("close did not cancel read")
	}
}
