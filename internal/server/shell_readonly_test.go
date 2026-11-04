package server

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBasicReadOnlyShellClassification(t *testing.T) {
	for _, s := range []string{`cat "file with spaces.txt"`, `rg 'TODO|FIXME' .`, `find . -name '*.go'`, `sed -n '1,120p' README.md`, `sed -n '1,$p' README.md`, `pwd && ls -la`, `cat README.md | head -n 5`, `cd 'some folder' && ls`, `grep 'a;b' file.txt`} {
		if !readOnlyAllowed(s) {
			t.Errorf("routine read requires approval: %s", s)
		}
	}
	for _, s := range []string{`mkdir docs`, `cat *.txt`, `cat $(touch x)`, `cat "$(touch x)"`, `cat x > out`, `cat x | tee out`, `ls && touch x`, `ls || touch x`, `ls &`, `cd .`, `sed -n '1p;w out' x`, `sed -i 's/a/b/' x`, `sed -n '1p' -i x`, `find . -de'lete'`, `find . -exec sh -c 'touch x' ';'`, `rg --pre=sh x`, `file -C -m magic`, `file -Cm magic`, `file --comp magic`, `git status`, `git diff`, `git log --out=out`, `git diff --output=out`, `git log --output out`, `git diff --ext-diff`, `git show --textconv HEAD`, `ls; echo x`, `ls\nx`, `ls # comment`, `'cat`, `cat <(touch x)`, `echo x\n touch x`} {
		if readOnlyAllowed(s) {
			t.Errorf("ambiguous/write/evaluation bypass: %s", s)
		}
	}
}

func TestReadOnlyShellCommandLengthBoundsWithoutCommandCountCap(t *testing.T) {
	parts := make([]string, 256)
	for i := range parts {
		parts[i] = "pwd"
	}
	command := strings.Join(parts, " && ")
	if len(command) >= 16000 {
		t.Fatal("test command unexpectedly exceeds the byte limit")
	}
	if !readOnlyAllowed(command) {
		t.Fatal("long chain of literal read-only commands was rejected by a command-count cap")
	}
	if readOnlyAllowed(strings.Repeat("x", 16001)) {
		t.Fatal("command exceeding the byte limit was accepted")
	}
}

func TestBasicReadOnlyShellExecutesWithoutReviewer(t *testing.T) {
	a := testApp(t)
	task := &Task{ID: "routine-read", Status: "running"}
	if err := os.WriteFile(filepath.Join(a.workPath, "file with spaces.txt"), []byte("alpha\nbeta\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range []string{`cat "file with spaces.txt"`, `sed -n '1,1p' 'file with spaces.txt'`, `cat 'file with spaces.txt' | head -n 1`, `pwd && ls`} {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		out, ok := a.approvedShell(ctx, task, cmd, 0)
		cancel()
		if !ok || !strings.Contains(out, "exit code: 0") {
			t.Fatalf("routine read did not run without approval: %s %s", cmd, out)
		}
		if task.PendingQuestion != nil || len(task.ApprovalReviews) != 0 {
			t.Fatal("routine read entered reviewer")
		}
	}
}

func TestGitRepositoryHelpersRequireApproval(t *testing.T) {
	a := testApp(t)
	repo := filepath.Join(a.workPath, "helper-config-repo")
	if err := os.MkdirAll(repo, 0700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "init", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	marker := filepath.Join(t.TempDir(), "fsmonitor-ran")
	helper := filepath.Join(t.TempDir(), "fsmonitor-helper")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\ntouch \""+marker+"\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", repo, "config", "core.fsmonitor", helper).CombinedOutput(); err != nil {
		t.Fatalf("git config: %v: %s", err, out)
	}
	command := "git -C " + shellQuote(repo) + " status"
	if readOnlyAllowed(command) {
		t.Fatal("Git with repository-local helpers must not bypass approval")
	}
	task := &Task{ID: "git-helper-approval", Status: "running"}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, ok := a.approvedShell(ctx, task, command, 0); ok {
		t.Fatal("Git helper command executed through the read-only path")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("repository fsmonitor helper unexpectedly ran: %v", err)
	}
}
