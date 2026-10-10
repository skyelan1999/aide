package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestKnowledgeTimelinePersistenceEvidence(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "history.json")
	key := knowledgeTimeKey("fixture-key")
	s, err := loadKnowledgeTime(file, "w", false, key)
	if err != nil {
		t.Fatal(err)
	}
	g := knowledgeGraph{Workspace: "w", Nodes: []knowledgeNode{{ID: "a", Text: "old claim", Origin: "transient", ContentHash: "old"}, {ID: "removed"}}, Edges: []knowledgeEdge{{From: "a", To: "removed", Kind: "call_candidate", Evidence: "AST", Confidence: "name_candidate"}}}
	s, changed, err := appendKnowledgeTime(s, g, time.Unix(1, 0))
	if err != nil || !changed {
		t.Fatal(changed, err)
	}
	if err = saveKnowledgeTime(file, s, key); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(file)
	if strings.Contains(string(raw), "old claim") {
		t.Fatal("plaintext history")
	}
	s, err = loadKnowledgeTime(file, "w", false, key)
	if err != nil {
		t.Fatal(err)
	}
	g.Nodes[0].Origin = "new-process"
	g.Sources = []knowledgeSource{{ID: "workspace", Coverage: &knowledgeCoverage{Cycle: 2}}}
	if _, changed, err = appendKnowledgeTime(s, g, time.Unix(2, 0)); err != nil || changed {
		t.Fatal("coverage/origin duplicate", changed, err)
	}
	g.Nodes = []knowledgeNode{{ID: "a", Text: "revised claim", ContentHash: "new"}, {ID: "added", Text: "supporting source"}}
	g.Edges = []knowledgeEdge{{From: "a", To: "added", Kind: "call_typed", Line: 4, Confidence: "type_binding"}}
	s, changed, err = appendKnowledgeTime(s, g, time.Unix(3, 0))
	if err != nil || !changed {
		t.Fatal(changed, err)
	}
	if err = saveKnowledgeTime(file, s, key); err != nil {
		t.Fatal(err)
	}
	s, err = loadKnowledgeTime(file, "w", false, key)
	if err != nil {
		t.Fatal(err)
	}
	c := compareKnowledgeTime(s.Snapshots[0], s.Snapshots[1])
	if len(c.Added) != 1 || len(c.Removed) != 1 || len(c.Changed) != 1 || c.Changed[0].Before.Text != "old claim" || c.Changed[0].After.Text != "revised claim" || len(c.AddedEdges) != 1 || c.AddedEdges[0].Confidence != "type_binding" || len(c.RemovedEdges) != 1 {
		t.Fatalf("%+v", c)
	}
	if _, err = loadKnowledgeTime(file, "other", false, key); err == nil {
		t.Fatal("workspace leak")
	}
	if _, err = loadKnowledgeTime(file, "w", true, key); err == nil {
		t.Fatal("mode leak")
	}
	if _, err = loadKnowledgeTime(file, "w", false, knowledgeTimeKey("wrong")); err == nil {
		t.Fatal("wrong key accepted")
	}
}

