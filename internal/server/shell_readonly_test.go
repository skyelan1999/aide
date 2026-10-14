package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBasicReadOnlyShellClassification(t *testing.T) {
	for _, s := range []string{`cat "file with spaces.txt"`, `rg 'TODO|FIXME' .`, `find . -name '*.go'`, `sed -n '1,120p' README.md`, `sed -n '1,$p' README.md`, `pwd && ls -la`, `cat README.md | head -n 5`, `cd 'some folder' && ls`, `git log --format='%h %s'`, `grep 'a;b' file.txt`} {
		if !readOnlyAllowed(s) {
			t.Errorf("routine read requires approval: %s", s)
		}
	}
	for _, s := range []string{`mkdir docs`, `cat *.txt`, `cat $(touch x)`, `cat "$(touch x)"`, `cat x > out`, `cat x | tee out`, `ls && touch x`, `ls || touch x`, `ls &`, `cd .`, `sed -n '1p;w out' x`, `sed -i 's/a/b/' x`, `sed -n '1p' -i x`, `find . -de'lete'`, `find . -exec sh -c 'touch x' ';'`, `rg --pre=sh x`, `file -C -m magic`, `file -Cm magic`, `file --comp magic`, `git log --out=out`, `git diff --output=out`, `git log --output out`, `git diff --ext-diff`, `git show --textconv HEAD`, `ls; echo x`, `ls\nx`, `ls # comment`, `'cat`, `cat <(touch x)`, `echo x\n touch x`} {
		if readOnlyAllowed(s) {
			t.Errorf("ambiguous/write/evaluation bypass: %s", s)
		}
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
