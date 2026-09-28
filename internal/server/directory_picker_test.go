package server

import (
	"encoding/json"
	"os"
	"testing"
)

func TestDirectoryPickerCreatesAndRenamesWorkspaceFolder(t *testing.T) {
	a := testApp(t)
	requireStatus(t, request(a, "POST", "/api/directory", map[string]any{
		"root": "workspace", "parentPath": ".", "name": "picker-folder",
	}), 200)
	info, err := a.workspace.Stat("picker-folder")
	if err != nil || !info.IsDir() {
		t.Fatalf("created directory missing or not a directory: %v", err)
	}
	requireStatus(t, request(a, "POST", "/api/directory/rename", map[string]any{
		"root": "workspace", "path": "picker-folder", "newName": "renamed-folder",
	}), 200)
	if _, err := a.workspace.Stat("picker-folder"); !os.IsNotExist(err) {
		t.Fatalf("old directory should be gone, got %v", err)
	}
	info, err = a.workspace.Stat("renamed-folder")
	if err != nil || !info.IsDir() {
		t.Fatalf("renamed directory missing or not a directory: %v", err)
	}
}

func TestFileManagerPropertiesAndDelete(t *testing.T) {
	a := testApp(t)
	if err := putText(a.workspace, "notes.txt", []byte("hello")); err != nil {
		t.Fatal(err)
	}
	w := request(a, "GET", "/api/file/properties?root=workspace&path=notes.txt", nil)
	requireStatus(t, w, 200)
	var props map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &props); err != nil || props["type"] != "文件" || props["size"].(float64) != 5 {
		t.Fatalf("unexpected file properties: %s (%v)", w.Body.String(), err)
	}
	requireStatus(t, request(a, "POST", "/api/file/delete", map[string]any{"root": "workspace", "path": "notes.txt"}), 200)
	if _, err := a.workspace.Stat("notes.txt"); !os.IsNotExist(err) {
		t.Fatalf("file should be deleted, got %v", err)
	}
	if err := a.workspace.Mkdir("empty", 0755); err != nil { t.Fatal(err) }
	requireStatus(t, request(a, "POST", "/api/file/delete", map[string]any{"root": "workspace", "path": "empty"}), 200)
	requireStatus(t, request(a, "POST", "/api/file/delete", map[string]any{"root": "workspace", "path": "."}), 400)
}

func TestDirectoryPickerRejectsUnsafeNamesAndRemoteControlCharacters(t *testing.T) {
	a := testApp(t)
	for _, name := range []string{"", ".", "..", "a/b", `a\b`, "a\n"} {
		w := request(a, "POST", "/api/directory", map[string]any{
			"root": "workspace", "parentPath": ".", "name": name,
		})
		if w.Code != 400 {
			t.Fatalf("unsafe name %q status %d want 400: %s", name, w.Code, w.Body.String())
		}
	}
	for _, p := range []string{"", "bad\npath", "bad\x00path", `bad\path`} {
		if err := validateRemoteBrowsePath(p); err == nil {
			t.Fatalf("remote browse path %q should be rejected", p)
		}
	}
	if err := validateRemoteBrowsePath("/srv/projects/../workspace"); err != nil {
		t.Fatalf("absolute remote browse path should be accepted: %v", err)
	}
}
