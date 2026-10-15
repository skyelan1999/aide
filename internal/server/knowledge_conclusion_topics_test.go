package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf16"
)

func knowledgeTopicFixture(t *testing.T) (*App, knowledgeTimeStore, string) {
	t.Helper()
	a := testApp(t)
	workspace, epoch := a.knowledgeUpdatesWorkspace()
	session, _ := conclusionAISession()
	a.mu.Lock()
	session.Runs[0].WorkspaceID = a.wsID()
	session.Messages[1].Content = "答复甲😀，这是没有结论标题的原话。"
	a.sessions[session.ID] = session
	a.mu.Unlock()
	for i, text := range []string{"# 报告\n## 结论\n原话甲😀，[依据](basis.txt)。", "# 报告\n## 最终结论\n原话乙😀，[依据](basis.txt)。"} {
		if i == 1 {
			a.mu.Lock()
			r := &Task{ID: "r2", Prompt: "另一个实际问题", WorkspaceID: a.wsID(), Status: "completed", Model: "fixture-model"}
			session.Runs = append(session.Runs, r)
			session.Messages = append(session.Messages, Message{Role: "user", SourceRunID: r.ID, Content: r.Prompt}, Message{Role: "assistant", SourceRunID: r.ID, Content: "答复乙😀，这是另一问题的无标题原话。"})
			a.mu.Unlock()
		}
		if err := os.WriteFile(filepath.Join(a.workspace.Name(), "topic.md"), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(a.workspace.Name(), "basis.txt"), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		g := a.knowledgeSnapshotModeCached(context.Background(), false, nil)
		if err := a.recordKnowledgeTime(g, false, epoch); err != nil {
			t.Fatal(err)
		}
	}
	history, err := loadKnowledgeTime(knowledgeTimeFile(a.dataPath, workspace, false), workspace, false, knowledgeTimeKey(a.token))
	if err != nil || len(history.Snapshots) != 2 {
		t.Fatal(err, len(history.Snapshots))
	}
	return a, history, workspace
}
func knowledgeTopicPick(t *testing.T, s knowledgeTimeSnapshot, kind string) knowledgeConclusionPick {
	t.Helper()
	candidates, _ := knowledgeConclusionCandidates(s, "")
	for _, c := range candidates {
		if c.SourceKind == kind && (c.Path == "topic.md" || c.Session != "") {
			return knowledgeConclusionPick{Snapshot: s.ID, Node: c.Node, Candidate: c.Token, End: len(utf16.Encode([]rune(c.Quote.Text)))}
		}
	}
	t.Fatal("candidate missing", candidates)
	return knowledgeConclusionPick{}
}
func TestKnowledgeConclusionSelectionUTF16AndRetitles(t *testing.T) {
	for _, x := range []struct {
		start, end int
		want       string
		ok         bool
	}{{0, 1, "甲", true}, {1, 3, "😀", true}, {3, 4, "乙", true}, {1, 2, "", false}, {2, 3, "", false}, {0, 5, "", false}, {-1, 1, "", false}} {
		got, ok := knowledgeConclusionUTF16("甲😀乙", x.start, x.end)
		if got != x.want || ok != x.ok {
			t.Fatal(x, got, ok)
		}
	}
	a, h, workspace := knowledgeTopicFixture(t)
	before, after := h.Snapshots[0], h.Snapshots[1]
	bp, ap := knowledgeTopicPick(t, before, "document"), knowledgeTopicPick(t, after, "document")
	bm, err := knowledgeConclusionSelect(before, bp, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	am, err := knowledgeConclusionSelect(after, ap, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	store := knowledgeConclusionTopics{Version: 1, Workspace: workspace, Topics: []knowledgeConclusionTopic{{ID: strings.Repeat("a", 32), Label: "同一主题", Members: []knowledgeConclusionSelection{bm, am}}}}
	out := compareKnowledgeTime(before, after).Conclusions
	appendKnowledgeManualTopics(&out, before, after, store)
	if len(out.Manual) != 1 || !out.Manual[0].TextChanged || !out.Manual[0].HeadingChanged || out.Manual[0].Alignment != "explicit_user" || len(out.Manual[0].Evidence) != 1 || !out.Manual[0].Evidence[0].ContentChanged {
		t.Fatalf("manual retitle alignment missing: %+v", out.Manual)
	}
	for _, c := range out.Changes {
		if c.Change.Kind == "changed" {
			t.Fatal("automatic heading inference changed", c)
		}
	}
	if a == nil {
		t.Fatal("nil fixture")
	}
	bad := bp
	bad.Candidate = strings.Repeat("0", 64)
	if _, err := knowledgeConclusionSelect(before, bad, "fixture"); err == nil {
		t.Fatal("forged candidate accepted")
	}
	if _, err := knowledgeConclusionSelect(before, bp, "fixture", true); err == nil {
		t.Fatal("candidate token crossed code view despite identical snapshot")
	}
	changed := before
	changed.Graph.Nodes = append([]knowledgeNode(nil), before.Graph.Nodes...)
	for i := range changed.Graph.Nodes {
		if changed.Graph.Nodes[i].ID == bp.Node {
			changed.Graph.Nodes[i].Text += "后续"
		}
	}
	if knowledgeTopicSelectionCurrent(changed, bm) {
		t.Fatal("changed source anchor accepted")
	}
}
func TestKnowledgeConclusionTopicsHTTPPersistenceAndConflicts(t *testing.T) {
	a, h, workspace := knowledgeTopicFixture(t)
	bp, ap := knowledgeTopicPick(t, h.Snapshots[0], "document"), knowledgeTopicPick(t, h.Snapshots[1], "document")
	query := "/api/knowledge-map/timeline/topics?workspace=" + workspace
	get := query + "&before=" + bp.Snapshot + "&after=" + ap.Snapshot
	w := request(a, "GET", get, nil)
	requireStatus(t, w, 200)
	requireStatus(t, request(a, "HEAD", get, nil), 200)
	body := map[string]any{"expected": 0, "action": "upsert", "label": "手动方案主题", "before": bp, "after": ap}
	w = request(a, "PUT", query, body)
	requireStatus(t, w, 200)
	file := knowledgeTopicFile(a.dataPath, workspace, false)
	raw, err := os.ReadFile(file)
	if err != nil || bytes.Contains(raw, []byte("手动方案")) || bytes.Contains(raw, []byte("原话")) {
		t.Fatal("plaintext topic storage", err)
	}
	info, err := os.Stat(file)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("topic permissions", err)
	}
	loaded, err := loadKnowledgeTopics(file, workspace, false, knowledgeTopicKey(a.token))
	if err != nil || loaded.Revision != 1 || len(loaded.Topics) != 1 {
		t.Fatal(err, loaded)
	}
	if _, err := loadKnowledgeTopics(file, workspace, true, knowledgeTopicKey(a.token)); err == nil {
		t.Fatal("view identity mismatch accepted")
	}
	if _, err := loadKnowledgeTopics(file, workspace, false, knowledgeTopicKey("wrong")); err == nil {
		t.Fatal("wrong key accepted")
	}
	requireStatus(t, request(a, "PUT", query, body), 409)
	w = request(a, "POST", "/api/knowledge-map/timeline/compare?workspace="+workspace, map[string]string{"before": bp.Snapshot, "after": ap.Snapshot})
	requireStatus(t, w, 200)
	var result knowledgeTimeComparison
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Conclusions.Manual) != 1 || !result.Conclusions.Manual[0].HeadingChanged || result.Conclusions.Manual[0].Before.Candidate.Quote.Text == result.Conclusions.Manual[0].After.Candidate.Quote.Text {
		t.Fatal(result.Conclusions.Manual)
	}
	requireStatus(t, request(a, "PUT", query+"&code=1", body), 404) // observations do not belong to the other view
	requireStatus(t, request(a, "GET", get+"&code=1", nil), 404)
	requireStatus(t, request(a, "PUT", "/api/knowledge-map/timeline/topics?workspace=foreign", body), 409)
	unauth := httptest.NewRecorder()
	a.Handler().ServeHTTP(unauth, httptest.NewRequest("GET", get, nil))
	requireStatus(t, unauth, 401)
	// Exactly one of two same-revision writers wins; the loser preserves the file.
	body["expected"] = 1
	body["id"] = loaded.Topics[0].ID
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); codes <- request(a, "PUT", query, body).Code }()
	}
	wg.Wait()
	close(codes)
	seen := map[int]int{}
	for code := range codes {
		seen[code]++
	}
	if seen[200] != 1 || seen[409] != 1 {
		t.Fatal(seen)
	}
	// Disable retains the captured original and allows a later explicit reactivation.
	w = request(a, "PUT", query, map[string]any{"expected": 2, "action": "disable", "id": loaded.Topics[0].ID})
	requireStatus(t, w, 200)
	current, err := loadKnowledgeTopics(file, workspace, false, knowledgeTopicKey(a.token))
	if err != nil || !current.Topics[0].Disabled || len(current.Topics[0].Members) != 2 {
		t.Fatal(err, current)
	}
	out := compareKnowledgeTime(h.Snapshots[0], h.Snapshots[1]).Conclusions
	appendKnowledgeManualTopics(&out, h.Snapshots[0], h.Snapshots[1], current)
	if len(out.Manual) != 0 {
		t.Fatal("disabled topic still compared", out.Manual)
	}
	// Corruption does not erase notes or block ordinary node/quote comparison.
	if err := os.WriteFile(file, []byte("broken encrypted record"), 0600); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, "GET", get, nil), 500)
	w = request(a, "POST", "/api/knowledge-map/timeline/compare?workspace="+workspace, map[string]string{"before": bp.Snapshot, "after": ap.Snapshot})
	requireStatus(t, w, 200)
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Conclusions.ManualState != "unavailable" {
		t.Fatal(err, result.Conclusions.ManualState)
	}
	before, _ := os.ReadFile(file)
	requireStatus(t, request(a, "PUT", query, body), 500)
	after, _ := os.ReadFile(file)
	if !bytes.Equal(before, after) {
		t.Fatal("corrupt notes overwritten")
	}
}
func TestKnowledgeConclusionTopicsLockCancelAndSymlinks(t *testing.T) {
	a, h, workspace := knowledgeTopicFixture(t)
	bp, ap := knowledgeTopicPick(t, h.Snapshots[0], "document"), knowledgeTopicPick(t, h.Snapshots[1], "document")
	query := "/api/knowledge-map/timeline/topics?workspace=" + workspace
	body := map[string]any{"expected": 0, "action": "upsert", "label": "主题", "before": bp, "after": ap}
	raw, _ := json.Marshal(body)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest("PUT", query, bytes.NewReader(raw)).WithContext(ctx)
	r.Header.Set("Authorization", "Bearer "+a.token)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	requireStatus(t, w, 408)
	file := knowledgeTopicFile(a.dataPath, workspace, false)
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("cancelled write created notes")
	}
	a.mu.Lock()
	a.settings.UserPasswordHash = "configured"
	a.mu.Unlock()
	if _, err := a.writeLockState(true); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, "PUT", query, body), 423)
	a.mu.Lock()
	a.settings.UserPasswordHash = ""
	a.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "protected")
	if err := os.WriteFile(target, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, file); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, "PUT", query, body), 500)
	protected, _ := os.ReadFile(target)
	if string(protected) != "unchanged" {
		t.Fatal("symlink target changed")
	}
}

