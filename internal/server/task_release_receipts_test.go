package server

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOutcomeReleaseAcceptsItsReadSnapshot(t *testing.T) {
	a := testApp(t)
	s := createSession(t, a)
	a.mu.Lock()
	s = a.sessions[s.ID]
	task := &Task{ID: "run", Mode: "chat", Prompt: "fixture", Status: "completed", Commands: []string{}}
	s.Runs = []*Task{task}
	a.mu.Unlock()
	base := "/api/sessions/" + s.ID + "/runs/run/outcome"
	read := func() map[string]any {
		t.Helper()
		w := request(a, "GET", base, nil)
		requireStatus(t, w, 200)
		var v map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	before := read()
	receipt := map[string]any{
		"schema": 1, "tag": "v0.1.17.0-RC2", "commit": strings.Repeat("1", 40),
		"url": "https://example.invalid/fixture", "observedAt": "2026-10-15T00:00:00Z", "deployment": "unknown",
		"artifacts": []map[string]any{{"name": "fixture.txt", "sha256": strings.Repeat("2", 64), "bytes": 1}},
	}
	requireStatus(t, request(a, "POST", base+"/releases", map[string]any{"snapshot": before["snapshot"], "receipt": receipt}), 200)
	after := read()
	if before["snapshot"] == after["snapshot"] || after["release"].(map[string]any)["state"] != "operator_recorded" {
		t.Fatal("operator import was not projected with a new snapshot")
	}
	requireStatus(t, request(a, "POST", base+"/releases", map[string]any{"snapshot": before["snapshot"], "receipt": receipt}), 409)
	requireStatus(t, request(a, "POST", base+"/releases", map[string]any{"snapshot": after["snapshot"], "receipt": receipt}), 200)
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(task.ReleaseReceipts) != 1 || task.ReleaseReceipts[0].Source != "operator_import" || task.ReleaseReceipts[0].RecordedAt == "" {
		t.Fatal("duplicate import or missing server provenance")
	}
}
