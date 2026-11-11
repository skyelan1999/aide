package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func fileAccessFixture(t *testing.T) (*App, string) { return fileAccessConfiguredFixture(t, nil) }
func fileAccessConfiguredFixture(t *testing.T, setup func(*App)) (*App, string) {
	t.Helper()
	a := testApp(t)
	if setup != nil {
		setup(a)
	}

	files := map[string]string{"manifest.json": `{"id":"file-access","name":"File access","version":"1.0.0","main":"index.js","workbench":{"protocol":1,"entry":"viewer.html","title":"Files","fileAccess":{"protocol":1,"extensions":[".pr",".md",".docx",".html"]}}}`, "index.js": `module.exports={apply(){}};`, "viewer.html": "<!doctype html><title>files</title>"}
	requireStatus(t, pluginBundleRequest(t, a, files), 201)
	opened := request(a, "POST", "/api/plugins/file-access/workbench/open", map[string]any{})
	requireStatus(t, opened, 200)
	var v struct {
		URL    string `json:"url"`
		Bridge struct {
			Key   string `json:"instanceId"`
			Files bool   `json:"fileAccess"`
		} `json:"bridge"`
	}
	json.Unmarshal(opened.Body.Bytes(), &v)
	if v.Bridge.Key == "" || !v.Bridge.Files {
		t.Fatal("missing opt-in file bridge", opened.Body.String())
	}
	requireStatus(t, workbenchRequest(a, "GET", v.URL, false), 200)
	return a, v.Bridge.Key
}
func fileAccessCall(a *App, key, id, method string, args map[string]any) *httptest.ResponseRecorder {
	return request(a, "POST", "/api/plugins/file-access/workbench/"+key+"/call", map[string]any{"protocol": 1, "requestId": id, "method": method, "arguments": args})
}
func fileAccessResult(t *testing.T, a *App, key, id, method string, args map[string]any) map[string]any {
	t.Helper()
	w := fileAccessCall(a, key, id, method, args)
	requireStatus(t, w, 200)
	var v struct {
		Result map[string]any `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	return v.Result
}
func fileAccessStage(t *testing.T, a *App, key, prefix, version string, data []byte) {
	t.Helper()
	fileAccessResult(t, a, key, prefix+"b", "document.save.begin", map[string]any{"size": len(data), "version": version})
	fileAccessResult(t, a, key, prefix+"c", "document.save.chunk", map[string]any{"offset": 0, "data": base64.StdEncoding.EncodeToString(data)})
	fileAccessResult(t, a, key, prefix+"r", "document.save.request", nil)
}

func TestWorkbenchFileAccessWorkspaceBinaryCreateAndConflict(t *testing.T) {
	a, key := fileAccessFixture(t)
	data := []byte{'P', 'R', 'D', 'B', 0, 255, 128, 10}
	selected := fileAccessResult(t, a, key, "bind", "files.select", map[string]any{"action": "bind", "source": "workspace", "path": "new.pr", "mode": "save", "version": ""})
	if selected["exists"] != false || selected["version"] != "" {
		t.Fatal(selected)
	}
	fileAccessStage(t, a, key, "one", "", data)
	if _, e := os.Stat(filepath.Join(a.workPath, "new.pr")); !os.IsNotExist(e) {
		t.Fatal("staging wrote the target")
	}
	saved := fileAccessResult(t, a, key, "commit", "document.save.commit", nil)
	if saved["saved"] != true {
		t.Fatal(saved)
	}
	b, e := os.ReadFile(filepath.Join(a.workPath, "new.pr"))
	if e != nil || !bytes.Equal(b, data) {
		t.Fatal(e, b)
	}
	info := fileAccessResult(t, a, key, "info", "document.info", nil)
	version := info["version"].(string)
	if version != hash(data) {
		t.Fatal(info)
	}
	fileAccessStage(t, a, key, "two", version, []byte("next"))
	os.WriteFile(filepath.Join(a.workPath, "new.pr"), []byte("external"), 0600)
	requireStatus(t, fileAccessCall(a, key, "conflict", "document.save.commit", nil), 409)
	b, _ = os.ReadFile(filepath.Join(a.workPath, "new.pr"))
	if string(b) != "external" {
		t.Fatal("conflict replaced target")
	}
}
func TestWorkbenchFileAccessAbsentTargetRaceDoesNotOverwrite(t *testing.T) {
	a, key := fileAccessFixture(t)
	fileAccessResult(t, a, key, "bind", "files.select", map[string]any{"action": "bind", "source": "workspace", "path": "race.pr", "mode": "save", "version": ""})
	fileAccessStage(t, a, key, "race", "", []byte("draft"))
	os.WriteFile(filepath.Join(a.workPath, "race.pr"), []byte("external"), 0600)
	requireStatus(t, fileAccessCall(a, key, "commit", "document.save.commit", nil), 409)
	b, _ := os.ReadFile(filepath.Join(a.workPath, "race.pr"))
	if string(b) != "external" {
		t.Fatal("new-file conflict overwrote target")
	}
}
func TestWorkbenchFileAccessReferencesAndReadOnly(t *testing.T) {
	for _, rw := range []bool{false, true} {
		t.Run(fmt.Sprint(rw), func(t *testing.T) {
			a, key := fileAccessFixture(t)
			dir := filepath.Join(a.workPath, "reference")
			os.MkdirAll(dir, 0700)
			os.WriteFile(filepath.Join(dir, "original.pr"), []byte("original"), 0600)
			a.mu.Lock()
			a.sourceRegistry.Sources = append(a.sourceRegistry.Sources, Source{ID: "files-ref", Name: "Fictional reference", Type: "local", Enabled: true, RW: rw, AIHiddenWorkspaces: []string{a.wsID()}, Config: SourceConfig{Path: "/workspace/reference"}})
			a.mu.Unlock()
			sources := fileAccessResult(t, a, key, "sources", "files.select", map[string]any{"action": "sources"})
			var found bool
			for _, v := range sources["sources"].([]any) {
				row := v.(map[string]any)
				if row["id"] == "source:files-ref" {
					found = true
					if row["writable"] != rw || row["config"] != nil {
						t.Fatal(row)
					}
				}
			}
			if !found {
				t.Fatal("manual source omitted")
			}
			selected := fileAccessResult(t, a, key, "open", "files.select", map[string]any{"action": "bind", "source": "source:files-ref", "path": "original.pr", "mode": "open"})
			if selected["editable"] != rw {
				t.Fatal(selected)
			}
			read := fileAccessResult(t, a, key, "read", "document.read", map[string]any{"offset": 0, "length": 100})
			if read["data"] != base64.StdEncoding.EncodeToString([]byte("original")) {
				t.Fatal(read)
			}
			if !rw {
				requireStatus(t, fileAccessCall(a, key, "save", "files.select", map[string]any{"action": "bind", "source": "source:files-ref", "path": "new.pr", "mode": "save", "version": ""}), 403)
				requireStatus(t, fileAccessCall(a, key, "begin", "document.save.begin", map[string]any{"size": 3, "version": hash([]byte("original"))}), 403)
				return
			}
			fileAccessResult(t, a, key, "save", "files.select", map[string]any{"action": "bind", "source": "source:files-ref", "path": "new.pr", "mode": "save", "version": ""})
			fileAccessStage(t, a, key, "new", "", []byte("encrypted"))
			fileAccessResult(t, a, key, "commit", "document.save.commit", nil)
			b, _ := os.ReadFile(filepath.Join(dir, "new.pr"))
			if string(b) != "encrypted" {
				t.Fatal("reference save", b)
			}
		})
	}
}
func TestWorkbenchFileAccessBrowseAndRejectionPaths(t *testing.T) {
	a, key := fileAccessFixture(t)
	os.MkdirAll(filepath.Join(a.workPath, "folder"), 0700)
	os.WriteFile(filepath.Join(a.workPath, "a.pr"), []byte("a"), 0600)
	os.WriteFile(filepath.Join(a.workPath, "hidden.txt"), []byte("x"), 0600)
	os.Symlink("a.pr", filepath.Join(a.workPath, "link.pr"))
	listed := fileAccessResult(t, a, key, "browse", "files.select", map[string]any{"action": "browse", "source": "workspace", "path": "."})
	for _, v := range listed["entries"].([]any) {
		row := v.(map[string]any)
		if row["name"] == "hidden.txt" || row["name"] == "link.pr" {
			t.Fatal(row)
		}
	}
	for i, name := range []string{"../escape.pr", "/absolute.pr", "hidden.txt", "link.pr", "folder/../a.pr", "missing/a.pr"} {
		w := fileAccessCall(a, key, fmt.Sprintf("reject_%d", i), "files.select", map[string]any{"action": "bind", "source": "workspace", "path": name, "mode": "save", "version": ""})
		if w.Code == 200 {
			t.Fatal("accepted", name)
		}
	}
	for _, f := range []*PluginWorkbenchFileAccess{{Protocol: 2, Extensions: []string{".pr"}}, {Protocol: 1, Extensions: []string{"*"}}, {Protocol: 1, Extensions: []string{".pr", ".pr"}}} {
		if validateWorkbenchFileAccess(f) == nil {
			t.Fatal("invalid declaration accepted")
		}
	}
	a.mu.Lock()
	a.pluginRegistry.Plugins[0].Workbench.FileAccess = nil
	a.mu.Unlock()
	requireStatus(t, fileAccessCall(a, key, "undeclared", "files.select", map[string]any{"action": "sources"}), 403)
}
func TestWorkbenchFileAccessPendingAndRevokedSource(t *testing.T) {
	a, key := fileAccessFixture(t)
	fileAccessResult(t, a, key, "bind", "files.select", map[string]any{"action": "bind", "source": "workspace", "path": "draft.pr", "mode": "save", "version": ""})
	fileAccessStage(t, a, key, "draft", "", []byte("draft"))
	requireStatus(t, fileAccessCall(a, key, "rebind", "files.select", map[string]any{"action": "bind", "source": "workspace", "path": "other.pr", "mode": "save", "version": ""}), 403)
	fileAccessResult(t, a, key, "abort", "document.save.abort", nil)
	a.mu.Lock()
	a.pluginWorkbenchBridges[key].Ticket.Gen--
	a.mu.Unlock()
	requireStatus(t, fileAccessCall(a, key, "revoked", "files.select", map[string]any{"action": "sources"}), 403)
}

func TestWorkbenchFileAccessTargetInspectionAndPinnedVersion(t *testing.T) {
	a, key := fileAccessFixture(t)
	os.WriteFile(filepath.Join(a.workPath, "original.pr"), []byte("original"), 0600)
	os.WriteFile(filepath.Join(a.workPath, "other.pr"), []byte("other"), 0600)
	fileAccessResult(t, a, key, "open", "files.select", map[string]any{"action": "bind", "source": "workspace", "path": "original.pr", "mode": "open"})
	target := fileAccessResult(t, a, key, "target", "files.select", map[string]any{"action": "target", "source": "workspace", "path": "other.pr", "mode": "save"})
	info := fileAccessResult(t, a, key, "still_original", "document.info", nil)
	if info["path"] != "original.pr" {
		t.Fatal("target inspection changed binding", info)
	}
	os.WriteFile(filepath.Join(a.workPath, "other.pr"), []byte("changed"), 0600)
	requireStatus(t, fileAccessCall(a, key, "stale", "files.select", map[string]any{"action": "bind", "source": "workspace", "path": "other.pr", "mode": "save", "version": target["version"]}), 409)
	requireStatus(t, fileAccessCall(a, key, "assets", "document.read", map[string]any{"asset": "other.pr"}), 403)
	fileAccessResult(t, a, key, "clear", "files.select", map[string]any{"action": "clear"})
	requireStatus(t, fileAccessCall(a, key, "unbound", "document.info", nil), 403)
}

func TestWorkbenchFileAccessSFTPCreateAndReplace(t *testing.T) {
	for _, reference := range []bool{false, true} {
		t.Run(fmt.Sprint(reference), func(t *testing.T) {
			remote := t.TempDir()
			bin, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			stub := filepath.Join(t.TempDir(), "owned-ssh")
			script := "#!/bin/sh\nfor arg do if [ \"$arg\" = sftp ]; then exec env AIDE_TRANSFER_SFTP_HELPER=1 " + shellQuote(bin) + " -test.run='^TestTransferSFTPProcess$'; fi; done\nexit 0\n"
			if err = os.WriteFile(stub, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			source := "workspace"
			if reference {
				source = "source:remote-ref"
			}
			a, key := fileAccessConfiguredFixture(t, func(a *App) {
				a.sshBin = stub
				a.wsConfig.Workspace.Mode = "ssh"
				a.wsConfig.Workspace.Host = "owned.invalid"
				a.wsConfig.Workspace.Path = remote
				if reference {
					a.sourceRegistry.Sources = append(a.sourceRegistry.Sources, Source{ID: "remote-ref", Name: "Owned SFTP fixture", Type: "workspace-sftp", Enabled: true, RW: true, Config: SourceConfig{Path: remote}})
				}
			})
			t.Cleanup(func() { a.sshBin = "" })
			fileAccessResult(t, a, key, "new", "files.select", map[string]any{"action": "bind", "source": source, "path": "remote.pr", "mode": "save", "version": ""})
			data := bytes.Repeat([]byte{1, 0, 255}, 20000)
			fileAccessStage(t, a, key, "newfile", "", data)
			fileAccessResult(t, a, key, "commit_new", "document.save.commit", nil)
			got, _ := os.ReadFile(filepath.Join(remote, "remote.pr"))
			if !bytes.Equal(got, data) {
				t.Fatal("new remote bytes differ")
			}
			info := fileAccessResult(t, a, key, "info", "document.info", nil)
			next := []byte("updated remote encrypted bytes")
			fileAccessStage(t, a, key, "replace", info["version"].(string), next)
			fileAccessResult(t, a, key, "commit_replace", "document.save.commit", nil)
			got, _ = os.ReadFile(filepath.Join(remote, "remote.pr"))
			if !bytes.Equal(got, next) {
				t.Fatal("updated remote bytes differ")
			}
		})
	}
}

func TestWorkbenchFileAccessUnconfiguredReferenceCannotFallback(t *testing.T) {
	a, key := fileAccessFixture(t)
	a.mu.Lock()
	a.sourceRegistry.Sources = append(a.sourceRegistry.Sources, Source{ID: "unconfigured", Name: "Empty path", Type: "local", Enabled: true, RW: true})
	a.mu.Unlock()
	sources := fileAccessResult(t, a, key, "sources", "files.select", map[string]any{"action": "sources"})
	for _, v := range sources["sources"].([]any) {
		if v.(map[string]any)["id"] == "source:unconfigured" {
			t.Fatal("unconfigured reference listed")
		}
	}
	requireStatus(t, fileAccessCall(a, key, "reject", "files.select", map[string]any{"action": "bind", "source": "source:unconfigured", "path": "fallback.pr", "mode": "save", "version": ""}), 400)
}
