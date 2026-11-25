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
	if body["measurement"] != "logical-file-bytes" || body["version"] != float64(2) || body["data"] == nil || body["workspace"] == nil {
		t.Fatalf("storage response = %+v", body)
	}
	disks, ok := body["disks"].([]any)
	if !ok || len(disks) != 2 {
		t.Fatalf("storage disks = %+v", body["disks"])
	}
	audit, ok := body["workspaceAudit"].(map[string]any)
	if !ok || audit["included"] != true {
		t.Fatalf("workspace audit = %+v", body["workspaceAudit"])
	}
	if strings.Contains(response.Body.String(), a.dataPath) || strings.Contains(response.Body.String(), a.workPath) {
		t.Fatalf("storage response exposed a local path: %s", response.Body.String())
	}
}

func TestStorageUsageHandlerExcludesSSHWorkspaceAudit(t *testing.T) {
	a := testApp(t)
	writeSizedFile(t, filepath.Join(a.dataPath, "sessions", "active", "usage-fixture.json"), 33)
	writeSizedFile(t, filepath.Join(a.workPath, "local-file-that-is-not-the-ssh-workspace.bin"), 4096)
	a.wsConfig.Workspace.Mode = "ssh"
	response := request(a, "GET", "/api/storage-usage", nil)
	requireStatus(t, response, 200)
	var body struct {
		Workspace      StorageUsage      `json:"workspace"`
		WorkspaceAudit map[string]any    `json:"workspaceAudit"`
		Disks          []StorageDiskUsage `json:"disks"`
		Capacity       map[string]int64  `json:"capacity"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Workspace.LogicalBytes != 0 || body.Workspace.Files != 0 {
		t.Fatalf("SSH workspace was scanned: %+v", body.Workspace)
	}
	if body.WorkspaceAudit["included"] != false || body.WorkspaceAudit["reason"] != "ssh-source" {
		t.Fatalf("workspace audit = %+v", body.WorkspaceAudit)
	}
	if len(body.Disks) != 1 || body.Disks[0].ID != "aide-data" {
		t.Fatalf("disks = %+v", body.Disks)
	}
	if body.Capacity["workspaceAvailableBytes"] != -1 {
		t.Fatalf("SSH workspace capacity = %+v", body.Capacity)
	}
	if strings.Contains(response.Body.String(), "local-file-that-is-not-the-ssh-workspace") {
		t.Fatalf("response exposed an excluded local file: %s", response.Body.String())
	}
}
