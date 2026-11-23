package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A final-format gate must not prevent the ordinary application of its
// prerequisites, and application of a generator must not claim a STEP exists.
func TestArtifactPrerequisiteNormalApplicationThenPendingExecution(t *testing.T) {
	a := testApp(t)
	const source = "print('synthetic fixture only; not executed')\n"
	task := &Task{ID: newID(), Mode: "chat", Prompt: "导出 STEP 文件", MediaPrompt: "生成 STEP 制品", Status: "running",
		Steps: []Step{{Name: "chat"}}, Steer: make(chan string, 1), WorkspaceID: a.wsID(), WorkspaceRev: a.wsRevision,
		Files:     []Change{{Path: "build.py", Content: source, NoSymlinks: true}},
		ToolUses:  []ToolUse{{Tool: "write_file", Result: "candidate only"}},
		AgentPlan: &AgentPlan{Items: []AgentPlanItem{{Step: "execute after source application", Status: "pending"}}},
	}
	registerHarnessFixtureTask(t, a, task)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parallelReply(w, "生成器源码已形成提案；等待正常应用，STEP 尚未生成。")
	}))
	defer provider.Close()
	a.mu.Lock()
	owner := a.approvalSessionLocked(task)
	a.mu.Unlock()
	input := []Message{{Role: "user", Content: task.Prompt}}
	a.execute(context.Background(), owner, task, Settings{BaseURL: provider.URL, Model: "fixture"}, input, input, nil, ProfileParams{})
	if task.Status != "awaiting_approval" || task.Applied || task.Delivery == nil || task.Delivery.State != "pending_approval" {
		t.Fatalf("source handoff blocked or claimed delivery: status=%s err=%s delivery=%+v", task.Status, task.Error, task.Delivery)
	}
	if _, err := os.Stat(filepath.Join(a.workPath, "build.py")); !os.IsNotExist(err) {
		t.Fatalf("candidate wrote before approval: %v", err)
	}
	w := request(a, "POST", "/api/sessions/"+owner.ID+"/runs/"+task.ID+"/apply", map[string]any{"remember": false})
	requireStatus(t, w, 200)
	got, err := os.ReadFile(filepath.Join(a.workPath, "build.py"))
	if err != nil || string(got) != source || !task.Applied || task.Files[0].HistoryPending {
		t.Fatalf("normal application failed: %v %+v", err, task.Files)
	}
	if task.Delivery.State != "pending_execution" || len(task.Delivery.Artifacts) != 0 || task.DeliveryBaseline == nil {
		t.Fatalf("source mistaken for STEP or baseline discarded: %+v", task.Delivery)
	}
	if task.AgentPlan.Items[0].Status != "pending" {
		t.Fatal("file application completed the future execution plan")
	}
	if err := a.checkTaskDelivery(task, "[源码](build.py)"); !errors.Is(err, errDeliveryIncomplete) {
		t.Fatalf("applied source bypassed final artifact gate: %v", err)
	}
	// This tests final-format/provenance gates only, not CAD geometry validity.
	if err := putText(a.workspace, "part.step", []byte("ISO-10303-21;\nEND-ISO-10303-21;\n")); err != nil {
		t.Fatal(err)
	}
	if err := a.checkTaskDelivery(task, "[制品](part.step)"); err != nil || task.Delivery.State != "delivered" {
		t.Fatalf("actual new target-format file rejected: %v %+v", err, task.Delivery)
	}
}

func TestArtifactPrerequisiteCannotFakeEvidenceOrMediaDelivery(t *testing.T) {
	for _, files := range [][]Change{nil, {{Path: "build.py"}}, {{Path: "build.py", Content: "already applied", Applied: true}}, {{Path: "build.py", Content: "saved history pending", Applied: true, HistoryPending: true}}} {
		a := testApp(t)
		task := &Task{Prompt: "生成 STEP", Files: files}
		if err := a.checkTaskDelivery(task, "没有 STEP"); !errors.Is(err, errDeliveryIncomplete) {
			t.Fatalf("absent/empty/saved proposal bypassed artifact gate: %+v %v", files, err)
		}
	}
	a := testApp(t)
	task := &Task{Prompt: "生成图片", Files: []Change{{Path: "build.py", Content: "print(1)"}}}
	if err := a.checkTaskDelivery(task, "源码待审批"); !errors.Is(err, errDeliveryIncomplete) {
		t.Fatal("native image delivery gate was changed", err)
	}
}

func TestArtifactPrerequisiteReceiptExplainsTargetNotDelivered(t *testing.T) {
	a := testApp(t)
	source := "fixture source\n"
	task := &Task{ID: newID(), AutoReview: true, Applied: true, Status: "completed", WorkspaceID: a.wsID(), WorkspaceRev: a.wsRevision,
		Files:    []Change{{Path: "build.py", Content: source, Applied: true}},
		Delivery: &TaskDelivery{State: "pending_execution", Kind: "file", Reason: "目标制品尚未生成或核验"}}
	registerHarnessFixtureTask(t, a, task)
	if err := putText(a.workspace, "build.py", []byte(source)); err != nil {
		t.Fatal(err)
	}
	a.recordReviewedFileResult(task)
	last := task.Steps[len(task.Steps)-1].Content
	if !strings.Contains(last, "目标制品尚未生成或核验") || !strings.Contains(last, "SHA-256") {
		t.Fatalf("receipt lost applied bytes or incomplete target distinction: %s", last)
	}
}

func TestArtifactPrerequisiteMixedFilesOnlyTargetIsDelivered(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		a := testApp(t)
		files := []Change{{Path: "build.py", Content: "synthetic source\n", Applied: true}, {Path: "part.step", Content: "synthetic target-format fixture\n", Applied: true}}
		for _, f := range files {
			if err := putText(a.workspace, f.Path, []byte(f.Content)); err != nil {
				t.Fatal(err)
			}
		}
		if reverse {
			files[0], files[1] = files[1], files[0]
		}
		task := &Task{Prompt: "生成 STEP", Applied: true, WorkspaceID: a.wsID(), WorkspaceRev: a.wsRevision,
			Files: files, Delivery: &TaskDelivery{State: "pending_approval", Kind: "file"}}
		a.finalizeProposalDeliveryLocked(task)
		if task.Delivery.State != "delivered" || len(task.Delivery.Artifacts) != 1 || task.Delivery.Artifacts[0] != "part.step" {
			t.Fatalf("source file entered artifact list or target was lost: %+v", task.Delivery)
		}
	}
}
