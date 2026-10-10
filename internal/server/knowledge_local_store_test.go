package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func seedKnowledgeLocalRestart(t *testing.T, a *App) {
	t.Helper()
	p := defaultKnowledgeIndexPolicy()
	p.FileBudget = 50
	a.mu.Lock()
	file := a.knowledgeIndexPolicyPath()
	a.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		t.Fatal(err)
	}
	if err := atomicJSON(file, p); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 120; i++ {
		if err := os.WriteFile(filepath.Join(a.workPath, fmt.Sprintf("restart-%03d.txt", i)), []byte("local confidential observation"), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
func localUpdateCount(t *testing.T, a *App) int {
	t.Helper()
	w := request(a, "GET", "/api/knowledge-map/updates", nil)
	requireStatus(t, w, 200)
	var response knowledgeUpdateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	for _, warning := range response.Warnings {
		if strings.Contains(warning, "检查点") {
			t.Fatal(warning)
		}
	}
	count := 0
	for _, n := range a.knowledgeUpdates.views[0].graph.Nodes {
		if n.Root == "workspace" && n.Kind == "file" && strings.HasPrefix(n.Path, "restart-") {
			count++
		}
	}
	return count
}
func TestKnowledgeLocalRestartAPI(t *testing.T) {
	a := testApp(t)
	seedKnowledgeLocalRestart(t, a)
	before := localUpdateCount(t, a)
	if before < 1 || before >= 120 {
		t.Fatalf("not a partial first batch: %d", before)
	}
	files, err := filepath.Glob(filepath.Join(a.dataPath, "cache", "knowledge-cursors", "local-*.json"))
	if err != nil || len(files) == 0 {
		t.Fatal("missing local checkpoint", err)
	}
	for _, file := range files {
		b, _ := os.ReadFile(file)
		if bytes.Contains(b, []byte("confidential")) || bytes.Contains(b, []byte("restart-")) {
			t.Fatal("plaintext checkpoint")
		}
	}
	restarted, err := New(a.workPath, a.refPath, a.dataPath)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	after := localUpdateCount(t, restarted)
	if after <= before || after > 120 {
		t.Fatalf("did not resume: %d -> %d", before, after)
	}
	oldOrigin := ""
	for _, n := range a.knowledgeUpdates.views[0].graph.Nodes {
		if n.Root == "workspace" {
			oldOrigin = n.Origin
			break
		}
	}
	for _, n := range restarted.knowledgeUpdates.views[0].graph.Nodes {
		if n.Root == "workspace" && n.Origin == oldOrigin {
			t.Fatal("old process origin reused")
		}
	}
	// A cancelled candidate cannot write the existing committed cursor.
	previous := restarted.knowledgeUpdates.views[0].cache
	saved := map[string][]byte{}
	for _, file := range files {
		saved[file], _ = os.ReadFile(file)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	candidate := newKnowledgeScanCache(previous)
	restarted.knowledgeSnapshotModeCached(ctx, false, candidate)
	if err := restarted.knowledgeLocalCommit(ctx, candidate, restarted.wsRevision); err == nil {
		t.Fatal("cancelled checkpoint committed")
	}
	for file, before := range saved {
		after, _ := os.ReadFile(file)
		if !bytes.Equal(before, after) {
			t.Fatal("cancelled checkpoint changed")
		}
	}
	// Index policy changes start a new catalogue instead of reviving old scope.
	p := defaultKnowledgeIndexPolicy()
	p.FileBudget = 50
	p.ScopePaths = map[string][]string{"workspace": {"restart-119.txt"}}
	restarted.mu.Lock()
	policyFile := restarted.knowledgeIndexPolicyPath()
	restarted.mu.Unlock()
	if err := atomicJSON(policyFile, p); err != nil {
		t.Fatal(err)
	}
	restarted.knowledgeUpdates.views = [2]*knowledgeUpdateView{}
	if count := localUpdateCount(t, restarted); count != 1 {
		t.Fatalf("old policy leaked: %d", count)
	}
}

func TestKnowledgeLocalProcessHelper(t *testing.T) {
	raw := os.Getenv("AIDE_TEST_LOCAL_CURSOR_PROCESS")
	if raw == "" {
		t.Skip("isolated process helper")
	}
	var input knowledgeCursorProcessInput
	if err := json.Unmarshal([]byte(raw), &input); err != nil {
		t.Fatal(err)
	}
	a, err := New(input.Work, input.Reference, input.Data)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	count := localUpdateCount(t, a)
	if err := os.WriteFile(input.Receipt, []byte(fmt.Sprint(count)), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestKnowledgeLocalFreshProcess(t *testing.T) {
	a := testApp(t)
	seedKnowledgeLocalRestart(t, a)
	before := localUpdateCount(t, a)
	input := knowledgeCursorProcessInput{Work: a.workPath, Reference: a.refPath, Data: a.dataPath, Receipt: filepath.Join(t.TempDir(), "receipt")}
	raw, _ := json.Marshal(input)
	cmd := exec.Command(os.Args[0], "-test.run=^TestKnowledgeLocalProcessHelper$", "-test.count=1")
	cmd.Env = append(os.Environ(), "AIDE_TEST_LOCAL_CURSOR_PROCESS="+string(raw))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fresh process: %v %s", err, output)
	}
	b, err := os.ReadFile(input.Receipt)
	if err != nil {
		t.Fatal(err)
	}
	after := 0
	if _, err := fmt.Sscan(string(b), &after); err != nil || after <= before {
		t.Fatalf("fresh process not resumed: %d -> %s (%v)", before, b, err)
	}
}
func TestKnowledgeLocalInvalidCheckpoint(t *testing.T) {
	a := testApp(t)
	seedKnowledgeLocalRestart(t, a)
	localUpdateCount(t, a)
	checkpoint := a.knowledgeUpdates.views[0].cache.localCheckpoints[0]
	a.mu.Lock()
	identity := a.knowledgeCursorIdentityLocked(checkpoint.Area, checkpoint.Policy, false, checkpoint.Files, checkpoint.Dirs)
	a.mu.Unlock()
	d := localPayload(identity, checkpoint.View, a.knowledgeUpdates.views[0].cache.files, checkpoint.Area.location)
	key := knowledgeTimeKey("local-test")
	file := filepath.Join(t.TempDir(), "cursor.json")
	if err := saveKnowledgeLocalCursor(file, d, key); err != nil {
		t.Fatal(err)
	}
	if loaded, err := loadKnowledgeLocalCursor(file, "different identity", key); err != nil || loaded != nil {
		t.Fatal("wrong identity reused")
	}
	if _, err := loadKnowledgeLocalCursor(file, identity, knowledgeTimeKey("wrong")); err == nil {
		t.Fatal("wrong key accepted")
	}
	d.Queue = append(d.Queue, knowledgeLocalQueue{Path: "../escape"})
	d.Complete = false
	if validateKnowledgeLocalCursor(&d) == nil {
		t.Fatal("unsafe path accepted")
	}
	target := filepath.Join(t.TempDir(), "target")
	os.WriteFile(target, []byte("keep"), 0600)
	link := filepath.Join(t.TempDir(), "link")
	os.Symlink(target, link)
	if _, err := loadKnowledgeLocalCursor(link, identity, key); err == nil {
		t.Fatal("symlink read accepted")
	}
	d.Queue = nil
	d.Complete = true
	if err := saveKnowledgeLocalCursor(link, d, key); err == nil {
		t.Fatal("symlink write accepted")
	}
	if b, _ := os.ReadFile(target); string(b) != "keep" {
		t.Fatal("target changed")
	}
}

func TestKnowledgeLocalRestartAcrossCycles(t *testing.T) {
	a := testApp(t)
	seedKnowledgeLocalRestart(t, a)
	count := 0
	for i := 0; i < 12; i++ {
		if a.knowledgeUpdates.views[0] != nil {
			a.knowledgeUpdates.views[0].checked = a.knowledgeUpdates.views[0].checked.Add(-knowledgeUpdateInterval)
		}
		count = localUpdateCount(t, a)
		if count >= 120 {
			break
		}
	}
	if count != 120 {
		t.Fatalf("incomplete catalogue: %d", count)
	}
	a.knowledgeUpdates.views[0].checked = a.knowledgeUpdates.views[0].checked.Add(-knowledgeUpdateInterval)
	if count := localUpdateCount(t, a); count != 120 {
		t.Fatalf("lost nodes in next cycle: %d", count)
	}
	restarted, err := New(a.workPath, a.refPath, a.dataPath)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if count := localUpdateCount(t, restarted); count != 120 {
		t.Fatalf("older regions lost at restart: %d", count)
	}
	for _, n := range restarted.knowledgeUpdates.views[0].graph.Nodes {
		if n.Root == "workspace" && n.Kind == "file" && strings.HasPrefix(n.Path, "restart-") && n.Text != "local confidential observation" {
			t.Fatal("material not retained", n.Path)
		}
	}
	if err := os.Remove(filepath.Join(a.workPath, "restart-119.txt")); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 12; i++ {
		restarted.knowledgeUpdates.views[0].checked = restarted.knowledgeUpdates.views[0].checked.Add(-knowledgeUpdateInterval)
		localUpdateCount(t, restarted)
	}
	for _, n := range restarted.knowledgeUpdates.views[0].graph.Nodes {
		if n.Path == "restart-119.txt" {
			t.Fatal("deleted node retained after resumed cycle")
		}
	}
}
