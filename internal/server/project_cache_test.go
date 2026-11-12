package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestProjectCacheIsolation(t *testing.T) {
	a := testApp(t)
	tmp := t.TempDir()
	ssh, sftp, log := filepath.Join(tmp, "ssh"), filepath.Join(tmp, "sftp"), filepath.Join(tmp, "batch.log")
	writeStub(t, ssh, "#!/bin/sh\nexit 0\n")
	writeStub(t, sftp, "#!/bin/sh\ncat >> "+shellQuote(log)+"\nexit 0\n")
	a.sshBin, a.sftpBin = ssh, sftp
	remote := filepath.Join(t.TempDir(), "remote-project")
	if err := os.Mkdir(remote, 0700); err != nil {
		t.Fatal(err)
	}
	// Native guarded writes now use the SFTP subsystem, not just CLI batches.
	// Serve that real protocol over owned pipes and inspect the resulting bytes.
	attachOwnedSFTPProtocol(t, a, "", "", "")
	requireStatus(t, request(a, "PUT", "/api/workspace-config", map[string]any{
		"workspace": map[string]any{"mode": "ssh", "path": remote, "host": "example", "username": "dev", "port": 22, "auth": "none"},
		"cache":     map[string]any{"location": "workspace", "path": "cache"},
	}), 200)
	if strings.HasPrefix(a.cacheContainer, a.workPath+string(os.PathSeparator)) {
		t.Fatalf("project mirror leaked to product: %s", a.cacheContainer)
	}
	remoteCache := filepath.Join(remote, "cache")
	if a.workspaceRemoteCachePath() != filepath.ToSlash(remoteCache) {
		t.Fatal(a.workspaceRemoteCachePath())
	}
	if !strings.HasPrefix(a.sourcesPath(), ConfigDir(a.dataPath)) {
		t.Fatal(a.sourcesPath())
	}
	out := a.createDesign("isolation", "## 开发流程\ncheck", "", "")
	designs := filepath.Join(remoteCache, "system-docs", "designs")
	if !strings.Contains(out, filepath.ToSlash(designs)+"/") {
		t.Fatalf("document path: %s", out)
	}
	if out := a.writeMemory("project-only"); !strings.Contains(out, "已写入") {
		t.Fatal(out)
	}
	entries, err := os.ReadDir(designs)
	if err != nil {
		t.Fatal(err)
	}
	designName := ""
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "DESIGN-") && strings.HasSuffix(entry.Name(), ".md") {
			if designName != "" {
				t.Fatal("unexpected duplicate remote design", entries)
			}
			designName = entry.Name()
		}
	}
	if designName == "" {
		t.Fatal("missing remote design bytes", entries)
	}
	b, err := os.ReadFile(filepath.Join(designs, designName))
	if err != nil || !strings.Contains(string(b), "check") {
		t.Fatal("remote design not written", err, string(b))
	}
	b, err = os.ReadFile(filepath.Join(designs, "designs-index.md"))
	if err != nil || !utf8.Valid(b) || !strings.Contains(string(b), "| DESIGN-001 | isolation |") {
		t.Fatal("remote design index missing generated document", err, string(b))
	}
	b, err = os.ReadFile(filepath.Join(remoteCache, "aide", "memory.md"))
	if err != nil || !strings.Contains(string(b), "project-only") {
		t.Fatal("remote project memory not written", err, string(b))
	}
}

func TestLocalCacheCommandAndMemoryAgree(t *testing.T) {
	a := testApp(t)
	target := filepath.Join(a.workPath, "project-cache")
	a.wsConfig.Cache.Path = "/workspace/project-cache"
	if err := a.applyWorkspaceConfig(); err != nil {
		t.Fatal(err)
	}
	if a.memoryPath() != filepath.Join(target, ".cache", "aide", "memory.md") {
		t.Fatal(a.memoryPath())
	}
	a.settings.SandboxMode = "danger-full-access"
	out, _, err := a.execShellCommand(context.Background(), nil, "printf '%s' \"$AIDE_CACHE\"")
	if err != nil || out != filepath.Join(target, ".cache") {
		t.Fatalf("cache environment %q %v", out, err)
	}
}

func TestLocalCacheDirectoryKeepsExplicitDotCache(t *testing.T) {
	for input, want := range map[string]string{
		"/project":        "/project/.cache",
		"/project/.cache": "/project/.cache",
	} {
		if got := localCacheDirectory(input); got != want {
			t.Fatalf("%s: got %s, want %s", input, got, want)
		}
	}
}
