package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sshApplyFixture(t *testing.T, holdPhase, readFailure string) (*App, *Session, *Task, string, string, string) {
	t.Helper()
	a := testApp(t)
	reliabilitySSHStub(t, a, true)
	a.wsConfig.Workspace.Path = "."
	dir := t.TempDir()
	remote := filepath.Join(dir, "remote")
	if err := os.Mkdir(remote, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remote, "hello.txt"), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	entered, release := filepath.Join(dir, "entered"), filepath.Join(dir, "release")
	t.Cleanup(func() { _ = os.WriteFile(release, nil, 0600) })
	writes := filepath.Join(dir, "writes")
	a.sftpBin = filepath.Join(dir, "sftp")
	writeStub(t, a.sftpBin, `#!/bin/sh
while IFS= read -r line; do
  eval "set -- $line"
  if [ "$1" = `+shellQuote(holdPhase)+` ]; then
    touch `+shellQuote(entered)+`
    while [ ! -f `+shellQuote(release)+` ]; do sleep 0.01; done
  fi
  case "$1" in
    get)
      if [ -n `+shellQuote(readFailure)+` ]; then
        printf '%s\n' `+shellQuote(readFailure)+` >&2
        exit 1
      fi
      cp `+shellQuote(remote)+`/"$(basename "$2")" "$3" || exit 1
      ;;
    put)
      printf '%s\n' put >> `+shellQuote(writes)+`
      cp "$3" `+shellQuote(remote)+`/"$(basename "$4")" || exit 1
      ;;
    rename)
      mv `+shellQuote(remote)+`/"$(basename "$2")" `+shellQuote(remote)+`/"$(basename "$3")" || exit 1
      ;;
    ls) printf '%s\n' '-rw-r--r-- 1 user group 3 Oct 8 10:00 hello.txt' ;;
  esac
done
`)
	task := &Task{ID: "ssh-apply", Status: "awaiting_approval", WorkspaceID: a.wsID(), WorkspaceRev: a.wsRevision, Files: []Change{{Path: "hello.txt", Content: "new", BaseHash: hash([]byte("old"))}}}
	s := &Session{ID: newID(), Runs: []*Task{task}}
	a.sessions[s.ID] = s
	return a, s, task, remote, entered, release
}

func sshApplyRequest(a *App, s *Session, task *Task) <-chan *httptest.ResponseRecorder {
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- request(a, http.MethodPost, "/api/sessions/"+s.ID+"/runs/"+task.ID+"/apply", map[string]any{})
	}()
	return done
}

func awaitSSHApply(t *testing.T, done <-chan *httptest.ResponseRecorder) *httptest.ResponseRecorder {
	t.Helper()
	select {
	case response := <-done:
		return response
	case <-time.After(3 * time.Second):
		t.Fatal("SSH proposal apply blocked; application lock must not span remote I/O")
		return nil
	}
}

func awaitSSHApplyMarker(t *testing.T, marker string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("SSH transfer did not reach its disposable fixture checkpoint")
}

