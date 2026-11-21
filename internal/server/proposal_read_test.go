package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func proposalReadFixture(t *testing.T) (*App, *Task, *Task) {
	t.Helper()
	a := testApp(t)
	old := &Task{ID: "old", WorkspaceID: a.wsID(), WorkspaceRev: a.wsRevision, Status: "awaiting_approval", Files: []Change{{Path: "draft.py", Content: "甲乙\nprint(1)\n", BaseHash: "disk-base"}}}
	current := &Task{ID: "current", WorkspaceID: a.wsID(), WorkspaceRev: a.wsRevision}
	a.sessions["own"] = &Session{ID: "own", Runs: []*Task{old, current}}
	a.sessions["other"] = &Session{ID: "other", Runs: []*Task{{ID: "foreign", WorkspaceID: a.wsID(), WorkspaceRev: a.wsRevision, Files: []Change{{Path: "secret", Content: "other-chat"}}}}}
	return a, old, current
}

func proposalReadJSON(t *testing.T, a *App, task *Task, args map[string]any) map[string]any {
	t.Helper()
	text, err := a.readProposal(task, args)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal([]byte(text), &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestReadProposalOwnPriorDraftWithoutApplying(t *testing.T) {
	a, old, current := proposalReadFixture(t)
	list := proposalReadJSON(t, a, current, map[string]any{})
	items := list["candidates"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["runId"] != old.ID {
		t.Fatalf("unexpected candidates: %v", list)
	}
	read := proposalReadJSON(t, a, current, map[string]any{"runId": old.ID, "path": "draft.py", "sha256": hash([]byte(old.Files[0].Content))})
	if read["content"] != old.Files[0].Content || read["applied"] != false || read["done"] != true {
		t.Fatalf("wrong draft: %v", read)
	}
	if _, err := os.Stat(filepath.Join(a.workPath, "draft.py")); !os.IsNotExist(err) || old.Applied || old.Files[0].BaseHash != "disk-base" || len(current.readSnapshots) != 0 {
		t.Fatal("candidate read changed disk, approval or base-version read state")
	}
}

func TestReadProposalUTF8PagingAndStaleHash(t *testing.T) {
	a, old, current := proposalReadFixture(t)
	first := proposalReadJSON(t, a, current, map[string]any{"runId": old.ID, "path": "draft.py", "limit": float64(4)})
	if first["content"] != "甲" || first["nextOffset"] != float64(3) || first["done"] != false {
		t.Fatalf("split UTF8: %v", first)
	}
	second := proposalReadJSON(t, a, current, map[string]any{"runId": old.ID, "path": "draft.py", "offset": float64(3)})
	if first["content"].(string)+second["content"].(string) != old.Files[0].Content {
		t.Fatal("paging lost bytes")
	}
	for _, args := range []map[string]any{
		{"runId": old.ID, "path": "draft.py", "sha256": "stale"},
		{"runId": old.ID, "path": "draft.py", "offset": float64(1)},
		{"runId": old.ID, "path": "draft.py", "limit": float64(1)},
		{"limit": float64(0)}, {"limit": float64(51)}, {"offset": float64(-1)}, {"offset": 0.5},
		{"path": "draft.py", "limit": float64(24577)},
	} {
		if _, err := a.readProposal(current, args); err == nil {
			t.Fatalf("invalid read accepted: %v", args)
		}
	}
}

func TestReadProposalScopeAndPaging(t *testing.T) {
	a, old, current := proposalReadFixture(t)
	old.Files = append(old.Files, Change{Path: "second.py", Content: "second"})
	page := proposalReadJSON(t, a, current, map[string]any{"limit": float64(1), "offset": float64(1)})
	if page["done"] != true || page["nextOffset"] != float64(2) || page["candidates"].([]any)[0].(map[string]any)["path"] != "second.py" {
		t.Fatalf("bad list page: %v", page)
	}
	for _, args := range []map[string]any{{"runId": "foreign"}, {"runId": "foreign", "path": "secret"}, {"source": "revoked-reference"}, {"runId": old.ID, "path": "../draft.py"}} {
		if text, err := a.readProposal(current, args); err == nil || strings.Contains(text, "other-chat") {
			t.Fatalf("scope escape: %v %q %v", args, text, err)
		}
	}
	old.WorkspaceRev++
	if _, err := a.readProposal(current, map[string]any{"runId": old.ID, "path": "draft.py"}); err == nil {
		t.Fatal("read a candidate from another workspace revision")
	}
	current.WorkspaceID = "different"
	if _, err := a.readProposal(current, map[string]any{}); err == nil {
		t.Fatal("workspace switch accepted")
	}
}
