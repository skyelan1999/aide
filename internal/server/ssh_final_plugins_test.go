package server

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func finalCandidate(t *testing.T, a *App, name string) {
	t.Helper()
	dir := os.Getenv("AIDE_FINAL_PLUGIN_CANDIDATES")
	if dir == "" {
		t.Fatal("candidate directory required for opted-in live test")
	}
	b, e := os.ReadFile(filepath.Join(dir, name))
	if e != nil {
		t.Fatal(e)
	}
	m, files, e := parsePluginBundle(b)
	if e != nil {
		t.Fatal(e)
	}
	for p, b := range files {
		f := filepath.Join(a.pluginsPath, m.ID, p)
		if e = os.MkdirAll(filepath.Dir(f), 0700); e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(f, b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	m.Enabled = true
	a.mu.Lock()
	a.pluginRegistry.Plugins = append(a.pluginRegistry.Plugins, m)
	a.mu.Unlock()
	t.Logf("installed candidate %s version=%s sha256=%s", m.ID, m.Version, hash(b))
}
func TestSSHLiveFinalPluginCompatibility(t *testing.T) {
	hostA, hostB := os.Getenv("AIDE_SSH_FIXTURE_A"), os.Getenv("AIDE_SSH_FIXTURE_B")
	if hostA == "" || hostB == "" {
		t.Skip("dual isolated SSH fixture required")
	}
	if os.Getenv("AIDE_SSH_TEST_ISOLATED") != "1" {
		t.Fatal("isolated fixture required")
	}
	a := testApp(t)
	useSourceOfficeScripts(t)
	ctx := context.Background()
	requireStatus(t, request(a, "PUT", "/api/workspace-config", map[string]any{"workspace": map[string]any{"mode": "ssh", "host": hostA, "port": 22, "username": "fixture", "auth": "password", "path": "/home/fixture"}, "password": "fixture-pass"}), 200)
	if e := a.ensureSSHSession(ctx); e != nil {
		t.Fatal(e)
	}
	run := "final-plugins-" + newID()
	var stdout, stderr bytes.Buffer
	if code, e := a.execRemote(ctx, "mkdir -p /home/fixture/"+run, &stdout, &stderr); e != nil || code != 0 {
		t.Fatal("fixture mkdir", e)
	}
	local := filepath.Join(t.TempDir(), "probe.db")
	script := `const {DatabaseSync}=require('node:sqlite');let d=new DatabaseSync(process.argv[1]);d.exec("CREATE TABLE values_table(v TEXT);INSERT INTO values_table VALUES('WORKSPACE-A')");d.close();`
	if out, e := exec.Command("node", "-e", script, local).CombinedOutput(); e != nil {
		t.Fatal(e, string(out))
	}
	db, e := os.ReadFile(local)
	if e != nil {
		t.Fatal(e)
	}
	write := func(p string, b []byte) {
		t.Helper()
		fs, e := a.streamTransferFS(ctx, "workspace", true)
		if e != nil {
			t.Fatal(e)
		}
		defer fs.Close()
		if e = fs.writeAtomic(run+"/"+p, b, false); e != nil {
			t.Fatal(e)
		}
	}
	write("probe.db", db)
	write("graph.md", []byte("# RemoteGraphFinalFixture\n\nOnly the selected SSH workspace."))
	doc, e := officeCreateBytes("docx", map[string]any{"blocks": []any{map[string]any{"type": "paragraph", "text": "Remote paragraph for SSH."}}})
	if e != nil {
		t.Fatal(e)
	}
	write("probe.docx", doc)
	image, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQIHWP4z8DwHwAFgAI/ScLbtAAAAABJRU5ErkJggg==")
	write("image.png", image)
	src := Source{ID: "final-ssh-reference", Name: "Independent B", Type: "sftp", Enabled: true, RW: true, Config: SourceConfig{Host: hostB, Port: 22, Username: "fixture", Auth: "password", Path: "/home/fixture"}}
	a.mu.Lock()
	a.sourceRegistry.Sources = append(a.sourceRegistry.Sources, src)
	a.storeSourceCredentialLocked(src.ID, "fixture-pass", "")
	a.mu.Unlock()
	if e = a.ensureSourceSession(ctx, src); e != nil {
		t.Fatal(e)
	}
 refFS,e:=a.streamTransferFS(ctx,src.ID,true);if e!=nil{t.Fatal(e)};if e=refFS.MkdirAll(run+"-reference");e!=nil{refFS.Close();t.Fatal(e)};refFS.Close()
 src.Config.Path="/home/fixture/"+run+"-reference"
 a.mu.Lock();for i:=range a.sourceRegistry.Sources{if a.sourceRegistry.Sources[i].ID==src.ID{a.sourceRegistry.Sources[i]=src}};a.mu.Unlock()
 if e=a.ensureSourceSession(ctx,src);e!=nil{t.Fatal(e)}
	if e = a.sftpWriteSource(src, "probe.db", db); e != nil {
		t.Fatal(e)
	}
	if e = a.sftpWriteSource(src, "image.png", image); e != nil {
		t.Fatal(e)
	}
	if e = a.sftpWriteSource(src, "probe.docx", doc); e != nil {
		t.Fatal(e)
	}
	a.mu.Lock()
	for i := range a.sourceRegistry.Sources {
		if a.sourceRegistry.Sources[i].ID == src.ID {
			a.sourceRegistry.Sources[i].RW = false
		}
	}
	a.mu.Unlock()
	finalCandidate(t, a, "aide-pkg-sqlite-v1.0.3.zip")
	finalCandidate(t, a, "aide-pkg-StarLink-v0.1.6.zip")
	a.runPluginHost(ctx)
	call := func(name string, args map[string]any) map[string]any {
		t.Helper()
		raw, e := a.callPluginToolContext(ctx, "sqlite", name, args)
		if e != nil {
			t.Fatal(name, e)
		}
		return raw.(map[string]any)
	}
	args := map[string]any{"db": run + "/probe.db", "sql": "SELECT v FROM values_table"}
	if got := call("sqlite_query", args)["rows"].([]any)[0].(map[string]any)["v"]; got != "WORKSPACE-A" {
		t.Fatal("wrong workspace", got)
	}
	call("sqlite_transaction", map[string]any{"db": run + "/probe.db", "statements": []any{map[string]any{"sql": "INSERT INTO values_table VALUES(?)", "params": []any{"COMMITTED-A"}}}})
	referenceArgs := map[string]any{"source": "source:" + src.ID, "db": "probe.db", "sql": "SELECT COUNT(*) AS n FROM values_table"}
	if got := call("sqlite_query", referenceArgs)["rows"].([]any)[0].(map[string]any)["n"]; got != float64(1) {
		t.Fatal("independent DB changed", got)
	}
	if out := a.docxToolSourceBound(ctx, a.workspace, "ssh", a.wsID(), "/home/fixture", src.ID, "probe.docx", "docx_structure.py"); !strings.Contains(out, "Remote paragraph") {
		t.Fatal("RO reference structure", out)
	}
	if out := a.officeCommentToolSourceBound(ctx, a.workspace, "ssh", a.wsID(), "/home/fixture", src.ID, "probe.docx", "add", map[string]any{"quote": "Remote paragraph", "text": "DENIED", "author": "Fixture"}); !strings.Contains(out, "失败") {
		t.Fatal("RO Office write accepted", out)
	}
	if _, e = a.callPluginToolContext(ctx, "sqlite", "sqlite_execute", map[string]any{"source": "source:" + src.ID, "db": "probe.db", "script": "INSERT INTO values_table VALUES('DENIED')"}); e == nil {
		t.Fatal("RO reference write accepted")
	}
	a.mu.Lock()
	for i := range a.sourceRegistry.Sources {
		if a.sourceRegistry.Sources[i].ID == src.ID {
			a.sourceRegistry.Sources[i].RW = true
		}
	}
	a.mu.Unlock()
	call("sqlite_export", map[string]any{"db": run + "/probe.db", "outputSource": "source:" + src.ID, "outputPath": "final-export.csv", "query": "SELECT * FROM values_table"})
	csv, e := a.sftpReadSource(src, "final-export.csv")
	if e != nil || !bytes.Contains(csv, []byte("COMMITTED-A")) {
		t.Fatal("remote CSV bytes", e)
	}
	refStructure := a.docxToolSourceBound(ctx, a.workspace, "ssh", a.wsID(), "/home/fixture", src.ID, "probe.docx", "docx_structure.py")
	if !strings.Contains(refStructure, "Remote paragraph") {
		t.Fatal("independent reference structure", refStructure)
	}
	refAdded := a.officeCommentToolSourceBound(ctx, a.workspace, "ssh", a.wsID(), "/home/fixture", src.ID, "probe.docx", "add", map[string]any{"quote": "Remote paragraph", "text": "Independent reference comment", "author": "Fixture"})
	var refComment struct {
		ID int `json:"id"`
	}
	if e = json.Unmarshal([]byte(refAdded), &refComment); e != nil {
		t.Fatal("reference comment", refAdded)
	}
	refResolved := a.docxToolSourceBound(ctx, a.workspace, "ssh", a.wsID(), "/home/fixture", src.ID, "probe.docx", "docx_resolve_comment.py", strconv.Itoa(refComment.ID))
	if !strings.Contains(refResolved, "true") {
		t.Fatal("reference resolve", refResolved)
	}
	refImage, e := a.readLocalImage(Attachment{Root: "source", Source: src.ID, Path: "image.png"})
	if e != nil || refImage.DataURL != "data:image/png;base64,"+base64.StdEncoding.EncodeToString(image) {
		t.Fatal("reference image", e)
	}
	if e = a.sftpWriteSource(src, "probe.db-wal", []byte("live-writer")); e != nil {
		t.Fatal(e)
	}
	if _, e = a.callPluginToolContext(ctx, "sqlite", "sqlite_query", referenceArgs); e == nil {
		t.Fatal("active WAL accepted")
	}
	a.mu.Lock()
	for i := range a.sourceRegistry.Sources {
		if a.sourceRegistry.Sources[i].ID == src.ID {
			a.sourceRegistry.Sources[i].AIHiddenWorkspaces = []string{a.wsID()}
		}
	}
	a.mu.Unlock()
	if _, e = a.callPluginToolContext(ctx, "sqlite", "sqlite_query", referenceArgs); e == nil {
		t.Fatal("hidden reference read accepted")
	}
	wid := a.wsID()
	structure := a.docxToolBound(ctx, a.workspace, "ssh", wid, "/home/fixture", run+"/probe.docx", "docx_structure.py")
	if !strings.Contains(structure, "Remote paragraph") {
		t.Fatal("remote structure", structure)
	}
	added := a.officeCommentToolBound(ctx, a.workspace, "ssh", wid, "/home/fixture", run+"/probe.docx", "add", map[string]any{"quote": "Remote paragraph", "text": "SSH comment", "author": "Fixture"})
	var comment struct {
		ID int `json:"id"`
	}
	if e = json.Unmarshal([]byte(added), &comment); e != nil {
		t.Fatal("remote comment", added)
	}
	resolved := a.docxToolBound(ctx, a.workspace, "ssh", wid, "/home/fixture", run+"/probe.docx", "docx_resolve_comment.py", strconv.Itoa(comment.ID))
	if !strings.Contains(resolved, `"resolved": true`) && !strings.Contains(resolved, `"resolved":true`) {
		t.Fatal("remote resolve", resolved)
	}
	raw, e := a.sftpRead("/home/fixture/" + run + "/probe.docx")
	if e != nil || !strings.Contains(officePart(t, raw, "word/commentsExtended.xml"), `done="1"`) {
		t.Fatal("remote resolved bytes", e)
	}
	if text, e := a.extractDocumentText(Attachment{Root: "workspace", Path: run + "/probe.docx"}); e != nil || !strings.Contains(text, "Remote paragraph") {
		t.Fatal("remote attachment text", text, e)
	}
	media, e := a.readLocalImage(Attachment{Root: "workspace", Path: run + "/image.png"})
	if e != nil || media.DataURL != "data:image/png;base64,"+base64.StdEncoding.EncodeToString(image) {
		t.Fatal("remote image bytes", e)
	}
	if out := a.docxToolBound(ctx, a.workspace, "ssh", wid+"stale", "/home/fixture", run+"/probe.docx", "docx_structure.py"); !strings.Contains(out, "已变化") {
		t.Fatal("stale workspace accepted", out)
	}
	// Real daemon -> authenticated local HTTPS host -> AI-scoped remote index.
	cert, key, e := ensureTLSCert(filepath.Join(a.dataPath, "certs"))
	if e != nil {
		t.Fatal(e)
	}
	pair, e := tls.LoadX509KeyPair(cert, key)
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewUnstartedServer(a.Handler())
	server.TLS = &tls.Config{Certificates: []tls.Certificate{pair}}
	server.StartTLS()
	defer server.Close()
	os.MkdirAll(filepath.Join(a.dataPath, "auth"), 0700)
	os.WriteFile(filepath.Join(a.dataPath, "auth/access-token"), []byte(a.token), 0600)
	releasePublicSettings(t, a, "starlink", map[string]any{"port": server.Listener.Addr().(*net.TCPAddr).Port, "dataDir": a.dataPath})
	result := releasePublicObject(t, releasePublicCall(t, a, "starlink", "starlink_search", map[string]any{"query": "RemoteGraphFinalFixture"}))
	if count, ok := result["totalMatches"].(float64); !ok || count < 1 {
		t.Fatal("remote StarLink graph", fmt.Sprint(result))
	}
	t.Log("PASS: actual candidate installation/Node calls, independent SSH reference, RO/AI visibility/WAL, CSV bytes, Office resolve, text/image attachments and SSH StarLink daemon")
}
