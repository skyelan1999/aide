package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestIssue146UnchangedArtifactCannotCompleteTask(t *testing.T) {
	for _, touchAfterStart := range []bool{false, true} {
		t.Run(fmt.Sprint(touchAfterStart), func(t *testing.T) {
			a := testApp(t)
			artifact := "old-report.pdf"
			if err := putText(a.workspace, artifact, []byte("%PDF-1.4 pre-existing report")); err != nil {
				t.Fatal(err)
			}
			old := time.Now().Add(-time.Hour)
			if err := os.Chtimes(filepath.Join(a.workPath, artifact), old, old); err != nil {
				t.Fatal(err)
			}
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if touchAfterStart {
					future := time.Now().Add(time.Hour)
					if err := os.Chtimes(filepath.Join(a.workPath, artifact), future, future); err != nil {
						t.Error(err)
					}
				}
				parallelReply(w, "已导出 [下载报告]("+artifact+")")
			}))
			defer provider.Close()
			task := &Task{ID: newID(), Prompt: "导出 pdf 报告", Created: time.Now().UTC().Format(time.RFC3339Nano), Mode: "chat", Steps: []Step{{Name: "chat"}}, Steer: make(chan string, 1)}
			registerHarnessFixtureTask(t, a, task)
			_, _, err := a.toolLoop(context.Background(), Settings{BaseURL: provider.URL, Model: "fixture"}, []Message{{Role: "user", Content: task.Prompt}}, ProfileParams{}, nil, task, nil, 0)
			if !errors.Is(err, errDeliveryIncomplete) || task.Delivery == nil || task.Delivery.State != "missing" {
				t.Fatalf("unchanged old file accepted as current delivery (touch=%v): error=%v delivery=%+v", touchAfterStart, err, task.Delivery)
			}
		})
	}
}

func TestIssue146PartialDirectoryCoverageDoesNotInventFreshness(t *testing.T) {
	a := testApp(t)
	if err := putText(a.workspace, "old/report.pdf", []byte("%PDF-1.4 old")); err != nil {
		t.Fatal(err)
	}
	for i := range 600 {
		p := filepath.Join(a.workPath, "many", fmt.Sprintf("%04d-%s", i, strings.Repeat("x", 190)))
		if err := os.MkdirAll(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	task := &Task{ID: newID(), Prompt: "导出 pdf 报告"}
	registerHarnessFixtureTask(t, a, task)
	if err := a.prepareTaskDeliveryBaseline(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if task.DeliveryBaseline.Scanned["many"] {
		t.Fatal("metadata budget was not exercised")
	}
	if err := a.checkTaskDelivery(task, "[原文件](old/report.pdf)"); !errors.Is(err, errDeliveryIncomplete) {
		t.Fatal("partial scan accepted stale artifact", err)
	}
	if err := putText(a.workspace, "fresh-dir/report.pdf", []byte("%PDF-1.4 fresh")); err != nil {
		t.Fatal(err)
	}
	if err := a.checkTaskDelivery(task, "[新目录](fresh-dir/report.pdf)"); err != nil {
		t.Fatal("complete parent directory lost proof of new subtree", err)
	}
}

func TestIssue146BaselineSaveFailureStopsBeforeProvider(t *testing.T) {
	a := testApp(t)
	task := &Task{ID: newID(), Prompt: "导出 pdf", Mode: "chat", Steps: []Step{{Name: "chat"}}, Steer: make(chan string, 1)}
	registerHarnessFixtureTask(t, a, task)
	a.mu.Lock()
	owner := a.approvalSessionLocked(task)
	file := SessionPath(a.dataPath, owner.ID, sessionBucketFor(owner))
	a.mu.Unlock()
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(file, 0700); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		parallelReply(w, "should not be called")
	}))
	defer provider.Close()
	_, _, err := a.toolLoop(context.Background(), Settings{BaseURL: provider.URL, Model: "fixture"}, []Message{{Role: "user", Content: task.Prompt}}, ProfileParams{}, nil, task, nil, 0)
	if err == nil || calls.Load() != 0 || task.DeliveryBaseline != nil {
		t.Fatal("failed baseline persistence proceeded to provider", err, calls.Load())
	}
}