func releaseSSHApply(t *testing.T, marker string) {
	t.Helper()
	if err := os.WriteFile(marker, nil, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestSSHApplyCompletesAndPersists(t *testing.T) {
	a, s, task, remote, _, _ := sshApplyFixture(t, "", "")
	response := awaitSSHApply(t, sshApplyRequest(a, s, task))
	requireStatus(t, response, http.StatusOK)
	if b, err := os.ReadFile(filepath.Join(remote, "hello.txt")); err != nil || string(b) != "new" {
		t.Fatalf("approved remote write: %q %v", b, err)
	}
	a.mu.Lock()
	completed := task.Status == "completed" && task.Applied && task.Files[0].Applied
	a.mu.Unlock()
	if !completed {
		t.Fatalf("missing applied receipt: %+v", task)
	}
	var saved Session
	b, err := os.ReadFile(SessionPath(a.dataPath, s.ID, "active"))
	if err != nil || json.Unmarshal(b, &saved) != nil || len(saved.Runs) != 1 || !saved.Runs[0].Files[0].Applied {
		t.Fatalf("write receipt not persisted: %s %v", b, err)
	}
	// A repeated approval is serialized and cannot apply the same file twice.
	requireStatus(t, awaitSSHApply(t, sshApplyRequest(a, s, task)), http.StatusConflict)
}

func TestSSHApplyRechecksStateAfterRead(t *testing.T) {
	for _, change := range []string{"revision", "workspace", "status", "proposal", "deleted"} {
		t.Run(change, func(t *testing.T) {
			a, s, task, remote, entered, release := sshApplyFixture(t, "get", "")
			done := sshApplyRequest(a, s, task)
			awaitSSHApplyMarker(t, entered)
			a.mu.Lock()
			switch change {
			case "revision":
				a.wsRevision++ // Also detect switching away and back to the same identity.
			case "workspace":
				a.wsConfig.Workspace.Path = "/other-project"
				a.wsRevision++
			case "status":
				task.Status = "cancelled"
			case "proposal":
				task.Files[0].Content = "replacement proposal"
			case "deleted":
				s.Deleted = true
				delete(a.sessions, s.ID)
			}
			a.mu.Unlock()
			releaseSSHApply(t, release)
			requireStatus(t, awaitSSHApply(t, done), http.StatusConflict)
			if b, err := os.ReadFile(filepath.Join(remote, "hello.txt")); err != nil || string(b) != "old" {
				t.Fatalf("stale approval wrote a file: %q %v", b, err)
			}
		})
	}
}

func TestSSHApplyRecordsWriteBeforeStopping(t *testing.T) {
	a, s, task, remote, entered, release := sshApplyFixture(t, "put", "")
	task.Files = append(task.Files, Change{Path: "second.txt", Content: "new second", BaseHash: hash([]byte("old second"))})
	if err := os.WriteFile(filepath.Join(remote, "second.txt"), []byte("old second"), 0600); err != nil {
		t.Fatal(err)
	}
	done := sshApplyRequest(a, s, task)
	awaitSSHApplyMarker(t, entered)
	a.mu.Lock()
	a.wsRevision++
	a.mu.Unlock()
	releaseSSHApply(t, release)
	requireStatus(t, awaitSSHApply(t, done), http.StatusConflict)
	a.mu.Lock()
	partial := task.Files[0].Applied && !task.Files[1].Applied && !task.Applied && strings.Contains(task.Error, "文件已写入")
	a.mu.Unlock()
	if !partial {
		t.Fatalf("incorrect partial write receipt: %+v", task)
	}
	if b, _ := os.ReadFile(filepath.Join(remote, "second.txt")); string(b) != "old second" {
		t.Fatalf("applied another file after configuration changed: %q", b)
	}
	var saved Session
	b, err := os.ReadFile(SessionPath(a.dataPath, s.ID, "active"))
	if err != nil || json.Unmarshal(b, &saved) != nil || !saved.Runs[0].Files[0].Applied || saved.Runs[0].Files[1].Applied {
		t.Fatalf("partial write receipt not persisted: %s %v", b, err)
	}
}

func TestSSHApplyNewFileRequiresConfirmedAbsence(t *testing.T) {
	a, s, task, remote, _, _ := sshApplyFixture(t, "", "Permission denied")
	task.Files[0].BaseHash = ""
	requireStatus(t, awaitSSHApply(t, sshApplyRequest(a, s, task)), http.StatusConflict)
	if b, _ := os.ReadFile(filepath.Join(remote, "hello.txt")); string(b) != "old" {
		t.Fatalf("unreadable file was treated as absent: %q", b)
	}
}

func TestSSHApplyWaitsForFilesWithoutHoldingAppLock(t *testing.T) {
	a, s, task, _, _, _ := sshApplyFixture(t, "", "")
	a.filesMu.Lock()
	done := sshApplyRequest(a, s, task)
	// Give the first request an opportunity to reach its contested file lock.
	time.Sleep(25 * time.Millisecond)
	locked := make(chan struct{})
	go func() {
		a.mu.Lock()
		a.mu.Unlock()
		close(locked)
	}()
	select {
	case <-locked:
	case <-time.After(time.Second):
		a.filesMu.Unlock()
		t.Fatal("approval waiting for filesMu blocked a concurrent application-lock holder")
	}
	a.filesMu.Unlock()
	requireStatus(t, awaitSSHApply(t, done), http.StatusOK)
}

func TestSSHApplySerializesApprovals(t *testing.T) {
	a, s, task, remote, entered, release := sshApplyFixture(t, "get", "")
	first := sshApplyRequest(a, s, task)
	awaitSSHApplyMarker(t, entered)
	second := sshApplyRequest(a, s, task)
	select {
	case response := <-second:
		t.Fatalf("second approval bypassed the in-flight writer: %d", response.Code)
	case <-time.After(25 * time.Millisecond):
	}
	releaseSSHApply(t, release)
	requireStatus(t, awaitSSHApply(t, first), http.StatusOK)
	requireStatus(t, awaitSSHApply(t, second), http.StatusConflict)
	b, err := os.ReadFile(filepath.Join(filepath.Dir(remote), "writes"))
	if err != nil || strings.Count(string(b), "put\n") != 1 {
		t.Fatalf("concurrent approvals replayed a write: %q %v", b, err)
	}
}
