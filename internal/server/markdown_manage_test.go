package server

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMarkdownRetentionKeepsLatestAndStableIDs(t *testing.T) {
	now := time.Now().UTC()
	index := markdownHistoryIndex{Path: "page.md", Versions: []markdownRevision{
		{ID: "000001", Created: now.AddDate(0, 0, -50).Format(time.RFC3339Nano)},
		{ID: "000010", Created: now.AddDate(0, 0, -20).Format(time.RFC3339Nano)},
		{ID: "000011", Created: now.AddDate(0, 0, -10).Format(time.RFC3339Nano)},
	}}
	kept := retainMarkdown(index, markdownRetention{KeepLatest: 2, KeepDays: 5}, now)
	if len(kept.Versions) != 1 || kept.Versions[0].ID != "000011" || nextMarkdownRevision(kept) != "000012" {
		t.Fatalf("retention/identity: %+v", kept)
	}
	if len(retainMarkdown(index, markdownRetention{}, now).Versions) != 3 {
		t.Fatal("default policy pruned history")
	}
}

func TestMarkdownManageImportRetentionAndGarbage(t *testing.T) {
	a := testApp(t)
	dir := a.markdownHistoryDir("workspace:" + a.wsID())
	for _, text := range []string{"# Before", "# After"} {
		if err := a.captureMarkdown(dir, "page.md", []byte(text), false, nil); err != nil {
			t.Fatal(err)
		}
	}
	backup, err := a.markdownBackupData("", "page.md", true)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(backup["base64"].(string))
	if err != nil {
		t.Fatal(err)
	}
	archive, err := readMarkdownArchive(raw, "page.md")
	if err != nil || len(archive.History.Versions) != 2 {
		t.Fatalf("archive %v", err)
	}
	if _, err := readMarkdownArchive(raw, "another.md"); err == nil {
		t.Fatal("wrong target accepted")
	}
	// Metadata pruning leaves objects until separately confirmed namespace GC.
	call := func(action string, extra map[string]any) map[string]any {
		t.Helper()
		body := map[string]any{"action": action, "path": "page.md", "workspaceId": a.wsID()}
		for k, v := range extra {
			body[k] = v
		}
		w := request(a, "POST", "/api/file/history/manage", body)
		requireStatus(t, w, 200)
		var result map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	plan := call("retention-preview", map[string]any{"policy": markdownRetention{KeepLatest: 1}})
	call("retention", map[string]any{"policy": markdownRetention{KeepLatest: 1}, "identity": plan["identity"], "revision": plan["revision"]})
	index, _ := loadMarkdownIndex(dir, "page.md")
	if len(index.Versions) != 1 {
		t.Fatal("pruning not applied")
	}
	gc := call("gc-preview", nil)
	if gc["gc"].(map[string]any)["objectCount"] != float64(1) {
		t.Fatalf("GC: %v", gc)
	}
	imp := call("import-preview", map[string]any{"archive": backup["base64"]})
	call("import", map[string]any{"archive": backup["base64"], "archiveDigest": imp["archiveDigest"], "identity": imp["identity"], "revision": imp["revision"]})
	index, _ = loadMarkdownIndex(dir, "page.md")
	if len(index.Versions) != 2 {
		t.Fatal("import failed")
	}
	// Another document and restore journals protect objects in the same namespace.
	protected, err := storeHistoryObject(dir, []byte("protected"))
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicJSON(historyIndexPath(dir, "other.md"), markdownHistoryIndex{Path: "other.md", Versions: []markdownRevision{{Content: protected}}}); err != nil {
		t.Fatal(err)
	}
	orphan, err := storeHistoryObject(dir, []byte("orphan"))
	if err != nil {
		t.Fatal(err)
	}
	gc = call("gc-preview", nil)
	gcplan := gc["gc"].(map[string]any)
	call("gc", map[string]any{"identity": gc["identity"], "revision": gc["revision"], "archiveDigest": gcplan["revision"]})
	if _, err := os.Stat(filepath.Join(dir, "objects", orphan)); !os.IsNotExist(err) {
		t.Fatal("orphan remains")
	}
	if _, err := readHistoryObject(dir, protected); err != nil {
		t.Fatal("other document object lost", err)
	}
	// Stale mutation must fail before index mutation.
	stale := request(a, "POST", "/api/file/history/manage", map[string]any{"action": "retention", "path": "page.md", "workspaceId": a.wsID(), "identity": plan["identity"], "revision": plan["revision"], "policy": markdownRetention{KeepLatest: 1}})
	requireStatus(t, stale, 409)
}
