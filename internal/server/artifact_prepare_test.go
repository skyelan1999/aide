package server

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func artifactPrepareFixture(t *testing.T) (*App, *Task) {
	t.Helper()
	a := testApp(t)
	task := &Task{ID: newID(), Mode: "chat", Prompt: "Export STEP", WorkspaceID: a.wsID(), WorkspaceRev: a.wsRevision, WorkspaceMode: "local",
		DeliveryBaseline: &TaskDeliveryBaseline{WorkspaceID: a.wsID(), Revision: a.wsRevision, Files: map[string]deliveryFileBefore{}, Directories: map[string]bool{".": true}, Scanned: map[string]bool{}}}
	registerHarnessFixtureTask(t, a, task)
	return a, task
}
func prepareArtifactPaths(t *testing.T, a *App, task *Task, paths ...string) {
	t.Helper()
	items := []any{}
	for _, p := range paths {
		items = append(items, p)
	}
	if _, err := a.prepareArtifacts(context.Background(), task, map[string]any{"paths": items}); err != nil {
		t.Fatal(err)
	}
}
func TestPrepareArtifactsPartialWorkspaceAndResumeEvidence(t *testing.T) {
	a, task := artifactPrepareFixture(t)
	p := "deep/output.step"
	if err := os.MkdirAll(filepath.Join(a.workPath, "deep"), 0700); err != nil {
		t.Fatal(err)
	}
	prepareArtifactPaths(t, a, task, p)
	if _, err := os.Stat(filepath.Join(a.workPath, p)); !os.IsNotExist(err) {
		t.Fatal("prepare wrote an artifact")
	}
	if len(task.readSnapshots) != 0 || task.DeliveryBaseline.Prepared[p].Existed {
		t.Fatal("wrong baseline boundary")
	}
	if err := putText(a.workspace, p, []byte("ISO-10303-21; new fixture")); err != nil {
		t.Fatal(err)
	}
	if err := a.checkTaskDelivery(task, "[STEP]("+p+")"); err != nil {
		t.Fatal("targeted before-state lost valid new output", err)
	}
	encoded, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	var resumed Task
	if err := json.Unmarshal(encoded, &resumed); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	d := a.inspectTaskDeliveryLocked(&resumed, "[STEP]("+p+")")
	a.mu.Unlock()
	if d == nil || d.State != "delivered" {
		t.Fatal("resume lost prepared provenance", d)
	}
}
func TestPrepareArtifactsOldFileAndTouchCannotClaimNewDelivery(t *testing.T) {
	a, task := artifactPrepareFixture(t)
	p := "old.step"
	old := []byte("ISO-10303-21; old1")
	if err := putText(a.workspace, p, old); err != nil {
		t.Fatal(err)
	}
	prepareArtifactPaths(t, a, task, p)
	if got := task.DeliveryBaseline.Prepared[p]; !got.Existed || got.SHA256 != hash(old) {
		t.Fatal(got)
	}
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(a.workPath, p), future, future); err != nil {
		t.Fatal(err)
	}
	if err := a.checkTaskDelivery(task, "[STEP]("+p+")"); !errors.Is(err, errDeliveryIncomplete) {
		t.Fatal("unchanged old artifact accepted", err)
	}
	if err := putText(a.workspace, p, []byte("ISO-10303-21; new2")); err != nil {
		t.Fatal(err)
	}
	prepareArtifactPaths(t, a, task, p) // Repeated observation must retain the first state.
	if task.DeliveryBaseline.Prepared[p].SHA256 != hash(old) {
		t.Fatal("idempotent prepare overwrote earliest hash")
	}
	if err := a.checkTaskDelivery(task, "[STEP]("+p+")"); err != nil {
		t.Fatal("actual changed bytes rejected", err)
	}
}
func TestPrepareArtifactsDoesNotOverrideKnownUnchangedOriginal(t *testing.T) {
	a, task := artifactPrepareFixture(t)
	p := "known.step"
	raw := []byte("ISO-10303-21; known")
	task.DeliveryBaseline.Files[p] = deliveryFileBefore{Size: int64(len(raw)), SHA256: hash(raw)}
	prepareArtifactPaths(t, a, task, p) // Missing now does not erase known pre-task existence.
	if err := putText(a.workspace, p, raw); err != nil {
		t.Fatal(err)
	}
	if err := a.checkTaskDelivery(task, "[STEP]("+p+")"); !errors.Is(err, errDeliveryIncomplete) {
		t.Fatal("deleted/recreated unchanged original bypassed baseline", err)
	}
}
func TestPrepareArtifactsPathScopeAndAllOrNothing(t *testing.T) {
	a, task := artifactPrepareFixture(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(a.workPath, "alias")); err != nil {
		t.Fatal(err)
	}
	for _, paths := range [][]any{{"ok.step", "../bad.step"}, {"alias/new.step"}, {"secret.env"}, {"x.step", "x.step"}, {}} {
		if _, err := a.prepareArtifacts(context.Background(), task, map[string]any{"paths": paths}); err == nil {
			t.Fatal("accepted invalid path batch", paths)
		}
		if len(task.DeliveryBaseline.Prepared) != 0 {
			t.Fatal("partial preparation committed")
		}
	}
	task.DeliveryBaseline.Excluded = []string{"private"}
	if _, err := a.prepareArtifacts(context.Background(), task, map[string]any{"paths": []any{"private/new.step"}}); err == nil {
		t.Fatal("excluded path prepared")
	}
	task.WorkspaceRev++
	if _, err := a.prepareArtifacts(context.Background(), task, map[string]any{"paths": []any{"new.step"}}); err == nil {
		t.Fatal("stale workspace prepared")
	}
	task.WorkspaceRev = a.wsRevision
	task.WorkspaceMode = "ssh"
	if _, err := a.prepareArtifacts(context.Background(), task, map[string]any{"paths": []any{"new.step"}}); err == nil {
		t.Fatal("SSH silently used local proof")
	}
}
func TestPrepareArtifactsSaveFailureCannotCreateEvidence(t *testing.T) {
	a, task := artifactPrepareFixture(t)
	original := task.DeliveryBaseline
	a.mu.Lock()
	owner := a.approvalSessionLocked(task)
	p := SessionPath(a.dataPath, owner.ID, sessionBucketFor(owner))
	a.mu.Unlock()
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(p, 0700); err != nil {
		t.Fatal(err)
	}
	_, err := a.prepareArtifacts(context.Background(), task, map[string]any{"paths": []any{"new.step"}})
	if err == nil || !strings.Contains(err.Error(), "not saved") {
		t.Fatal("missing persist failure", err)
	}
	if task.DeliveryBaseline != original || len(original.Prepared) != 0 {
		t.Fatal("failed persistence granted output proof")
	}
}
func TestPrepareArtifactsDenyAndObservationLimits(t *testing.T) {
	a, task := artifactPrepareFixture(t)
	a.settings.DisabledTools = append(a.settings.DisabledTools, "prepare_artifacts")
	a.executeToolCall(context.Background(), testToolCall("prepare_artifacts", `{"paths":["new.step"]}`), task, nil)
	if len(task.DeliveryBaseline.Prepared) != 0 {
		t.Fatal("disabled tool recorded proof")
	}
	paths := []any{}
	for i := 0; i < 17; i++ {
		paths = append(paths, "new.step")
	}
	if _, err := a.prepareArtifacts(context.Background(), task, map[string]any{"paths": paths}); err == nil {
		t.Fatal("unbounded batch")
	}
	task.DeliveryBaseline.Files["unhashed.step"] = deliveryFileBefore{Size: -1}
	if _, err := a.prepareArtifacts(context.Background(), task, map[string]any{"paths": []any{"unhashed.step"}}); err == nil {
		t.Fatal("unhashed original lost conservative guard")
	}
}

func TestPrepareArtifactsConcurrentObservationsKeepBothPaths(t *testing.T) {
	a, task := artifactPrepareFixture(t)
	var group sync.WaitGroup
	errors := make(chan error, 2)
	for _, p := range []string{"part-a.step", "part-b.step"} {
		group.Add(1)
		go func(path string) {
			defer group.Done()
			_, err := a.prepareArtifacts(context.Background(), task, map[string]any{"paths": []any{path}})
			errors <- err
		}(p)
	}
	group.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(task.DeliveryBaseline.Prepared) != 2 {
		t.Fatal("lost concurrent before-state evidence")
	}
	a.mu.Lock()
	owner := a.approvalSessionLocked(task)
	file := SessionPath(a.dataPath, owner.ID, sessionBucketFor(owner))
	a.mu.Unlock()
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var session Session
	if err := json.Unmarshal(raw, &session); err != nil {
		t.Fatal(err)
	}
	if len(session.Runs[0].DeliveryBaseline.Prepared) != 2 {
		t.Fatal("concurrent evidence not persisted")
	}
}
