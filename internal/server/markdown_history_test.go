package server

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func enableMarkdownHistory(t *testing.T, a *App) {
	t.Helper()
	reg := pluginRegistry{Version: 1, Plugins: []PluginManifest{{ID: "markdown-history", Enabled: true}}}
	if err := os.MkdirAll(a.pluginsPath, 0700); err != nil {
		t.Fatal(err)
	}
	if err := atomicJSON(filepath.Join(a.pluginsPath, "registry.json"), reg); err != nil {
		t.Fatal(err)
	}
}

func TestMarkdownHistoryObservedAssetsAndDedup(t *testing.T) {
	a := testApp(t)
	enableMarkdownHistory(t, a)
	put := func(p, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(a.workPath, p), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	put("page.md", "# Page\n![image](image.svg)")
	put("image.svg", "<svg>first</svg>")
	identity, read, err := a.historyTarget("", "page.md")
	if err != nil {
		t.Fatal(err)
	}
	captureBeforeListing := func() {
		t.Helper()
		a.historyMu.Lock()
		defer a.historyMu.Unlock()
		if err := a.captureMarkdown(a.markdownHistoryDir(identity), "page.md", []byte("# Page\n![image](image.svg)"), true, read); err != nil {
			t.Fatal(err)
		}
	}
	list := func() map[string]any {
		t.Helper()
		w := request(a, "GET", "/api/file/history?path=page.md", nil)
		requireStatus(t, w, 200)
		var data map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
			t.Fatal(err)
		}
		return data
	}
	if got := list(); got["total"] != float64(1) || got["observation"] != "checked" {
		t.Fatalf("first observation: %v", got)
	}
	if got := list(); got["total"] != float64(1) || got["observationChange"] != nil {
		t.Fatalf("unchanged duplicated: %v", got)
	}
	put("image.svg", "<svg>external</svg>")
	// Another sync path can capture a change before the user opens history.
	// The observation watermark must keep that change visible until listing.
	captureBeforeListing()
	if got := list(); got["total"] != float64(2) {
		t.Fatalf("resource change missing: %v", got)
	} else {
		change, ok := got["observationChange"].(map[string]any)
		assets, assetsOK := change["assetsChanged"].([]any)
		if !ok || change["version"] != "000002" || change["bodyChanged"] != false || !assetsOK || len(assets) != 1 || assets[0] != "image.svg" {
			t.Fatalf("resource change summary: %#v", got["observationChange"])
		}
	}
	if got := list(); got["observationChange"] != nil {
		t.Fatalf("already observed resource change repeated: %v", got)
	}
	put("page.md", "# Page revised\n![image](image.svg)")
	captureBeforeListing()
	if got := list(); got["total"] != float64(3) {
		t.Fatalf("body change missing: %v", got)
	} else if change, ok := got["observationChange"].(map[string]any); !ok || change["version"] != "000003" || change["bodyChanged"] != true || len(change["assetsChanged"].([]any)) != 0 {
		t.Fatalf("body change summary: %#v", got["observationChange"])
	}
	w := request(a, "GET", "/api/file/history?path=page.md&revision=000001&asset=image.svg", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), "PHN2Zz5maXJzdDwvc3ZnPg==") {
		t.Fatal(w.Body.String())
	}
	if err := os.Remove(filepath.Join(a.workPath, "page.md")); err != nil {
		t.Fatal(err)
	}
	if got := list(); got["observation"] != "unavailable" || got["total"] != float64(3) {
		t.Fatalf("unavailable must preserve history: %v", got)
	}
	requireStatus(t, request(a, "GET", "/api/file/history?path=page.md&revision=000001", nil), 200)
}

