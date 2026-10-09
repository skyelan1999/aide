package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSourceBrowseDraftDoesNotSaveSource(t *testing.T) {
	a := testApp(t)
	a.mu.Lock()
	before := len(a.sourceRegistry.Sources)
	a.mu.Unlock()
	log := reliabilitySSHStub(t, a, true)
	sftp := filepath.Join(t.TempDir(), "sftp")
	writeStub(t, sftp, "#!/bin/sh\ncat >/dev/null\nprintf '%s\\n' 'drwxr-xr-x 2 fixture fixture 4096 Jan 1 2026 child folder'\n")
	a.sftpBin = sftp
	w := request(a, "POST", "/api/sources/browse", map[string]any{"path": "/srv/refs", "config": map[string]any{"host": "fixture.example", "username": "fixture", "auth": "none"}})
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), "/srv/refs/child folder") {
		t.Fatal(w.Body.String())
	}
	a.mu.Lock()
	count := len(a.sourceRegistry.Sources)
	a.mu.Unlock()
	if count != before {
		t.Fatalf("draft browse saved source: %d", count)
	}
	b, err := os.ReadFile(log)
	if err != nil || !strings.Contains(string(b), "exit\n") {
		t.Fatalf("draft connection not closed: %q %v", b, err)
	}
}

func TestSourceBrowseRejectsInvalidInput(t *testing.T) {
	a := testApp(t)
	for _, body := range []map[string]any{
		{"path": "/srv\nls", "config": map[string]any{"host": "host", "auth": "none"}},
		{"path": ".", "config": map[string]any{"host": "-option", "auth": "none"}},
		{"path": ".", "config": map[string]any{"host": "host", "port": 70000, "auth": "none"}},
		{"path": ".", "config": map[string]any{"host": "host", "auth": "invalid"}},
	} {
		requireStatus(t, request(a, "POST", "/api/sources/browse", body), 400)
	}
}
