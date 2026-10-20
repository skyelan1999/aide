package server

import (
	"context"
	"encoding/base64"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTerminalContextReferenceKeysAndBoundaries(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	key := []byte("-----BEGIN OPENSSH PRIVATE KEY-----\nfixture-not-a-real-key\n-----END OPENSSH PRIVATE KEY-----\n")
	if err := os.WriteFile(filepath.Join(a.workPath, "device.key"), key, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.reference.Name(), "reference.key"), key, 0600); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.sourceRegistry.Sources = append(a.sourceRegistry.Sources, Source{ID: "test-keys", Name: "Keys", Enabled: true, Type: "local"})
	a.mu.Unlock()
	for _, source := range []string{"", "test-keys"} {
		file := "device.key"
		if source != "" {
			file = "reference.key"
		}
		got, err := a.terminalToolContext(ctx, "terminal_session_create", map[string]any{"kind": "ssh", "auth": "key", "identitySource": source, "identityFile": file})
		if err != nil {
			t.Fatal(err)
		}
		decoded, _ := base64.StdEncoding.DecodeString(got["identityMaterial"].(string))
		if string(decoded) != string(key) {
			t.Fatal("wrong key")
		}
	}
	listing, err := a.terminalToolContext(ctx, "terminal_session_files", map[string]any{"identitySource": "test-keys", "path": "."})
	if err != nil {
		t.Fatal(err)
	}
	if listing["identityMaterial"] != nil {
		t.Fatal("listing leaked private key")
	}
	for _, args := range []map[string]any{{"identityFile": "../outside"}, {"identitySource": "missing", "identityFile": "device.key"}} {
		if _, err := a.terminalToolContext(ctx, "terminal_session_create", args); err == nil {
			t.Fatal("accepted invalid key locator")
		}
	}
	outside := filepath.Join(t.TempDir(), "secret.key")
	os.WriteFile(outside, key, 0600)
	os.Symlink(outside, filepath.Join(a.workPath, "escape.key"))
	if _, err := a.terminalToolContext(ctx, "terminal_session_create", map[string]any{"identityFile": "escape.key"}); err == nil {
		t.Fatal("accepted symlink key")
	}
	a.mu.Lock()
	for i := range a.sourceRegistry.Sources {
		if a.sourceRegistry.Sources[i].ID == "test-keys" {
			a.sourceRegistry.Sources[i].AIHiddenWorkspaces = []string{a.wsID()}
		}
	}
	a.mu.Unlock()
	if _, err := a.terminalToolContext(ctx, "terminal_session_create", map[string]any{"identitySource": "test-keys", "identityFile": "reference.key"}); err == nil {
		t.Fatal("read hidden source key")
	}
	if _, err := a.terminalToolContext(ctx, "terminal_session_files", map[string]any{"identitySource": "test-keys"}); err == nil {
		t.Fatal("listed hidden source")
	}
}

func TestTerminalContextSSHStorageIsolationWithoutLocalFallback(t *testing.T) {
	a := testApp(t)
	a.mu.Lock()
	a.wsConfig.Workspace.Mode = "ssh"
	a.wsConfig.Workspace.Host = "example.invalid"
	a.wsConfig.Workspace.Path = "/remote/project"
	a.wsConfig.Workspace.Username = "alice"
	a.mu.Unlock()
	first, err := a.terminalToolContext(context.Background(), "terminal_session_list", nil)
	if err != nil {
		t.Fatal(err)
	}
	root := first["storageRoot"].(string)
	if root == "" || root == a.workPath {
		t.Fatal("remote fell back to installation")
	}
	stat, err := os.Stat(root)
	if err != nil || stat.Mode().Perm() != 0700 {
		t.Fatal("private storage required", err)
	}
	if first["remote"].(map[string]any)["path"] != "/remote/project" {
		t.Fatal("lost remote cwd")
	}
	a.mu.Lock()
	a.wsConfig.Workspace.Username = "bob"
	a.mu.Unlock()
	second, err := a.terminalToolContext(context.Background(), "terminal_session_list", nil)
	if err != nil {
		t.Fatal(err)
	}
	if second["storageRoot"] == root {
		t.Fatal("different remote users share records")
	}
	if _, err := os.Stat(filepath.Join(a.workPath, ".aide", "terminal-workbench")); !os.IsNotExist(err) {
		t.Fatal("remote wrote to local work root")
	}
}

func TestTerminalContextPinnedMasterNeverRetargets(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "master.sock")
	old, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	pinned, err := pinTerminalSSHMaster(t.TempDir(), socket)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(filepath.Dir(pinned))
	defer os.Remove(pinned)
	if err := os.Remove(socket); err != nil {
		t.Fatal(err)
	}
	replacement, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer replacement.Close()
	go func() {
		connection, err := old.Accept()
		if err == nil {
			connection.Write([]byte("original"))
			connection.Close()
		}
	}()
	connection, err := net.DialTimeout("unix", pinned, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	connection.SetReadDeadline(time.Now().Add(time.Second))
	body := make([]byte, 8)
	if _, err = io.ReadFull(connection, body); err != nil || string(body) != "original" {
		t.Fatal("pinned socket retargeted", err)
	}
	old.Close()
	if other, err := net.DialTimeout("unix", pinned, time.Second); err == nil {
		other.Close()
		t.Fatal("dead original master reached a replacement")
	}
}
