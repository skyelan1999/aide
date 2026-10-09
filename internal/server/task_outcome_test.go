package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func outcomeFixture(t *testing.T) (*App, *Task) {
	a := testApp(t)
	task := &Task{ID: "outcome-run", Status: "completed", Prompt: "Implement and verify", WorkspaceID: "original-workspace", Files: []Change{{Path: "first.txt", Content: "proposal", BaseHash: "before"}, {Path: "second.txt", Content: "applied", Applied: true}}, Commands: []string{"go test ./..."}, ToolUses: []ToolUse{{Tool: "read_file", Args: `{"path":"first.txt"}`, Result: strings.Repeat("returned ", 300)}, {Tool: "record_verification", Args: `{"title":"Checks","content":"A report claims success"}`, Result: "record created"}}, AgentPlan: &AgentPlan{Items: []AgentPlanItem{{Step: "Finish", Status: "completed", Evidence: []int{1}}}}, ToolExecutionIntents: []ToolExecutionIntent{{CallID: "pending", Tool: "run_shell", State: "dispatched"}}}
	a.sessions["outcome-session"] = &Session{ID: "outcome-session", Number: 42, Title: "Outcome acceptance", Runs: []*Task{task}}
	return a, task
}
func TestTaskOutcomePreservesEvidenceBoundaries(t *testing.T) {
	a, task := outcomeFixture(t)
	task.ToolExecutionIntents = append(task.ToolExecutionIntents, ToolExecutionIntent{CallID: "done", Tool: "read_file", State: "completed"})
	w := request(a, "GET", "/api/sessions/outcome-session/runs/outcome-run/outcome?limit=1", nil)
	requireStatus(t, w, 200)
	var d map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &d)
	if d["status"] != "completed" || d["workspaceId"] != "original-workspace" {
		t.Fatal(d)
	}
	if d["summary"].(map[string]any)["unknownOutcomes"] != float64(1) || len(d["unknownCalls"].([]any)) != 1 {
		t.Fatal("completed intent must not be reported unknown", d)
	}
	release := d["release"].(map[string]any)
	if release["state"] != "not_recorded" {
		t.Fatal(release)
	}
	f := d["files"].([]any)[0].(map[string]any)
	if f["state"] != "proposed" {
		t.Fatal(f)
	}
	u := d["executions"].([]any)[0].(map[string]any)
	if u["state"] != "result_recorded" || u["truncated"] != true {
		t.Fatal(u)
	}
	v := d["verification"].([]any)[0].(map[string]any)
	if v["state"] != "report_submitted" || v["evidence"] != "E0002" {
		t.Fatal(v)
	}
	page := d["page"].(map[string]any)
	if page["filesMore"] != true || page["executionsMore"] != true {
		t.Fatal(page)
	}
	if len(d["gaps"].([]any)) < 3 {
		t.Fatal("missing verification boundaries", d)
	}
	w = request(a, "GET", "/api/sessions/outcome-session/runs/outcome-run/outcome?fileOffset=1&executionOffset=1&limit=1", nil)
	requireStatus(t, w, 200)
	_ = json.Unmarshal(w.Body.Bytes(), &d)
	if d["files"].([]any)[0].(map[string]any)["state"] != "application_recorded" {
		t.Fatal(d)
	}
	if d["executions"].([]any)[0].(map[string]any)["id"] != "E0002" {
		t.Fatal(d)
	}
}
func TestTaskOutcomeEvidenceDigestAndOwnership(t *testing.T) {
	a, task := outcomeFixture(t)
	digest := outcomeDigest(task.ToolUses[0])
	url := "/api/sessions/outcome-session/runs/outcome-run/outcome/evidence/E0001?digest=" + digest
	w := request(a, "GET", url, nil)
	requireStatus(t, w, 200)
	var d map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &d)
	if d["result"] != task.ToolUses[0].Result || d["digest"] != digest {
		t.Fatal("evidence must retain original bytes")
	}
	task.ToolUses[0].Result = "changed"
	requireStatus(t, request(a, "GET", url, nil), 409)
	for _, path := range []string{"/api/sessions/other/runs/outcome-run/outcome", "/api/sessions/outcome-session/runs/other/outcome", "/api/sessions/outcome-session/runs/outcome-run/outcome/evidence/E0000"} {
		requireStatus(t, request(a, "GET", path, nil), 404)
	}
	for _, q := range []string{"limit=0", "limit=501", "fileOffset=-1", "executionOffset=wrong"} {
		requireStatus(t, request(a, "GET", "/api/sessions/outcome-session/runs/outcome-run/outcome?"+q, nil), 400)
	}
	a.sessions["outcome-session"].Deleted = true
	requireStatus(t, request(a, "GET", url, nil), 404)
	r := httptest.NewRequest(http.MethodGet, "/api/sessions/outcome-session/runs/outcome-run/outcome", nil)
	out := httptest.NewRecorder()
	a.Handler().ServeHTTP(out, r)
	requireStatus(t, out, 401)
}
func TestTaskOutcomeSnapshotIsDetached(t *testing.T) {
	a, original := outcomeFixture(t)
	snapshot, _, _, err := a.outcomeTaskSnapshot("outcome-session", "outcome-run")
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Files[0].Content = "mutated"
	snapshot.ToolUses[0].Result = "mutated"
	snapshot.AgentPlan.Items[0].Evidence[0] = 99
	if original.Files[0].Content == "mutated" || original.ToolUses[0].Result == "mutated" || original.AgentPlan.Items[0].Evidence[0] == 99 {
		t.Fatal("snapshot aliases live task")
	}
}