func TestKnowledgeTimelineRetentionAndCorruption(t *testing.T) {
	s := knowledgeTimeStore{Version: 1, Workspace: "w"}
	for i := 0; i < 35; i++ {
		g := knowledgeGraph{Workspace: "w", Nodes: []knowledgeNode{{ID: "a", Number: i}}}
		var err error
		s, _, err = appendKnowledgeTime(s, g, time.Unix(int64(i), 0))
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(s.Snapshots) != 32 || s.Dropped != 3 {
		t.Fatal(len(s.Snapshots), s.Dropped)
	}
	file := filepath.Join(t.TempDir(), "bad.json")
	os.WriteFile(file, []byte("corrupt"), 0600)
	if _, err := loadKnowledgeTime(file, "w", false, knowledgeTimeKey("key")); err == nil {
		t.Fatal("corruption accepted")
	}
	raw, _ := os.ReadFile(file)
	if string(raw) != "corrupt" {
		t.Fatal("history overwritten")
	}
	link := filepath.Join(t.TempDir(), "link.json")
	os.Symlink(file, link)
	if _, err := loadKnowledgeTime(link, "w", false, knowledgeTimeKey("key")); err == nil {
		t.Fatal("symlink followed")
	}
}

func TestKnowledgeTimelineHandlerIsolation(t *testing.T) {
	a := testApp(t)
	workspace, epoch := a.knowledgeUpdatesWorkspace()
	g := knowledgeGraph{Workspace: workspace, Nodes: []knowledgeNode{{ID: "a", Text: "original"}}}
	if err := a.recordKnowledgeTime(g, false, epoch); err != nil {
		t.Fatal(err)
	}
	g.Nodes[0].Text = "changed"
	if err := a.recordKnowledgeTime(g, false, epoch); err != nil {
		t.Fatal(err)
	}
	url := "/api/knowledge-map/timeline?workspace=" + workspace
	w := request(a, "GET", url, nil)
	requireStatus(t, w, 200)
	var data struct {
		Entries []struct {
			ID string `json:"id"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Entries) != 2 {
		t.Fatal(w.Body.String())
	}
	q := map[string]string{"before": data.Entries[0].ID, "after": data.Entries[1].ID}
	requireStatus(t, request(a, "POST", "/api/knowledge-map/timeline/compare?workspace="+workspace, q), 200)
	requireStatus(t, request(a, "POST", "/api/knowledge-map/timeline/compare?workspace="+workspace+"&code=1", q), 404)
	requireStatus(t, request(a, "GET", "/api/knowledge-map/timeline?workspace=other", nil), 409)
	a.mu.Lock()
	a.settings.UserPasswordHash = "configured"
	a.mu.Unlock()
	if _, err := a.writeLockState(true); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, "GET", url, nil), 423)
	requireStatus(t, request(a, "POST", "/api/knowledge-map/timeline/compare?workspace="+workspace, q), 423)
}

func TestKnowledgeTimelineCanonicalAndCapacity(t *testing.T) {
	s := knowledgeTimeStore{Version: 1, Workspace: "w"}
	e := knowledgeEdge{From: "a", To: "b", Kind: "mention"}
	g := knowledgeGraph{Workspace: "w", Nodes: []knowledgeNode{{ID: "b"}, {ID: "a"}}, Edges: []knowledgeEdge{e, e}}
	s, _, err := appendKnowledgeTime(s, g, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	g.Edges = []knowledgeEdge{e}
	g.Nodes[0], g.Nodes[1] = g.Nodes[1], g.Nodes[0]
	if _, changed, err := appendKnowledgeTime(s, g, time.Unix(2, 0)); err != nil || changed {
		t.Fatal("duplicate/order created history", changed, err)
	}
	g.Nodes[0].Text = strings.Repeat("x", knowledgeTimelineBytes)
	if _, _, err := appendKnowledgeTime(s, g, time.Unix(3, 0)); err == nil {
		t.Fatal("oversize accepted")
	}
	if len(s.Snapshots) != 1 || len(s.Snapshots[0].Graph.Nodes[0].Text) != 0 {
		t.Fatal("failed append mutated history")
	}
}
func TestKnowledgeTimelineUpdatesRestartAndEpoch(t *testing.T) {
	a := testApp(t)
	note := filepath.Join(a.workPath, "note.md")
	if err := os.WriteFile(note, []byte("first evidence"), 0600); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, "GET", "/api/knowledge-map/updates", nil), 200)
	workspace, epoch := a.knowledgeUpdatesWorkspace()
	file := knowledgeTimeFile(a.dataPath, workspace, false)
	first, err := loadKnowledgeTime(file, workspace, false, knowledgeTimeKey(a.token))
	if err != nil || len(first.Snapshots) != 1 {
		t.Fatal(first, err)
	}
	if err := os.WriteFile(note, []byte("second evidence"), 0600); err != nil {
		t.Fatal(err)
	}
	a.knowledgeUpdates.views[0].checked = time.Time{}
	requireStatus(t, request(a, "GET", "/api/knowledge-map/updates", nil), 200)
	second, err := loadKnowledgeTime(file, workspace, false, knowledgeTimeKey(a.token))
	if err != nil || len(second.Snapshots) != 2 {
		t.Fatal(second, err)
	}
	a.Close()
	restarted, err := New(a.workPath, a.refPath, a.dataPath)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	requireStatus(t, request(restarted, "GET", "/api/knowledge-map/timeline?workspace="+workspace, nil), 200)
	persisted, err := loadKnowledgeTime(file, workspace, false, knowledgeTimeKey(restarted.token))
	if err != nil || len(persisted.Snapshots) != 2 {
		t.Fatal(persisted, err)
	}
	changed := compareKnowledgeTime(persisted.Snapshots[0], persisted.Snapshots[1])
	found := false
	for _, n := range changed.Changed {
		if n.Before.Text == "first evidence" && n.After.Text == "second evidence" {
			found = true
		}
	}
	if !found {
		t.Fatal("no real file evidence change", changed)
	}
	restarted.mu.Lock()
	restarted.wsRevision = epoch + 1
	restarted.mu.Unlock()
	if err := restarted.recordKnowledgeTime(knowledgeGraph{Workspace: workspace}, false, epoch); err == nil {
		t.Fatal("stale epoch stored")
	}
	unauth := httptest.NewRecorder()
	restarted.Handler().ServeHTTP(unauth, httptest.NewRequest("GET", "/api/knowledge-map/timeline?workspace="+workspace, nil))
	requireStatus(t, unauth, 401)
}
