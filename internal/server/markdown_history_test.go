package server

import (
	"encoding/json"
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
	if got := list(); got["total"] != float64(1) {
		t.Fatalf("unchanged duplicated: %v", got)
	}
	put("image.svg", "<svg>external</svg>")
	if got := list(); got["total"] != float64(2) {
		t.Fatalf("resource change missing: %v", got)
	}
	w := request(a, "GET", "/api/file/history?path=page.md&revision=000001&asset=image.svg", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), "PHN2Zz5maXJzdDwvc3ZnPg==") {
		t.Fatal(w.Body.String())
	}
	if err := os.Remove(filepath.Join(a.workPath, "page.md")); err != nil {
		t.Fatal(err)
	}
	if got := list(); got["observation"] != "unavailable" || got["total"] != float64(2) {
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