func TestMarkdownHistoryRejectsCorruptObjects(t *testing.T) {
	dir := t.TempDir()
	digest, err := storeHistoryObject(dir, []byte("original"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := readHistoryObject(dir, digest); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "objects", digest), []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readHistoryObject(dir, digest); err == nil {
		t.Fatal("corrupt object accepted")
	}
	if _, err := readHistoryObject(dir, "../secret"); err == nil {
		t.Fatal("invalid digest accepted")
	}
}

func TestMarkdownHistoryReadPinsWorkspace(t *testing.T) {
	a := testApp(t)
	if err := os.WriteFile(filepath.Join(a.workPath, "page.md"), []byte("first workspace"), 0600); err != nil {
		t.Fatal(err)
	}
	_, read, err := a.historyTarget("", "page.md")
	if err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "page.md"), []byte("other workspace"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(other)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	a.mu.Lock()
	original := a.workspace
	a.workspace = root
	a.mu.Unlock()
	defer func() { a.mu.Lock(); a.workspace = original; a.mu.Unlock() }()
	b, err := read("page.md")
	if err != nil || string(b) != "first workspace" {
		t.Fatalf("reader redirected: %q %v", b, err)
	}
}

func TestMarkdownRestoreBundleAndConflict(t *testing.T) {
	a := testApp(t)
	enableMarkdownHistory(t, a)
	put := func(p, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(a.workPath, p), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	put("page.md", "![one](image.svg)")
	put("image.svg", "old image")
	requireStatus(t, request(a, "GET", "/api/file/history?path=page.md", nil), 200)
	put("page.md", "new document")
	put("image.svg", "new image")
	put("unrelated.txt", "keep me")
	preview := func() markdownRestorePlan {
		t.Helper()
		w := request(a, "GET", "/api/file/history/restore?path=page.md&revision=000001&assets=1", nil)
		requireStatus(t, w, 200)
		var plan markdownRestorePlan
		if err := json.Unmarshal(w.Body.Bytes(), &plan); err != nil {
			t.Fatal(err)
		}
		return plan
	}
	body := func(plan markdownRestorePlan) map[string]any {
		expected := map[string]string{}
		for _, f := range plan.Files {
			expected[f.Path] = f.Before
		}
		return map[string]any{"path": "page.md", "workspaceId": a.wsID(), "identity": plan.Identity, "revision": "000001", "assets": true, "expected": expected}
	}
	plan := preview()
	put("image.svg", "external conflict")
	requireStatus(t, request(a, "POST", "/api/file/history/restore", body(plan)), 409)
	if b, _ := os.ReadFile(filepath.Join(a.workPath, "page.md")); string(b) != "new document" {
		t.Fatal("conflict wrote body")
	}
	plan = preview()
	w := request(a, "POST", "/api/file/history/restore", body(plan))
	requireStatus(t, w, 200)
	for p, want := range map[string]string{"page.md": "![one](image.svg)", "image.svg": "old image", "unrelated.txt": "keep me"} {
		b, err := os.ReadFile(filepath.Join(a.workPath, p))
		if err != nil || string(b) != want {
			t.Fatalf("%s: %q %v", p, b, err)
		}
	}
	var result struct {
		Journal string `json:"journal"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	dir := a.markdownHistoryDir(plan.Identity)
	raw, err := os.ReadFile(filepath.Join(dir, "restores", result.Journal+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var journal markdownRestoreJournal
	if err := json.Unmarshal(raw, &journal); err != nil {
		t.Fatal(err)
	}
	if journal.State != "completed" || journal.Pending != "" {
		t.Fatalf("journal: %+v", journal)
	}
	for _, file := range journal.Plan.Files {
		if !file.Applied || !file.Verified {
			t.Fatalf("not applied: %+v", file)
		}
		if _, err := readHistoryObject(dir, file.Before); err != nil {
			t.Fatalf("backup missing: %v", err)
		}
	}
}

func TestMarkdownRestoreUnavailableAssetAndReadOnlySource(t *testing.T) {
	a := testApp(t)
	enableMarkdownHistory(t, a)
	if err := os.WriteFile(filepath.Join(a.workPath, "page.md"), []byte("![missing](absent.png)"), 0600); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, "GET", "/api/file/history?path=page.md", nil), 200)
	requireStatus(t, request(a, "GET", "/api/file/history/restore?path=page.md&revision=000001&assets=1", nil), 400)
	requireStatus(t, request(a, "GET", "/api/file/history/restore?path=page.md&revision=000001&assets=0", nil), 200)
	a.mu.Lock()
	a.sourceRegistry.Sources = append(a.sourceRegistry.Sources, Source{ID: "readonly-history", Type: "local", Enabled: true, RW: false})
	a.mu.Unlock()
	if _, _, _, err := a.historyRestoreWriter("readonly-history"); err == nil {
		t.Fatal("read-only source accepted")
	}
}

func TestMarkdownHistoryPartialWriteContract(t *testing.T) {
	for _, phase := range []string{"before", "writer", "after"} {
		t.Run(phase, func(t *testing.T) {
			a := testApp(t)
			enableMarkdownHistory(t, a)
			target := filepath.Join(a.workPath, "partial.md")
			if err := os.WriteFile(target, []byte("old"), 0600); err != nil {
				t.Fatal(err)
			}
			identity := "workspace:" + a.wsID()
			dir := a.markdownHistoryDir(identity)
			if phase == "before" {
				if err := os.WriteFile(filepath.Dir(dir), []byte("blocked"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			called := false
			err := a.withMarkdownHistory(identity, "partial.md", []byte("new"), func(p string) ([]byte, error) { return os.ReadFile(filepath.Join(a.workPath, p)) }, func() error {
				called = true
				if phase == "writer" {
					return errors.New("destination write refused")
				}
				if err := os.WriteFile(target, []byte("new"), 0600); err != nil {
					return err
				}
				if err := os.Rename(dir, dir+".prior"); err != nil {
					return err
				}
				return os.WriteFile(dir, []byte("archive blocked"), 0600)
			})
			if err == nil {
				t.Fatal("archive/write failure accepted")
			}
			raw, readErr := os.ReadFile(target)
			if readErr != nil {
				t.Fatal(readErr)
			}
			var partial *markdownPostWriteError
			saved := phase == "after"
			if errors.As(err, &partial) != saved || string(raw) == "new" != saved || called != (phase != "before") {
				t.Fatalf("phase=%s err=%v body=%q called=%v", phase, err, raw, called)
			}
			response := httptest.NewRecorder()
			fileWriteFailure(response, err, "new")
			var body map[string]any
			if json.Unmarshal(response.Body.Bytes(), &body) != nil {
				t.Fatal(response.Body.String())
			}
			if response.Code != 400 {
				t.Fatal(response.Code)
			}
			if saved {
				if body["saved"] != true || body["historySaved"] != false || body["hash"] != hash([]byte("new")) {
					t.Fatal(body)
				}
			} else if _, ok := body["saved"]; ok {
				t.Fatal("pre-write failure reported saved", body)
			}
		})
	}
}

func TestHistoryObjectReuseVerifiesBytesAndKind(t *testing.T) {
	for _, kind := range []string{"same-size-corrupt", "symlink", "directory", "oversized", "objects-directory-link"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			original := []byte("immutable original")
			digest, err := storeHistoryObject(dir, original)
			if err != nil {
				t.Fatal(err)
			}
			object := filepath.Join(dir, "objects", digest)
			switch kind {
			case "same-size-corrupt":
				damaged := append([]byte(nil), original...)
				damaged[0] = 'X'
				if err := os.WriteFile(object, damaged, 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				target := filepath.Join(t.TempDir(), "outside")
				if err := os.WriteFile(target, original, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(object); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, object); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Remove(object); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(object, 0700); err != nil {
					t.Fatal(err)
				}
			case "oversized":
				if err := os.Truncate(object, maxRawFile+1); err != nil {
					t.Fatal(err)
				}
			case "objects-directory-link":
				moved := filepath.Join(t.TempDir(), "archive-objects")
				if err := os.Rename(filepath.Join(dir, "objects"), moved); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(moved, filepath.Join(dir, "objects")); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.Lstat(object)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := storeHistoryObject(dir, original); err == nil {
				t.Fatal("invalid archived object silently reused")
			}
			if _, err := readHistoryObject(dir, digest); err == nil {
				t.Fatal("invalid archived object served")
			}
			after, err := os.Lstat(object)
			if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || before.Size() != after.Size() {
				t.Fatalf("invalid object changed instead of being preserved: %v", err)
			}
		})
	}
}

func TestHistoryObjectValidReuseIsImmutable(t *testing.T) {
	for _, original := range [][]byte{nil, []byte("valid content")} {
		dir := t.TempDir()
		digest, err := storeHistoryObject(dir, original)
		if err != nil {
			t.Fatal(err)
		}
		object := filepath.Join(dir, "objects", digest)
		before, err := os.Stat(object)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(object, 0400); err != nil {
			t.Fatal(err)
		}
		again, err := storeHistoryObject(dir, original)
		if err != nil || again != digest {
			t.Fatalf("valid reuse: %s %v", again, err)
		}
		after, err := os.Stat(object)
		if err != nil || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) || after.Mode().Perm() != 0400 {
			t.Fatalf("immutable valid object was rewritten: %v", err)
		}
		got, err := readHistoryObject(dir, digest)
		if err != nil || string(got) != string(original) {
			t.Fatalf("valid readback: %q %v", got, err)
		}
	}
}

func TestMarkdownHistoryInvalidBackupBlocksWrite(t *testing.T) {
	for _, damaged := range []string{"body", "asset"} {
		t.Run(damaged, func(t *testing.T) {
			a := testApp(t)
			enableMarkdownHistory(t, a)
			old := []byte("# Original\n![image](asset.svg)\n")
			asset := []byte("<svg>original</svg>")
			for name, data := range map[string][]byte{"page.md": old, "asset.svg": asset} {
				if err := os.WriteFile(filepath.Join(a.workPath, name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			identity, read, err := a.historyTarget("", "page.md")
			if err != nil {
				t.Fatal(err)
			}
			dir := a.markdownHistoryDir(identity)
			if err := a.captureMarkdown(dir, "page.md", old, true, read); err != nil {
				t.Fatal(err)
			}
			expected := old
			if damaged == "asset" {
				expected = asset
			}
			object := filepath.Join(dir, "objects", hash(expected))
			bad := append([]byte(nil), expected...)
			bad[0] = 'X'
			if err := os.WriteFile(object, bad, 0600); err != nil {
				t.Fatal(err)
			}
			indexBefore, err := os.ReadFile(historyIndexPath(dir, "page.md"))
			if err != nil {
				t.Fatal(err)
			}
			body := map[string]any{"path": "page.md", "content": "# Changed\n![image](asset.svg)\n", "hash": hash(old), "workspaceId": a.wsID()}
			response := request(a, "PUT", "/api/file", body)
			requireStatus(t, response, 400)
			var reply map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &reply); err != nil {
				t.Fatal(err)
			}
			if _, saved := reply["saved"]; saved {
				t.Fatal("failed pre-save backup claimed write succeeded", reply)
			}
			after, err := os.ReadFile(filepath.Join(a.workPath, "page.md"))
			if err != nil || string(after) != string(old) {
				t.Fatalf("current body overwritten: %q %v", after, err)
			}
			after, err = os.ReadFile(object)
			if err != nil || string(after) != string(bad) {
				t.Fatal("damaged history was changed", err)
			}
			after, err = os.ReadFile(historyIndexPath(dir, "page.md"))
			if err != nil || string(after) != string(indexBefore) {
				t.Fatal("index mutated after rejected backup", err)
			}
			if err := os.WriteFile(object, expected, 0600); err != nil {
				t.Fatal(err)
			}
			requireStatus(t, request(a, "PUT", "/api/file", body), 200)
			current, err := os.ReadFile(filepath.Join(a.workPath, "page.md"))
			if err != nil || string(current) != body["content"] {
				t.Fatal("retry did not save", err)
			}
			index, err := loadMarkdownIndex(dir, "page.md")
			if err != nil || len(index.Versions) != 2 {
				t.Fatalf("history after retry: %+v %v", index, err)
			}
			restored, err := readHistoryObject(dir, hash(old))
			if err != nil || string(restored) != string(old) {
				t.Fatal("old body lost", err)
			}
			restored, err = readHistoryObject(dir, hash(asset))
			if err != nil || string(restored) != string(asset) {
				t.Fatal("old asset lost", err)
			}
		})
	}
}
