package server

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func decodeCommandEvents(t *testing.T, body io.Reader) []map[string]any {
	t.Helper()
	var events []map[string]any
	decoder := json.NewDecoder(body)
	for {
		var event map[string]any
		if err := decoder.Decode(&event); err == io.EOF {
			return events
		} else if err != nil {
			t.Fatalf("decode command stream: %v", err)
		}
		events = append(events, event)
	}
}

func commandEvent(events []map[string]any, kind string) map[string]any {
	for _, event := range events {
		if event["type"] == kind {
			return event
		}
	}
	return nil
}

func TestStandaloneCDParsing(t *testing.T) {
	for _, tc := range []struct {
		command, target string
	}{
		{"cd", "."},
		{"  cd  ", "."},
		{`cd "dir with spaces"`, "dir with spaces"},
		{`cd 'dir with spaces'`, "dir with spaces"},
		{`cd dir\ with\ spaces`, "dir with spaces"},
		{"cd -- nested", "nested"},
	} {
		target, ok := standaloneCD(tc.command)
		if !ok || target != tc.target {
			t.Errorf("standaloneCD(%q) = %q, %v; want %q, true", tc.command, target, ok, tc.target)
		}
	}
	for _, command := range []string{"pwd", "cd sub && pwd", "cd sub; pwd", `cd "$HOME"`, "cd -", "cd ~", "cd one two", `cd "unterminated`} {
		if target, ok := standaloneCD(command); ok {
			t.Errorf("standaloneCD(%q) unexpectedly returned %q", command, target)
		}
	}
}

func TestCommandPanelPersistentWorkingDirectory(t *testing.T) {
	a := testApp(t)
	a.settings.SandboxMode = "danger-full-access"
	nested := filepath.Join("dir with spaces", "nested")
	if err := os.MkdirAll(filepath.Join(a.workPath, nested), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(a.workPath, ".git"), 0700); err != nil {
		t.Fatal(err)
	}

	w := request(a, "POST", "/api/command", map[string]any{"command": `cd "dir with spaces"`, "cwd": "."})
	requireStatus(t, w, 200)
	event := commandEvent(decodeCommandEvents(t, w.Body), "cwd")
	if event == nil || event["cwd"] != "dir with spaces" {
		t.Fatalf("quoted cd did not persist its directory: %s", w.Body.String())
	}

	w = request(a, "POST", "/api/command", map[string]any{"command": "pwd", "cwd": "dir with spaces"})
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), filepath.Base(a.workPath)) || !strings.Contains(w.Body.String(), "dir with spaces") {
		t.Fatalf("next command did not use selected cwd: %s", w.Body.String())
	}

	w = request(a, "POST", "/api/command", map[string]any{"command": "cd nested", "cwd": "dir with spaces"})
	requireStatus(t, w, 200)
	event = commandEvent(decodeCommandEvents(t, w.Body), "cwd")
	if event == nil || event["cwd"] != nested {
		t.Fatalf("relative nested cd failed: %s", w.Body.String())
	}

	w = request(a, "POST", "/api/command", map[string]any{"command": "cd ../..", "cwd": nested})
	requireStatus(t, w, 200)
	event = commandEvent(decodeCommandEvents(t, w.Body), "cwd")
	if event == nil || event["cwd"] != "." {
		t.Fatalf("parent cd did not return to workspace: %s", w.Body.String())
	}

	absolute := filepath.Join(a.workPath, "dir with spaces")
	w = request(a, "POST", "/api/command", map[string]any{"command": `cd "` + absolute + `"`, "cwd": "."})
	requireStatus(t, w, 200)
	event = commandEvent(decodeCommandEvents(t, w.Body), "cwd")
	if event == nil || event["cwd"] != "dir with spaces" {
		t.Fatalf("absolute in-workspace cd failed: %s", w.Body.String())
	}

	a.workspaceDisplay = "/Users/fixture/project"
	for _, tc := range []struct{ target, cwd string }{
		{"/Users/fixture/project", "."},
		{"/Users/fixture/project/dir with spaces", "dir with spaces"},
	} {
		w = request(a, "POST", "/api/command", map[string]any{"command": `cd "` + tc.target + `"`, "cwd": "."})
		requireStatus(t, w, 200)
		event = commandEvent(decodeCommandEvents(t, w.Body), "cwd")
		if event == nil || event["cwd"] != tc.cwd {
			t.Fatalf("displayed workspace path %q failed: %s", tc.target, w.Body.String())
		}
	}
	w = request(a, "POST", "/api/command", map[string]any{"command": "cd /Users/fixture/other", "cwd": "."})
	requireStatus(t, w, 400)

	w = request(a, "POST", "/api/command", map[string]any{"command": `cd "dir with spaces" && pwd`, "cwd": "."})
	requireStatus(t, w, 200)
	body := w.Body.String()
	events := decodeCommandEvents(t, strings.NewReader(body))
	if commandEvent(events, "cwd") != nil || !strings.Contains(body, "dir with spaces") {
		t.Fatalf("compound cd must retain shell-only semantics: %s", body)
	}

	for _, tc := range []struct {
		command, cwd string
	}{
		{"cd ..", "."},
		{"cd .git", "."},
		{"cd missing", "."},
		{"pwd", "../"},
	} {
		w = request(a, "POST", "/api/command", map[string]any{"command": tc.command, "cwd": tc.cwd})
		requireStatus(t, w, 400)
	}
}

func TestResolveRemoteCommandDir(t *testing.T) {
	for _, tc := range []struct {
		base, current, target, dir, rel string
	}{
		{"/srv/workspace", ".", "sub dir", "/srv/workspace/sub dir", "sub dir"},
		{"/srv/workspace", "sub dir", "../other", "/srv/workspace/other", "other"},
		{"/srv/workspace", ".", "/srv/workspace/other", "/srv/workspace/other", "other"},
		{"/srv/workspace", "sub", "..", "/srv/workspace", "."},
		{"/", ".", "/tmp", "/tmp", "tmp"},
	} {
		dir, rel, err := resolveRemoteCommandDir(tc.base, tc.current, tc.target)
		if err != nil || dir != tc.dir || rel != tc.rel {
			t.Errorf("resolveRemoteCommandDir(%q, %q, %q) = %q, %q, %v; want %q, %q", tc.base, tc.current, tc.target, dir, rel, err, tc.dir, tc.rel)
		}
	}
	for _, target := range []string{"..", "/srv/workspace2", ".git", "/etc"} {
		if _, _, err := resolveRemoteCommandDir("/srv/workspace", ".", target); err == nil {
			t.Errorf("remote target %q escaped policy", target)
		}
	}
}
