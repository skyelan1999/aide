package server

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspaceStartupMissingLocalPathKeepsConfiguration(t *testing.T) {
	root := t.TempDir()
	work := filepath.Join(root, "work")
	ref := filepath.Join(root, "ref")
	data := filepath.Join(root, "data")
	if err := os.MkdirAll(work, 0700); err != nil {
		t.Fatal(err)
	}
	saved := defaultWorkspaceConfig()
	saved.Workspace.Path = "missing-on-this-device"
	saved.Recent.Workspace = []string{"preserve-recent"}
	saved.Cache.Path = "missing-cache-on-this-device"
	path := filepath.Join(work, wsConfigFile)
	if err := atomicJSON(path, saved); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(work, ref, data)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if a.workspace.Name() != work || a.wsConfig.Workspace.Path != "" || a.wsStartupWarning == "" {
		t.Fatalf("missing runtime fallback: %q %q", a.workspace.Name(), a.wsStartupWarning)
	}
	out := a.workspaceConfigOut()
	if out["workspace"].(map[string]any)["path"] != saved.Workspace.Path {
		t.Fatal("repair form lost saved path")
	}
	if _, err := os.Stat(filepath.Join(work, saved.Cache.Path)); !os.IsNotExist(err) {
		t.Fatal("startup created stale cache target", err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("startup overwrote original configuration")
	}
	// Invalid user edits still reject; this is only a startup recovery path.
	requireStatus(t, request(a, "PUT", "/api/workspace-config", map[string]any{"workspace": map[string]any{"mode": "local", "path": "still-missing"}}), 400)
	if a.wsStartupWarning == "" {
		t.Fatal("rejected edit cleared recovery warning")
	}
	requireStatus(t, request(a, "PUT", "/api/workspace-config", map[string]any{"workspace": map[string]any{"mode": "local", "path": ""}}), 200)
	if a.wsStartupWarning != "" || a.wsStartupConfig != nil {
		t.Fatal("successful repair did not clear recovery")
	}
	var repaired WorkspaceConfig
	b, _ := os.ReadFile(path)
	if err := json.Unmarshal(b, &repaired); err != nil || repaired.Workspace.Path != "" {
		t.Fatal("repair not saved", err)
	}
}

func TestWorkspaceStartupMalformedConfigStillFails(t *testing.T) {
	root := t.TempDir()
	work := filepath.Join(root, "work")
	if err := os.MkdirAll(work, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, wsConfigFile), []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	a, err := New(work, filepath.Join(root, "ref"), filepath.Join(root, "data"))
	if a != nil {
		a.Close()
	}
	if err == nil {
		t.Fatal("malformed config must not be silently reset")
	}
}
