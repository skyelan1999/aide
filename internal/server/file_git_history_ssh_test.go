package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A shell transport double executes the exact remote command against a distinct
// temporary tree. The opt-in live test below independently exercises OpenSSH.
func sshGitHistoryTestApp(t *testing.T) (*App, string) {
	t.Helper()
	a := testApp(t)
	remote := t.TempDir()
	script := filepath.Join(t.TempDir(), "ssh-double")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nfor arg do\n[ \"$arg\" != check ] || exit 0\nlast=$arg\ndone\nexec sh -c \"$last\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	a.sshBin = script
	a.wsConfig.Workspace.Mode = "ssh"
	a.wsConfig.Workspace.Host = "fixture.invalid"
	a.wsConfig.Workspace.Path = remote
	return a, remote
}

func TestFileGitHistorySSHCompare(t *testing.T) {
	a, remote := sshGitHistoryTestApp(t)
	// The local cache has conflicting data and must never satisfy this request.
	gitHistoryTestRun(t, a.workspace.Name(), "init", "-q")
	file := "文件 ' $(literal) [a]*.txt"
	if err := os.WriteFile(filepath.Join(a.workspace.Name(), file), []byte("LOCAL CACHE"), 0600); err != nil {
		t.Fatal(err)
	}
	gitHistoryTestRun(t, remote, "init", "-q")
	write := func(text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(remote, file), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("remote first\n")
	gitHistoryTestRun(t, remote, "add", "--", file)
	gitHistoryTestRun(t, remote, "commit", "-qm", "first")
	first := gitHistoryTestRun(t, remote, "rev-parse", "HEAD")
	write("remote second\n")
	gitHistoryTestRun(t, remote, "add", "--", file)
	gitHistoryTestRun(t, remote, "commit", "-qm", "second")
	write("remote staged\n")
	gitHistoryTestRun(t, remote, "add", "--", file)
	write("remote disk\n")
	marker := filepath.Join(t.TempDir(), "should-not-run")
	gitHistoryTestRun(t, remote, "config", "core.fsmonitor", "touch "+marker)
	gitHistoryTestRun(t, remote, "config", "diff.external", "touch "+marker)
	index, _ := os.ReadFile(filepath.Join(remote, ".git/index"))
	listing := gitHistoryTestRequest(t, a, file, "", "")
	if listing["available"] != true || len(listing["versions"].([]any)) != 2 {
		t.Fatal(listing)
	}
	for _, pair := range [][4]string{{first, "HEAD", "remote first\n", "remote second\n"}, {"HEAD", "index", "remote second\n", "remote staged\n"}, {"index", "worktree", "remote staged\n", "remote disk\n"}} {
		got := gitHistoryTestRequest(t, a, file, pair[0], pair[1])
		if got["before"].(map[string]any)["content"] != pair[2] || got["after"].(map[string]any)["content"] != pair[3] {
			t.Fatal(got)
		}
		if !strings.Contains(got["diff"].(string), "+"+strings.TrimSpace(pair[3])) {
			t.Fatal(got)
		}
	}
	after, _ := os.ReadFile(filepath.Join(remote, ".git/index"))
	if string(index) != string(after) {
		t.Fatal("index mutated")
	}
	disk, _ := os.ReadFile(filepath.Join(remote, file))
	if string(disk) != "remote disk\n" {
		t.Fatal("worktree mutated")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("untrusted Git helper executed")
	}
	if err := os.Remove(filepath.Join(remote, file)); err != nil {
		t.Fatal(err)
	}
	if gitHistoryTestRequest(t, a, file, "HEAD", "worktree")["after"].(map[string]any)["exists"] != false {
		t.Fatal("deleted remote file")
	}
}

func TestFileGitHistorySSHBoundaries(t *testing.T) {
	a, remote := sshGitHistoryTestApp(t)
	if gitHistoryTestRequest(t, a, "no.txt", "", "")["available"] != false {
		t.Fatal("nonrepo")
	}
	nested := filepath.Join(remote, "nested")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	gitHistoryTestRun(t, nested, "init", "-q")
	if err := os.WriteFile(filepath.Join(nested, "untracked.txt"), []byte("new\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got := gitHistoryTestRequest(t, a, "nested/untracked.txt", "index", "worktree")
	if got["available"] != true {
		t.Fatal(got)
	}
	if got["before"].(map[string]any)["exists"] != false || got["after"].(map[string]any)["content"] != "new\n" {
		t.Fatal(got)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(remote, "escape")); err != nil {
		t.Fatal(err)
	}
	for _, q := range []url.Values{
		{"path": {"escape/file.txt"}}, {"path": {"nested/untracked.txt"}, "from": {"HEAD~1"}, "to": {"worktree"}},
		{"path": {"../outside"}}, {"path": {"nested/untracked.txt"}, "workspaceId": {"old-workspace"}},
	} {
		w := request(a, "GET", "/api/file/git-history?"+q.Encode(), nil)
		if w.Code != 400 && w.Code != 409 {
			t.Fatalf("%s: %d %s", q.Encode(), w.Code, w.Body.String())
		}
	}
	for name, content := range map[string][]byte{"large.txt": []byte(strings.Repeat("x", gitHistoryTextLimit+1)), "binary.txt": {0, 1, 2}} {
		if err := os.WriteFile(filepath.Join(nested, name), content, 0600); err != nil {
			t.Fatal(err)
		}
		requireStatus(t, request(a, "GET", "/api/file/git-history?path=nested/"+name+"&from=index&to=worktree", nil), 400)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	repo, _, err := a.findSSHGitHistoryRepo(ctx, "nested/untracked.txt")
	if err != nil || repo == nil {
		t.Fatal(repo, err)
	}
	a.invalidateSSHSession(sshControlSocket)
	if _, err = repo.remoteRead(ctx); err == nil || !strings.Contains(err.Error(), "已切换") {
		t.Fatal("generation change accepted", err)
	}
	a2, _ := sshGitHistoryTestApp(t)
	ctx2, cancel2 := context.WithCancel(context.Background())
	cancel2()
	if _, _, err = a2.findSSHGitHistoryRepo(ctx2, "x.txt"); err == nil || !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation not propagated", err)
	}
}

// Separate real SSH acceptance, invoked by scripts/test-ssh-workspace.sh.
func TestSSHLiveGitHistory(t *testing.T) {
	host := os.Getenv("AIDE_SSH_FIXTURE_A")
	if host == "" {
		t.Skip("requires isolated SSH fixture")
	}
	if os.Getenv("AIDE_SSH_TEST_ISOLATED") != "1" {
		t.Fatal("isolated fixture required")
	}
	a := testApp(t)
	base := "/home/fixture/git-history-" + newID()
	requireStatus(t, request(a, "PUT", "/api/workspace-config", map[string]any{"workspace": map[string]any{"mode": "ssh", "host": host, "port": 22, "username": "fixture", "auth": "password", "path": base}, "password": "fixture-pass"}), 200)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := a.ensureSSHSession(ctx); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr strings.Builder
	command := "mkdir -p " + shellQuote(base) + " && cd " + shellQuote(base) + " && git init -q && git config user.name Fixture && git config user.email fixture@example.test && printf 'remote first\\n' > 'file space.txt' && git add -- 'file space.txt' && git commit -qm first && printf 'remote staged\\n' > 'file space.txt' && git add -- 'file space.txt' && printf 'remote disk\\n' > 'file space.txt'"
	code, err := a.execRemote(ctx, command, &stdout, &stderr)
	if err != nil || code != 0 {
		t.Fatalf("prepare: %d %v %s", code, err, stderr.String())
	}
	got := gitHistoryTestRequest(t, a, "file space.txt", "HEAD", "index")
	if got["before"].(map[string]any)["content"] != "remote first\n" || got["after"].(map[string]any)["content"] != "remote staged\n" {
		t.Fatal(got)
	}
	got = gitHistoryTestRequest(t, a, "file space.txt", "index", "worktree")
	if got["after"].(map[string]any)["content"] != "remote disk\n" {
		t.Fatal(got)
	}
	list := gitHistoryTestRequest(t, a, "file space.txt", "", "")
	if len(list["versions"].([]any)) != 1 {
		t.Fatal(list)
	}
	// Probe also supplies the non-Markdown editor's history-button visibility.
	w := request(a, "GET", "/api/file/git-history?path=file%20space.txt&probe=1", nil)
	requireStatus(t, w, 200)
	var probe map[string]any
	if err = json.Unmarshal(w.Body.Bytes(), &probe); err != nil || probe["available"] != true {
		t.Fatal(probe, err)
	}
}
