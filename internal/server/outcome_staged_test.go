package server

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestOutcomeSavedStageVisibleBeforeFinalContent(t *testing.T) {
	a, task := outcomeFixture(t)
	task.Status = "running"
	task.Steps = []Step{{Name: "chat", Status: "running", Reasoning: "PRIVATE_REASONING", OutputSegments: []OutputSegment{{ID: "stage-one", Round: 0, Content: "第一阶段已保存"}}}}
	w := request(a, "GET", "/api/sessions/outcome-session/runs/outcome-run/outcome", nil)
	requireStatus(t, w, 200)
	var data map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	outputs := data["outputs"].([]any)
	if len(outputs) != 1 {
		t.Fatalf("persisted stage missing while final content empty: %s", w.Body.String())
	}
	output := outputs[0].(map[string]any)
	segments, ok := output["segments"].([]any)
	if !ok || len(segments) != 1 {
		t.Fatal(output)
	}
	segment := segments[0].(map[string]any)
	if segment["id"] != "stage-one" || segment["content"] != "第一阶段已保存" || segment["round"] != float64(0) || segment["index"] != float64(1) || segment["digest"] != hash([]byte("第一阶段已保存")) {
		t.Fatal(segment)
	}
	if output["content"] != "" || output["status"] != "running" || strings.Contains(w.Body.String(), "PRIVATE_REASONING") {
		t.Fatal("stage replaced final state or leaked reasoning", output)
	}
}

func TestOutcomeStageSnapshotDetachedAndRejectsStalePages(t *testing.T) {
	a, task := outcomeFixture(t)
	task.Steps = []Step{{Name: "chat", Status: "running", OutputSegments: []OutputSegment{{ID: "one", Content: "Saved one"}}}}
	snap, _, _, err := a.outcomeTaskSnapshot("outcome-session", task.ID)
	if err != nil {
		t.Fatal(err)
	}
	task.Steps[0].OutputSegments[0].Content = "Current changed"
	if snap.Steps[0].OutputSegments[0].Content != "Saved one" {
		t.Fatal("snapshot aliases live segment")
	}
	snap.Steps[0].OutputSegments[0].Content = "Detached changed"
	if task.Steps[0].OutputSegments[0].Content != "Current changed" {
		t.Fatal("live segment aliases snapshot")
	}
	base := "/api/sessions/outcome-session/runs/outcome-run/outcome"
	w := request(a, "GET", base+"?limit=1", nil)
	requireStatus(t, w, 200)
	var data map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &data)
	token := data["snapshot"].(string)
	task.Steps[0].OutputSegments = append(task.Steps[0].OutputSegments, OutputSegment{ID: "two", Round: 1, Content: "Saved two"})
	requireStatus(t, request(a, "GET", base+"?fileOffset=1&limit=1&snapshot="+token, nil), 409)
}

func TestOutcomeStagePreviewBudgetStillBindsHiddenText(t *testing.T) {
	_, task := outcomeFixture(t)
	step := Step{Name: "chat", Status: "running"}
	for i := 0; i < 120; i++ {
		step.OutputSegments = append(step.OutputSegments, OutputSegment{ID: strings.Repeat("x", i+1), Round: i, Content: "one"})
	}
	task.Steps = []Step{step}
	read := func() map[string]any {
		return taskOutcomeProjection(task, "outcome-session", 42, "test", 0, 0, 1)["outputs"].([]map[string]any)[0]
	}
	data := read()
	raw, _ := json.Marshal(data["segments"])
	var segments []map[string]any
	_ = json.Unmarshal(raw, &segments)
	if len(segments) != 100 || segments[0]["index"] != float64(21) || segments[99]["index"] != float64(120) || data["segmentsTotal"] != 120 || data["segmentsOmitted"] != 20 {
		t.Fatal("count budget", data)
	}
	before := outcomeSnapshotDigest(task, "outcome-session", 42, "test")
	task.Steps[0].OutputSegments[0].Content = "Hidden changed"
	if outcomeSnapshotDigest(task, "outcome-session", 42, "test") == before {
		t.Fatal("omitted segment mutation did not invalidate snapshot")
	}
	for i := range task.Steps[0].OutputSegments {
		task.Steps[0].OutputSegments[i].Content = strings.Repeat("界", 9000)
	}
	data = read()
	raw, _ = json.Marshal(data["segments"])
	_ = json.Unmarshal(raw, &segments)
	total := 0
	for _, segment := range segments {
		count := utf8.RuneCountInString(segment["content"].(string))
		if count > 8000 || segment["truncated"] != true {
			t.Fatal(segment)
		}
		total += count
	}
	if len(segments) != 4 || total != 32000 || segments[len(segments)-1]["index"] != float64(120) {
		t.Fatal("text budget must retain latest stages", len(segments), total)
	}
}

func TestOutcomeStagesDoNotPromoteNonChatOrSystemText(t *testing.T) {
	_, task := outcomeFixture(t)
	task.Steps = []Step{{Name: "plan", Content: "Plan visible", OutputSegments: []OutputSegment{{Content: "HIDDEN_NONCHAT"}}}, {Name: "file_application_receipt", OutputSegments: []OutputSegment{{Content: "HIDDEN_RECEIPT"}}}}
	data := taskOutcomeProjection(task, "s", 1, "test", 0, 0, 10)
	raw, _ := json.Marshal(data)
	if strings.Contains(string(raw), "HIDDEN_") {
		t.Fatal("non-chat segments became assistant output")
	}
	task.Mode = "workflow"
	task.Steps = []Step{{Name: "chat", OutputSegments: []OutputSegment{{Content: "HIDDEN_MODE"}}}}
	raw, _ = json.Marshal(taskOutcomeProjection(task, "s", 1, "test", 0, 0, 10))
	if strings.Contains(string(raw), "HIDDEN_MODE") {
		t.Fatal("wrong-mode segment projected")
	}
}