func TestIssue146CompletedExecutionDropsBaseline(t *testing.T) {
	a := testApp(t)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := putText(a.workspace, "fresh.pdf", []byte("%PDF-1.4 produced")); err != nil {
			t.Error(err)
		}
		parallelReply(w, "[报告](fresh.pdf)")
	}))
	defer provider.Close()
	task := &Task{ID: newID(), Prompt: "导出 pdf", Mode: "chat", Status: "running", Steps: []Step{{Name: "chat"}}, Steer: make(chan string, 1), WorkspaceID: a.wsID(), WorkspaceRev: a.wsRevision}
	registerHarnessFixtureTask(t, a, task)
	a.mu.Lock()
	owner := a.approvalSessionLocked(task)
	a.mu.Unlock()
	input := []Message{{Role: "user", Content: task.Prompt}}
	a.execute(context.Background(), owner, task, Settings{BaseURL: provider.URL, Model: "fixture"}, input, input, nil, ProfileParams{})
	if task.Status != "completed" || task.Delivery == nil || task.Delivery.State != "delivered" || task.DeliveryBaseline != nil {
		t.Fatal("completed task retained scan metadata or failed delivery", task.Status, task.Error, task.Delivery)
	}
	b, err := os.ReadFile(SessionPath(a.dataPath, owner.ID, sessionBucketFor(owner)))
	if err != nil || bytes.Contains(b, []byte(`"deliveryBaseline"`)) {
		t.Fatal("completed session persisted transient baseline", err)
	}
}

func TestIssue146NewAndChangedArtifactsComplete(t *testing.T) {
	for _, artifact := range []string{"fresh.pdf", "fresh-dir/report.pdf", "existing.pdf"} {
		t.Run(artifact, func(t *testing.T) {
			a := testApp(t)
			before := []byte("%PDF-1.4 old content")
			if artifact == "existing.pdf" {
				if err := putText(a.workspace, artifact, before); err != nil {
					t.Fatal(err)
				}
			}
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data := bytes.ReplaceAll(before, []byte("old"), []byte("new")) // Same size defeats a stat-only check.
				if err := putText(a.workspace, artifact, data); err != nil {
					t.Error(err)
				}
				parallelReply(w, "[报告]("+artifact+")")
			}))
			defer provider.Close()
			task := &Task{ID: newID(), Prompt: "导出 pdf 报告", Mode: "chat", Steps: []Step{{Name: "chat"}}, Steer: make(chan string, 1)}
			registerHarnessFixtureTask(t, a, task)
			_, _, err := a.toolLoop(context.Background(), Settings{BaseURL: provider.URL, Model: "fixture"}, []Message{{Role: "user", Content: task.Prompt}}, ProfileParams{}, nil, task, nil, 0)
			if err != nil || task.Delivery == nil || task.Delivery.State != "delivered" {
				t.Fatal(err, task.Delivery)
			}
		})
	}
}