func TestKnowledgeConclusionTopicsSelectedReplyExpiryAndCapacity(t *testing.T) {
	a, h, workspace := knowledgeTopicFixture(t)
	find := func(s knowledgeTimeSnapshot, run string) knowledgeConclusionPick {
		candidates, _ := knowledgeConclusionCandidates(s, "")
		for _, c := range candidates {
			if c.SourceKind == "assistant_reply" && c.AI.Run == run {
				return knowledgeConclusionPick{Snapshot: s.ID, Node: c.Node, Candidate: c.Token, End: 5}
			}
		}
		t.Fatal("unheaded reply missing")
		return knowledgeConclusionPick{}
	}
	bp, ap := find(h.Snapshots[0], "r1"), find(h.Snapshots[1], "r2")
	query := "/api/knowledge-map/timeline/topics?workspace=" + workspace
	requireStatus(t, request(a, "PUT", query, map[string]any{"expected": 0, "action": "upsert", "label": "无标题主题", "before": bp, "after": ap}), 200)
	file := knowledgeTopicFile(a.dataPath, workspace, false)
	store, err := loadKnowledgeTopics(file, workspace, false, knowledgeTopicKey(a.token))
	if err != nil {
		t.Fatal(err)
	}
	before, after := store.Topics[0].Members[0], store.Topics[0].Members[1]
	if before.Candidate.Quote.Text != "答复甲😀" || after.Candidate.Quote.Text != "答复乙😀" || before.Candidate.AI.QuestionHash == after.Candidate.AI.QuestionHash {
		t.Fatal("selected reply/original question mismatch", store)
	}
	out := compareKnowledgeTime(h.Snapshots[0], h.Snapshots[1]).Conclusions
	appendKnowledgeManualTopics(&out, h.Snapshots[0], h.Snapshots[1], store)
	if len(out.Manual) != 1 || !out.Manual[0].TextChanged || !out.Manual[0].SourceChanged || out.Manual[0].Before.Candidate.SourceKind != "assistant_reply" {
		t.Fatal(out.Manual)
	}
	for _, c := range out.Changes {
		if c.Change.Kind == "reply_changed" {
			t.Fatal("different questions were automatically aligned", c)
		}
	}
	// Retention removes the source observation, never the operator's selected quote.
	g := h.Snapshots[1].Graph
	g.Nodes = append([]knowledgeNode(nil), g.Nodes...)
	g.Nodes[0].Modified = "third-observation"
	updated, _, err := appendKnowledgeTimeAI(h, g, h.Snapshots[1].AI, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	updated.Snapshots = updated.Snapshots[1:]
	updated.Dropped++
	if err := saveKnowledgeTime(knowledgeTimeFile(a.dataPath, workspace, false), updated, knowledgeTimeKey(a.token)); err != nil {
		t.Fatal(err)
	}
	w := request(a, "GET", query+"&before="+updated.Snapshots[0].ID+"&after="+updated.Snapshots[1].ID, nil)
	requireStatus(t, w, 200)
	var view struct {
		Topics []knowledgeTopicView `json:"topics"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if len(view.Topics) != 1 || view.Topics[0].ExpiredMembers != 1 || len(view.Topics[0].Archived) != 1 || view.Topics[0].Archived[0].Candidate.Quote.Text != "答复甲😀" {
		t.Fatal(view)
	}
	out = compareKnowledgeTime(updated.Snapshots[0], updated.Snapshots[1]).Conclusions
	appendKnowledgeManualTopics(&out, updated.Snapshots[0], updated.Snapshots[1], store)
	if len(out.Manual) != 1 || out.Manual[0].State != "not_aligned_for_pair" || out.Manual[0].TextChanged {
		t.Fatal("expired observation inferred a retraction", out.Manual)
	}
	// A failed capacity write cannot replace the prior encrypted notes.
	original, _ := os.ReadFile(file)
	large := store
	large.Topics = nil
	for i := 0; i < 32; i++ {
		topic := store.Topics[0]
		topic.ID = fmt.Sprintf("%032x", i+1)
		topic.Members = nil
		for j := 0; j < 32; j++ {
			m := before
			m.Pick.Snapshot = fmt.Sprintf("H%032x", j+1)
			m.Pick.Start = 0
			m.Pick.End = 4096
			m.Candidate.Quote.Text = strings.Repeat("x", 4096)
			m.Candidate.Quote.TextHash = hash([]byte(m.Candidate.Quote.Text))
			topic.Members = append(topic.Members, m)
		}
		large.Topics = append(large.Topics, topic)
	}
	if err := saveKnowledgeTopics(file, large, knowledgeTopicKey(a.token)); err == nil {
		t.Fatal("oversized notes accepted")
	}
	retained, _ := os.ReadFile(file)
	if !bytes.Equal(original, retained) {
		t.Fatal("capacity rejection changed prior notes")
	}
	if err := os.RemoveAll(filepath.Join(a.dataPath, "cache")); err != nil {
		t.Fatal(err)
	}
	retained, err = os.ReadFile(file)
	if err != nil || !bytes.Equal(original, retained) {
		t.Fatal("rebuildable cache cleanup removed operator notes", err)
	}
}
