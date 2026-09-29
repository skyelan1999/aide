package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectCacheIsolation(t *testing.T) {
	a := testApp(t)
	tmp := t.TempDir()
	ssh, sftp, log := filepath.Join(tmp, "ssh"), filepath.Join(tmp, "sftp"), filepath.Join(tmp, "batch.log")
	writeStub(t, ssh, "#!/bin/sh\nexit 0\n")
	writeStub(t, sftp, "#!/bin/sh\ncat >> "+shellQuote(log)+"\nexit 0\n")
	a.sshBin, a.sftpBin = ssh, sftp
	requireStatus(t, request(a, "PUT", "/api/workspace-config", map[string]any{
		"workspace": map[string]any{"mode": "ssh", "path": "/srv/project", "host": "example", "username": "dev", "port": 22, "auth": "none"},
		"cache":     map[string]any{"location": "workspace", "path": "cache"},
	}), 200)
	if strings.HasPrefix(a.cacheContainer, a.workPath+string(os.PathSeparator)) {
		t.Fatalf("project mirror leaked to product: %s", a.cacheContainer)
	}
	if a.workspaceRemoteCachePath() != "/srv/project/cache" {
		t.Fatal(a.workspaceRemoteCachePath())
	}
	if !strings.HasPrefix(a.sourcesPath(), ConfigDir(a.dataPath)) {
		t.Fatal(a.sourcesPath())
	}
	out := a.createDesign("isolation", "## 开发流程\ncheck", "", "")
	if !strings.Contains(out, "/srv/project/cache/system-docs/designs/") {
		t.Fatalf("document path: %s", out)
	}
	if out := a.writeMemory("project-only"); !strings.Contains(out, "已写入") {
		t.Fatal(out)
	}
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/srv/project/cache/system-docs/designs/", "/srv/project/cache/aide/memory.md"} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("missing remote upload %s: %s", want, b)
		}
	}
}

func TestLocalCacheCommandAndMemoryAgree(t *testing.T) {
	a := testApp(t)
	target := filepath.Join(a.workPath, "project-cache")
	a.wsConfig.Cache.Path = "/workspace/project-cache"
	if err := a.applyWorkspaceConfig(); err != nil {
		t.Fatal(err)
	}
	if a.memoryPath() != filepath.Join(target, "aide", "memory.md") {
		t.Fatal(a.memoryPath())
	}
	a.settings.SandboxMode = "danger-full-access"
	out, _, err := a.execShellCommand(context.Background(), nil, "printf '%s' \"$AIDE_CACHE\"")
	if err != nil || out != target {
		t.Fatalf("cache environment %q %v", out, err)
	}
}
