package server

import (
	"encoding/json"
	"testing"
)

func TestSessionHistoryRunningMetadataAcrossPages(t *testing.T) {
	hidden := &Task{ID: "hidden", Status: "running"}
	session := &Session{ID: "s", Runs: []*Task{hidden, nil, {ID: "recent", Status: "completed"}}}
	view := makeSessionHistoryView(session, 1)
	if view.RunningRuns != 1 || len(view.Runs) != 1 || !view.HasOlder {
		t.Fatalf("hidden running task not represented: %#v", view)
	}
	if len(session.Runs) != 3 || session.Runs[0] != hidden {
		t.Fatal("history projection modified persisted session")
	}
	hidden.Status = "completed"
	view = makeSessionHistoryView(session, 1)
	if view.RunningRuns != 0 {
		t.Fatal("terminal hidden task still counts as running")
	}
	data, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result["runningRuns"] != float64(0) {
		t.Fatal("explicit zero is required to distinguish legacy partial responses")
	}
	session.Runs = nil
	if empty := makeSessionHistoryView(session, 1); empty.RunningRuns != 0 || empty.HasOlder {
		t.Fatal("empty session has active history")
	}
}
