package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func applyHistoryFixture(t *testing.T, markdown bool) (*App, *Session, *Task, string) {
	t.Helper()
	a := testApp(t)
	enableMarkdownHistory(t, a)
	name := "first.py"
	if markdown {
		name = "first.md"
	}
	task := &Task{ID: "history-partial", Status: "awaiting_approval", WorkspaceID: a.wsID(), WorkspaceRev: a.wsRevision,
		Files: []Change{{Path: name, Content: "saved first\n"}, {Path: "second.py", Content: "saved second\n"}}}
	s := &Session{ID: "history-owner", Runs: []*Task{task}}
	a.sessions[s.ID] = s
	dir := a.markdownHistoryDir("workspace:" + a.wsID())
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(dir, "unreadable.json")
	if markdown {
		// New Markdown has no pre-write capture, so this fails after the writer.
		blocked = historyIndexPath(dir, name)
		if err := os.Mkdir(blocked, 0700); err != nil {
			t.Fatal(err)
		}
	} else if err := os.Symlink("missing-index", blocked); err != nil {
		t.Fatal(err)
	}
	return a, s, task, blocked
}

func TestApplyHistoryPostWriteRecovery(t *testing.T) {
	for _, markdown := range []bool{false, true} {
		name := "script"
		if markdown {
			name = "markdown"
		}
		t.Run(name, func(t *testing.T) {
			a, s, task, blocked := applyHistoryFixture(t, markdown)
			url := "/api/sessions/" + s.ID + "/runs/" + task.ID + "/apply"
			requireStatus(t, request(a, "POST", url, map[string]any{}), 500)
			first := filepath.Join(a.workPath, task.Files[0].Path)
			raw, err := os.ReadFile(first)
			if err != nil || string(raw) != task.Files[0].Content || !task.Files[0].Applied || !task.Files[0].HistoryPending || task.Applied || task.Status != "awaiting_approval" {
				t.Fatalf("partial state was not recorded: %q %v %+v", raw, err, task)
			}
			if _, err := os.Stat(filepath.Join(a.workPath, "second.py")); !os.IsNotExist(err) {
				t.Fatal("later proposal ran after archive failure", err)
			}
			before, err := os.Stat(first)
			if err != nil {
				t.Fatal(err)
			}
			out := taskOutcomeProjection(task, s.ID, 1, "", 0, 0, 5)
			if files := out["files"].([]outcomeFile); files[0].State != "saved_history_pending" {
				t.Fatalf("outcome lost the pending archive: %+v", files)
			}
			read := proposalReadJSON(t, a, task, map[string]any{"path": task.Files[0].Path})
			if read["applied"] != true || read["historyPending"] != true {
				t.Fatal("proposal read hid partial saved state", read)
			}
			// A repeated failure must only attempt archive recovery, never rewrite.
			requireStatus(t, request(a, "POST", url, map[string]any{}), 500)
			still, _ := os.Stat(first)
			if !os.SameFile(before, still) || !before.ModTime().Equal(still.ModTime()) || !task.Files[0].HistoryPending {
				t.Fatal("retry rewrote a saved file or cleared unresolved history")
			}
			// Exercise the persisted task representation, not only in-memory flags.
			encoded, err := json.Marshal(task)
			if err != nil {
				t.Fatal(err)
			}
			var resumed Task
			if err := json.Unmarshal(encoded, &resumed); err != nil || !resumed.Files[0].Applied || !resumed.Files[0].HistoryPending || resumed.Files[0].BaseHash != "" {
				t.Fatalf("partial state did not survive serialization: %v %+v", err, resumed)
			}
			s.Runs[0] = &resumed
			if err := os.Remove(blocked); err != nil {
				t.Fatal(err)
			}
			requireStatus(t, request(a, "POST", url, map[string]any{}), 200)
			after, _ := os.Stat(first)
			if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
				t.Fatal("successful archive repair rewrote the existing file")
			}
			if !resumed.Applied || resumed.Status != "completed" || resumed.Error != "" {
				t.Fatalf("recovery did not finish: %+v", resumed)
			}
			for _, f := range resumed.Files {
				b, err := os.ReadFile(filepath.Join(a.workPath, f.Path))
				if err != nil || string(b) != f.Content || !f.Applied || f.HistoryPending {
					t.Fatalf("wrong final disk/history state: %q %v %+v", b, err, f)
				}
			}
			if markdown {
				requireStatus(t, request(a, "GET", "/api/file/history?path=first.md", nil), 200)
			}
		})
	}
}

func TestApplyHistoryRecoveryRejectsChangedState(t *testing.T) {
	for _, change := range []string{"bytes", "workspace", "readonly", "invalid-record"} {
		t.Run(change, func(t *testing.T) {
			a, s, task, blocked := applyHistoryFixture(t, false)
			url := "/api/sessions/" + s.ID + "/runs/" + task.ID + "/apply"
			requireStatus(t, request(a, "POST", url, map[string]any{}), 500)
			if err := os.Remove(blocked); err != nil {
				t.Fatal(err)
			}
			first := filepath.Join(a.workPath, task.Files[0].Path)
			wanted := task.Files[0].Content
			code := 409
			switch change {
			case "bytes":
				wanted = "later user edit"
				if err := os.WriteFile(first, []byte(wanted), 0600); err != nil {
					t.Fatal(err)
				}
			case "workspace":
				task.WorkspaceID = "other-workspace"
			case "readonly":
				a.settings.SandboxMode = "read-only"
				code = 403
			case "invalid-record":
				task.Files[0].Applied = false
			}
			requireStatus(t, request(a, "POST", url, map[string]any{}), code)
			b, err := os.ReadFile(first)
			if err != nil || string(b) != wanted || task.Applied || !task.Files[0].HistoryPending {
				t.Fatalf("recovery changed protected data/state: %q %v %+v", b, err, task)
			}
			if _, err := os.Stat(filepath.Join(a.workPath, "second.py")); !os.IsNotExist(err) {
				t.Fatal("rejected recovery wrote the later file", err)
			}
		})
	}
}

func TestApplyHistoryPreWriteFailureDoesNotRecordApplied(t *testing.T) {
	for _, phase := range []string{"before-history", "invalid-parent", "identical-existing"} {
		t.Run(phase, func(t *testing.T) {
			a, s, task, blocked := applyHistoryFixture(t, true)
			first := filepath.Join(a.workPath, task.Files[0].Path)
			if phase == "before-history" {
				if err := os.WriteFile(first, []byte("original"), 0600); err != nil {
					t.Fatal(err)
				}
				task.Files[0].Before, task.Files[0].BaseHash = "original", hash([]byte("original"))
			} else if phase == "invalid-parent" {
				if err := os.Remove(blocked); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(a.workPath, "blocked"), []byte("parent is a file"), 0600); err != nil {
					t.Fatal(err)
				}
				task.Files[0].Path = "blocked/first.py"
			} else if err := os.WriteFile(first, []byte(task.Files[0].Content), 0600); err != nil {
				t.Fatal(err)
			}
			code := 500
			if phase == "identical-existing" || phase == "invalid-parent" {
				code = 409
			}
			requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs/"+task.ID+"/apply", map[string]any{}), code)
			if task.Applied || task.Files[0].Applied || task.Files[0].HistoryPending {
				t.Fatalf("failure without a successful writer was adopted: %+v", task)
			}
			if phase == "before-history" {
				b, err := os.ReadFile(first)
				if err != nil || string(b) != "original" {
					t.Fatal("pre-write history failure changed original", err)
				}
			}
		})
	}
}
