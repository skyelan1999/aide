package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestProjectMessagesScopeDateAndProvenance(t *testing.T) {
	a := testApp(t)
	zone := time.FixedZone("Asia/Shanghai", 8*3600)
	start, _ := time.ParseInLocation("2006-01-02", "2026-10-24", zone)
	run := func(id, prompt, at, ws string) *Task {
		return &Task{ID: id, Prompt: prompt, Created: at, WorkspaceID: ws}
	}
	a.sessions = map[string]*Session{"s": {ID: "s", Number: 7, Title: "Project discussion", Runs: []*Task{run("yes", "[project-name:Project X] progress", "2026-10-23T16:00:00Z", a.wsID()), run("old", "[project-name:Project X] progress", "2026-10-23T15:59:59Z", a.wsID()), run("foreign", "[project-name:Project X] progress", "2026-10-24T01:00:00Z", "other"), run("unrelated", "Other work", "2026-10-24T01:00:00Z", a.wsID())}, Messages: []Message{{Role: "user", SourceRunID: "yes", Content: "Project X progress"}, {Role: "assistant", SourceRunID: "yes", Content: "Confirmed action"}, {Role: "tool", SourceRunID: "yes", Content: "hidden tool data"}, {Role: "assistant", Content: "undated"}, {Role: "assistant", SourceRunID: "old", CreatedAt: "2026-10-24T01:00:00Z", Content: "Answer crossed midnight"}, {Role: "user", SourceRunID: "old", Content: "outside"}, {Role: "user", SourceRunID: "foreign", Content: "foreign secret"}, {Role: "user", SourceRunID: "unrelated", Content: "unrelated secret"}}}}
	before, _ := json.Marshal(a.sessions)
	rows, omitted := a.projectMessagesLocked("Project X", "", "", start, start.AddDate(0, 0, 1))
	if len(rows) != 3 || omitted != 0 {
		t.Fatalf("rows=%+v omitted=%d", rows, omitted)
	}
	for _, r := range rows {
		if (r.RunID != "yes" && r.RunID != "old") || r.Day != "2026-10-24" || r.Match != "projectName" {
			t.Fatal(r)
		}
	}
	after, _ := json.Marshal(a.sessions)
	if string(before) != string(after) {
		t.Fatal("read extraction changed sessions")
	}
	a.sessions["s"].Kind = assistantSessionKind
	rows, _ = a.projectMessagesLocked("Project X", "", "", start, start.AddDate(0, 0, 1))
	if len(rows) != 0 {
		t.Fatal("private assistant session leaked")
	}
}

func TestProjectMessagesBridgeDeclarationReplayAndValidation(t *testing.T) {
	a, key := bridgeFixture(t)
	call := func(id string, args map[string]any) int {
		return request(a, "POST", "/api/plugins/workbench-fixture/workbench/"+key+"/call", map[string]any{"protocol": 1, "requestId": id, "method": "project.messages", "arguments": args}).Code
	}
	args := map[string]any{"projectName": "Project X", "fromDate": "2026-10-24", "toDate": "2026-10-24"}
	if got := call("denied", args); got != 403 {
		t.Fatal(got)
	}
	for i := range a.pluginRegistry.Plugins {
		if a.pluginRegistry.Plugins[i].ID == "workbench-fixture" {
			a.pluginRegistry.Plugins[i].Workbench.ProjectMessages = true
		}
	}
	opened := workbenchRequest(a, "POST", "/api/plugins/workbench-fixture/workbench/open", true)
	var fresh struct {
		URL    string `json:"url"`
		Bridge struct {
			Instance string `json:"instanceId"`
		} `json:"bridge"`
	}
	if err := json.Unmarshal(opened.Body.Bytes(), &fresh); err != nil {
		t.Fatal(err)
	}
	key = fresh.Bridge.Instance
	if ready := workbenchRequest(a, "GET", fresh.URL, false); ready.Code != 200 {
		t.Fatal(ready.Code, ready.Body.String())
	}
	if got := call("ok", args); got != 200 {
		t.Fatal(got)
	}
	if got := call("ok", args); got != 409 {
		t.Fatal("replay", got)
	}
	for i, change := range []map[string]any{{"fromDate": "2026-02-30"}, {"toDate": "2027-10-24"}, {"path": "../secret.pp"}, {"projectName": "x"}, {"includeAll": true}} {
		bad := map[string]any{}
		for k, v := range args {
			bad[k] = v
		}
		for k, v := range change {
			bad[k] = v
		}
		if got := call(fmt.Sprintf("bad_%d", i), bad); got != 400 {
			t.Fatal(change, got)
		}
	}
	a.pluginWorkbenchBridges[key].Expires = time.Now().Add(-time.Second)
	if got := call("expired", args); got != 403 {
		t.Fatal(got)
	}
}

func TestProjectMessagesProviderMetadataNotSent(t *testing.T) {
	raw, err := json.Marshal(outgoingMessages([]Message{{Role: "user", Content: "Hello", SourceRunID: "internal-run", CreatedAt: "2026-10-24T01:00:00Z"}}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "createdAt") || strings.Contains(string(raw), "sourceRunId") {
		t.Fatal("local metadata leaked to provider", string(raw))
	}
}

func TestProjectMessageLegacyTimeCompatibility(t *testing.T) {
	for _, stamp := range []string{`123`, `{}`, `null`, `"2026-10-24T01:00:00Z"`} {
		var m Message
		if err := json.Unmarshal([]byte(`{"role":"user","content":"legacy","createdAt":`+stamp+`}`), &m); err != nil {
			t.Fatal(stamp, err)
		}
		if m.Content != "legacy" {
			t.Fatal("content changed")
		}
	}
}
