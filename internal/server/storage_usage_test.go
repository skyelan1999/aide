package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSizedFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestScanStorageUsageGroupsDataAndSkipsSymlinks(t *testing.T) {
	root := t.TempDir()
	writeSizedFile(t, filepath.Join(root, "sessions", "active", "one.json"), 11)
	writeSizedFile(t, filepath.Join(root, "stats", "network.json"), 22)
	writeSizedFile(t, filepath.Join(root, "root.json"), 5)
	outside := filepath.Join(t.TempDir(), "outside.bin")
	writeSizedFile(t, outside, 999)
	if err := os.Symlink(outside, filepath.Join(root, "sessions", "link.bin")); err != nil {
		t.Fatal(err)
	}

	writeSizedFile(t, filepath.Join(root, ".aide-data", "storage", "duplicate.bin"), 1000)
	got, err := scanStorageUsage(context.Background(), root, true, ".aide-data")
	if err != nil {
		t.Fatal(err)
	}
	if got.LogicalBytes != 38 || got.Files != 3 {
		t.Fatalf("total = %+v", got)
	}
	if len(got.Children) != 3 {
		t.Fatalf("children = %+v", got.Children)
	}
	byID := map[string]StorageUsage{}
	for _, child := range got.Children {
		byID[child.ID] = child
	}
	if byID["stats"].LogicalBytes != 22 || byID["sessions"].LogicalBytes != 11 || byID["_root"].LogicalBytes != 5 {
		t.Fatalf("groups = %+v", byID)
	}
}

func TestStorageUsageHandlerReturnsRealTotalsWithoutPaths(t *testing.T) {
	a := testApp(t)
	writeSizedFile(t, filepath.Join(a.dataPath, "sessions", "active", "usage-fixture.json"), 33)
	writeSizedFile(t, filepath.Join(a.workPath, "workspace-usage-fixture.bin"), 44)
	response := request(a, "GET", "/api/storage-usage", nil)
	requireStatus(t, response, 200)
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["measurement"] != "logical-file-bytes" || body["data"] == nil || body["workspace"] == nil {
		t.Fatalf("storage response = %+v", body)
	}
	if strings.Contains(response.Body.String(), a.dataPath) || strings.Contains(response.Body.String(), a.workPath) {
		t.Fatalf("storage response exposed a local path: %s", response.Body.String())
	}
}