func TestIssue146BaselinePersistsAndRejectsAliases(t *testing.T) {
	a := testApp(t)
	if err := putText(a.workspace, "old/report.pdf", []byte("%PDF-1.4 unchanged")); err != nil {
		t.Fatal(err)
	}
	task := &Task{ID: newID(), Prompt: "导出 pdf 报告"}
	registerHarnessFixtureTask(t, a, task)
	if err := a.prepareTaskDeliveryBaseline(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	var recovered Task
	if err := json.Unmarshal(encoded, &recovered); err != nil {
		t.Fatal(err)
	}
	if err := a.checkTaskDelivery(&recovered, "[旧报告](old/report.pdf)"); !errors.Is(err, errDeliveryIncomplete) {
		t.Fatal("persisted baseline accepted unchanged file", err)
	}
	if err := os.Symlink("old", filepath.Join(a.workPath, "new-alias")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	if err := a.checkTaskDelivery(&recovered, "[别名](new-alias/report.pdf)"); !errors.Is(err, errDeliveryIncomplete) {
		t.Fatal("new directory alias accepted old file", err)
	}
	if err := putText(a.workspace, "fresh.pdf", []byte("%PDF-1.4 created after snapshot")); err != nil {
		t.Fatal(err)
	}
	if err := a.checkTaskDelivery(&recovered, "[新报告](fresh.pdf)"); err != nil {
		t.Fatal("persisted baseline lost proof of absence", err)
	}
	a.mu.Lock()
	a.wsRevision++
	a.mu.Unlock()
	if err := a.checkTaskDelivery(&recovered, "[旧命名空间](fresh.pdf)"); !errors.Is(err, errDeliveryIncomplete) {
		t.Fatal("baseline accepted a changed workspace revision", err)
	}
}

func TestIssue146NativeResumeAndAppliedWritesAreEvidence(t *testing.T) {
	a := testApp(t)
	asset := "generated/speech/qwen-fixture.mp3"
	if err := putText(a.workspace, asset, []byte("ID3same-task-generated-audio")); err != nil {
		t.Fatal(err)
	}
	task := &Task{Prompt: "生成MP3", WorkspaceID: a.wsID(), WorkspaceRev: a.wsRevision,
		MediaGeneration: &MediaGeneration{Kind: "speech", Asset: asset, Identity: "provider-identity", RequestHash: "request-digest"}}
	if err := a.checkTaskDelivery(task, "[音频]("+asset+")"); err != nil {
		t.Fatal("same-task native generation resume rejected", err)
	}
	if err := putText(a.workspace, "old.mp3", []byte("ID3unrelated")); err != nil {
		t.Fatal(err)
	}
	if err := a.checkTaskDelivery(task, "[其他音频](old.mp3)"); !errors.Is(err, errDeliveryIncomplete) {
		t.Fatal("unrelated asset inherited native evidence", err)
	}
	content := "%PDF-1.4 approved file"
	if err := putText(a.workspace, "approved.pdf", []byte(content)); err != nil {
		t.Fatal(err)
	}
	approved := &Task{Prompt: "导出 pdf", Files: []Change{{Path: "approved.pdf", Content: content, Applied: true}}}
	if err := a.checkTaskDelivery(approved, "[文件](approved.pdf)"); err != nil {
		t.Fatal("verified applied write rejected", err)
	}
	approved.Files[0].Content = "different"
	if err := a.checkTaskDelivery(approved, "[文件](approved.pdf)"); !errors.Is(err, errDeliveryIncomplete) {
		t.Fatal("mismatching applied content accepted", err)
	}
	approved.Files[0].Content = content
	approved.Files[0].Binding = "unrelated-document-binding"
	if err := a.checkTaskDelivery(approved, "[文件](approved.pdf)"); !errors.Is(err, errDeliveryIncomplete) {
		t.Fatal("invalid workspace binding supplied write evidence", err)
	}
}

func TestIssue146CaseAliasesAreNotNewPaths(t *testing.T) {
	a := testApp(t)
	if err := putText(a.workspace, "Original.PDF", []byte("%PDF-1.4 old")); err != nil {
		t.Fatal(err)
	}
	if _, err := a.workspace.Stat("original.pdf"); err != nil {
		t.Skip("filesystem is case sensitive; exercise on a case-insensitive volume")
	}
	task := &Task{ID: newID(), Prompt: "导出 pdf"}
	registerHarnessFixtureTask(t, a, task)
	if err := a.prepareTaskDeliveryBaseline(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if err := a.checkTaskDelivery(task, "[别名](original.pdf)"); !errors.Is(err, errDeliveryIncomplete) {
		t.Fatal("case alias was accepted as a new artifact", err)
	}
}

func TestIssue146LargeNewAndSizeChangedArtifacts(t *testing.T) {
	a := testApp(t)
	f, err := a.workspace.OpenFile("large.pdf", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(128 << 20); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	task := &Task{ID: newID(), Prompt: "导出 pdf"}
	registerHarnessFixtureTask(t, a, task)
	if err := a.prepareTaskDeliveryBaseline(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if err := a.checkTaskDelivery(task, "[未变的大文件](large.pdf)"); !errors.Is(err, errDeliveryIncomplete) {
		t.Fatal("unhashed unchanged large file accepted", err)
	}
	if err := os.Truncate(filepath.Join(a.workPath, "large.pdf"), (128<<20)+1); err != nil {
		t.Fatal(err)
	}
	if err := a.checkTaskDelivery(task, "[大小变化](large.pdf)"); err != nil {
		t.Fatal("proven size change rejected", err)
	}
	if err := os.Rename(filepath.Join(a.workPath, "large.pdf"), filepath.Join(a.workPath, "new-large.pdf")); err != nil {
		t.Fatal(err)
	}
	if err := a.checkTaskDelivery(task, "[新路径](new-large.pdf)"); err != nil {
		t.Fatal("new large artifact rejected", err)
	}
}
