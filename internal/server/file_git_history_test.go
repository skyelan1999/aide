package server

import (
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitHistoryTestRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.test", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.test")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
func gitHistoryTestRequest(t *testing.T, a *App, p, from, to string) map[string]any {
	t.Helper()
	q := url.Values{"path": {p}, "workspaceId": {a.wsID()}}
	if from != "" {
		q.Set("from", from)
	}
	if to != "" {
		q.Set("to", to)
	}
	w := request(a, "GET", "/api/file/git-history?"+q.Encode(), nil)
	requireStatus(t, w, 200)
	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}
func TestFileGitHistoryCompare(t *testing.T) {
	a := testApp(t)
	dir := a.workspace.Name()
	gitHistoryTestRun(t, dir, "init", "-q")
	file := "文件 [a]*.txt"
	write := func(text string) {
		t.Helper()
		if e := os.WriteFile(filepath.Join(dir, file), []byte(text), 0600); e != nil {
			t.Fatal(e)
		}
	}
	write("first\n")
	gitHistoryTestRun(t, dir, "add", "--", file)
	gitHistoryTestRun(t, dir, "commit", "-qm", "first revision")
	first := gitHistoryTestRun(t, dir, "rev-parse", "HEAD")
	write("second\n")
	gitHistoryTestRun(t, dir, "add", "--", file)
	gitHistoryTestRun(t, dir, "commit", "-qm", "second revision")
	write("staged\n")
	gitHistoryTestRun(t, dir, "add", "--", file)
	write("working\n")
	// Dangerous project configuration must never execute during read-only inspection.
	marker := filepath.Join(t.TempDir(), "executed")
	script := filepath.Join(t.TempDir(), "danger.sh")
	if e := os.WriteFile(script, []byte("#!/bin/sh\ntouch '"+marker+"'\n"), 0700); e != nil {
		t.Fatal(e)
	}
	gitHistoryTestRun(t, dir, "config", "core.fsmonitor", script)
	gitHistoryTestRun(t, dir, "config", "diff.external", script)
	indexBefore, e := os.ReadFile(filepath.Join(dir, ".git", "index"))
	if e != nil {
		t.Fatal(e)
	}
	list := gitHistoryTestRequest(t, a, file, "", "")
	if list["available"] != true || len(list["versions"].([]any)) != 2 {
		t.Fatalf("list: %v", list)
	}
	for _, pair := range [][4]string{{"HEAD", "worktree", "second\n", "working\n"}, {"HEAD", "index", "second\n", "staged\n"}, {"index", "worktree", "staged\n", "working\n"}, {first, "HEAD", "first\n", "second\n"}} {
		got := gitHistoryTestRequest(t, a, file, pair[0], pair[1])
		if got["before"].(map[string]any)["content"] != pair[2] || got["after"].(map[string]any)["content"] != pair[3] {
			t.Fatalf("compare: %v", got)
		}
		patch := got["diff"].(string)
		if !strings.Contains(patch, "-"+strings.TrimSpace(pair[2])) || !strings.Contains(patch, "+"+strings.TrimSpace(pair[3])) || strings.Contains(patch, "aide-git-compare-") {
			t.Fatalf("patch: %s", patch)
		}
	}
	if _, e = os.Stat(marker); !os.IsNotExist(e) {
		t.Fatal("external diff or fsmonitor executed")
	}
	indexAfter, _ := os.ReadFile(filepath.Join(dir, ".git", "index"))
	if string(indexBefore) != string(indexAfter) {
		t.Fatal("comparison mutated index")
	}
	current, _ := os.ReadFile(filepath.Join(dir, file))
	if string(current) != "working\n" {
		t.Fatal("comparison mutated file")
	}
	if e = os.WriteFile(filepath.Join(dir, "new.txt"), []byte("untracked\n"), 0600); e != nil {
		t.Fatal(e)
	}
	untracked := gitHistoryTestRequest(t, a, "new.txt", "HEAD", "worktree")
	if untracked["before"].(map[string]any)["exists"] != false || !strings.Contains(untracked["diff"].(string), "+untracked") {
		t.Fatal(untracked)
	}
	if e = os.Remove(filepath.Join(dir, file)); e != nil {
		t.Fatal(e)
	}
	deleted := gitHistoryTestRequest(t, a, file, "HEAD", "worktree")
	if deleted["after"].(map[string]any)["exists"] != false {
		t.Fatal(deleted)
	}
}
func TestFileGitHistoryBoundaries(t *testing.T) {
	a := testApp(t)
	dir := a.workspace.Name()
	if gitHistoryTestRequest(t, a, "page.txt", "", "")["available"] != false {
		t.Fatal("nonrepo reported available")
	}
	nested := filepath.Join(dir, "nested")
	if e := os.Mkdir(nested, 0700); e != nil {
		t.Fatal(e)
	}
	gitHistoryTestRun(t, nested, "init", "-q")
	if e := os.WriteFile(filepath.Join(nested, "page.txt"), []byte("hello\n"), 0600); e != nil {
		t.Fatal(e)
	}
	gitHistoryTestRun(t, nested, "add", ".")
	gitHistoryTestRun(t, nested, "commit", "-qm", "baseline")
	if gitHistoryTestRequest(t, a, "nested/page.txt", "", "")["available"] != true {
		t.Fatal("nested repo missing")
	}
	for _, q := range []string{"path=../outside", "path=.git/config", "path=nested/page.txt&workspaceId=wrong", "path=nested/page.txt&from=HEAD%3Aother&to=worktree", "path=nested/page.txt&from=--help&to=worktree"} {
		w := request(a, "GET", "/api/file/git-history?"+q, nil)
		if w.Code < 400 {
			t.Fatalf("unsafe request accepted: %s %s", q, w.Body)
		}
	}
	if e := os.Symlink(t.TempDir(), filepath.Join(dir, "escape")); e != nil {
		t.Fatal(e)
	}
	requireStatus(t, request(a, "GET", "/api/file/git-history?path=escape/file.txt", nil), 400)
	if e := os.WriteFile(filepath.Join(nested, "binary.txt"), []byte{0, 1, 2}, 0600); e != nil {
		t.Fatal(e)
	}
	requireStatus(t, request(a, "GET", "/api/file/git-history?path=nested/binary.txt&from=HEAD&to=worktree", nil), 400)
	if e := os.WriteFile(filepath.Join(nested, "large.txt"), []byte(strings.Repeat("x", gitHistoryTextLimit+1)), 0600); e != nil {
		t.Fatal(e)
	}
	requireStatus(t, request(a, "GET", "/api/file/git-history?path=nested/large.txt&from=HEAD&to=worktree", nil), 400)
	a.mu.Lock()
	a.wsConfig.Workspace.Mode = "ssh"
	a.mu.Unlock()
	if gitHistoryTestRequest(t, a, "nested/page.txt", "", "")["available"] != false {
		t.Fatal("SSH cache exposed as local Git")
	}
}
func TestFileGitHistoryWorktree(t *testing.T) {
	a := testApp(t)
	dir := a.workspace.Name()
	repo := filepath.Join(dir, "repo")
	if e := os.Mkdir(repo, 0700); e != nil {
		t.Fatal(e)
	}
	gitHistoryTestRun(t, repo, "init", "-q")
	if e := os.WriteFile(filepath.Join(repo, "page.txt"), []byte("linked\n"), 0600); e != nil {
		t.Fatal(e)
	}
	gitHistoryTestRun(t, repo, "add", ".")
	gitHistoryTestRun(t, repo, "commit", "-qm", "baseline")
	gitHistoryTestRun(t, repo, "worktree", "add", "-q", "-b", "linked", filepath.Join(dir, "linked"))
	got := gitHistoryTestRequest(t, a, "linked/page.txt", "HEAD", "worktree")
	if got["before"].(map[string]any)["content"] != "linked\n" || got["diff"] != "" {
		t.Fatal(got)
	}
}
