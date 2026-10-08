package server

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSFTPNotExistClassification(t *testing.T) {
	for _, tt := range []struct {
		name, message string
		want          bool
	}{
		{"stat missing", "SFTP 失败: Couldn't stat remote file: No such file", true},
		{"canonical missing", "SFTP 失败: Couldn't canonicalize: No such file or directory", true},
		{"quoted missing", "SFTP 失败: sftp> get \"/new folder/new file.md\" \"/tmp/file\"\nFile \"/new folder/new file.md\" not found.", true},
		{"stat denied", "SFTP 失败: Couldn't stat remote file: Permission denied", false},
		{"readdir denied", "SFTP 失败: remote readdir(\"/secret\"): Permission denied", false},
		{"transport", "SFTP 失败: Connection closed", false},
		{"unclassified stat", "SFTP 失败: Couldn't stat remote file", false},
		{"filename only", "SFTP 失败: sftp> get \"/File \\\"x\\\" not found.\" \"/tmp/file\"\nConnection closed", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSFTPNotExistErr(errors.New(tt.message)); got != tt.want {
				t.Fatalf("got %v want %v for %q", got, tt.want, tt.message)
			}
		})
	}
	if isSFTPNotExistErr(nil) {
		t.Fatal("nil error must not classify as missing")
	}
}

func TestRemoteConfiguredDirectoryPaths(t *testing.T) {
	a := testApp(t)
	a.wsConfig.Workspace.Mode = "ssh"
	a.wsConfig.Workspace.Path = "/srv/project"
	a.wsConfig.Cache.Location = "workspace"
	for _, tt := range []struct{ config, want string }{
		{"cache", "/srv/project/cache"},
		{"/srv/shared cache", "/srv/shared cache"},
		{"/srv/shared/../cache", "/srv/cache"},
	} {
		a.wsConfig.Cache.Path = tt.config
		if got := a.workspaceRemoteCachePath(); got != tt.want {
			t.Errorf("cache %q: got %q want %q", tt.config, got, tt.want)
		}
		if got := a.workspaceRemoteSourcePath(tt.config, "子目录/read me.md"); got != tt.want+"/子目录/read me.md" {
			t.Errorf("docs %q: got %q", tt.config, got)
		}
		loc := transferLocation{hasSource: true, source: Source{Type: "workspace-sftp", Config: SourceConfig{Path: tt.config}}}
		if got, remote := a.transferRemote(loc, "子目录/read me.md"); !remote || got != tt.want+"/子目录/read me.md" {
			t.Errorf("docs transfer %q: got %q remote=%v", tt.config, got, remote)
		}
	}
	a.wsConfig.Cache.Path = ""
	if got := a.workspaceRemoteCachePath(); got != "/srv/project/.cache" {
		t.Fatal("default remote cache:", got)
	}
	a.wsConfig.Workspace.Path = ""
	if got := a.workspaceRemotePath("."); got != "." {
		t.Fatal("blank workspace must use remote home:", got)
	}
	if got := a.workspaceRemoteSourcePath("", "notes.md"); got != "./notes.md" {
		t.Fatal("blank docs must use remote workspace:", got)
	}
	// Absolute paths are supported only for explicitly configured directories;
	// the selected root's file requests must still reject escapes before SFTP.
	for _, file := range []string{"/etc/passwd", "../secret.md", "nested/../../secret.md"} {
		if err := safePath(file); err == nil {
			t.Errorf("file boundary accepted %q", file)
		}
	}
}

func TestSFTPSourceCredentialChangeClosesMaster(t *testing.T) {
	a := testApp(t)
	dir := t.TempDir()
	log := filepath.Join(dir, "ssh.log")
	a.sshBin = filepath.Join(dir, "ssh")
	writeStub(t, a.sshBin, "#!/bin/sh\nprintf '%s\\n' \"$*\" >> "+shellQuote(log)+"\nexit 0\n")
	const id = "rotating-source"
	src := sourceBody(id, "rotate", "sftp", map[string]any{"host": "example.invalid", "port": 22, "username": "reader", "auth": "password", "path": "/docs"}, true)
	requireStatus(t, request(a, "PUT", "/api/sources", map[string]any{"sources": []any{src}, "secrets": map[string]any{id: map[string]any{"password": "fixture-only-original"}}}), 200)
	for _, secret := range []map[string]any{{"password": "fixture-only-new"}, {"key": "fixture-only-key"}, {"clear": true}} {
		if err := os.WriteFile(log, nil, 0600); err != nil {
			t.Fatal(err)
		}
		requireStatus(t, request(a, "PUT", "/api/sources", map[string]any{"sources": []any{src}, "secrets": map[string]any{id: secret}}), 200)
		b, err := os.ReadFile(log)
		if err != nil || !strings.Contains(string(b), "-O exit") || !strings.Contains(string(b), sourceSocket(id)) {
			t.Fatalf("credential change did not retire its existing master: %s %v", b, err)
		}
	}
}
