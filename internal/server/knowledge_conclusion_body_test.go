package server

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

// Uses real indexing, encrypted history writes and disk reload. Both answers
// share the same visible graph prefix and conclusion; only the appendix differs.
func knowledgeBodyFixture(t *testing.T) (*App, knowledgeTimeStore, string) {
	t.Helper()
	a := testApp(t)
	workspace, epoch := a.knowledgeUpdatesWorkspace()
	s, _ := conclusionAISession()
	a.mu.Lock()
	s.Runs[0].WorkspaceID = a.wsID()
	a.sessions[s.ID] = s
	a.mu.Unlock()
	for _, name := range []string{"topic.md", "basis.txt"} {
		if err := os.WriteFile(filepath.Join(a.workspace.Name(), name), []byte("临时来源\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, tail := range []string{"历史尾段甲😀", "历史尾段乙😀"} {
		a.mu.Lock()
		s.Messages[1].Content = "## 结论\n原结论保持。\n\n## 附录\n" + strings.Repeat("保存当时的上下文，不读取当前来源。\n", 300) + tail + "\n[依据]: basis.txt\n"
		a.mu.Unlock()
		g := a.knowledgeSnapshotModeCached(context.Background(), false, nil)
		if err := a.recordKnowledgeTime(g, false, epoch); err != nil {
			t.Fatal(err)
		}
	}
	history, err := loadKnowledgeTime(knowledgeTimeFile(a.dataPath, workspace, false), workspace, false, knowledgeTimeKey(a.token))
	if err != nil || len(history.Snapshots) != 2 {
		t.Fatal("body-only history observation lost", err, len(history.Snapshots))
	}
	return a, history, workspace
}

func TestKnowledgeConclusionBodyIndexedHTTPAndSavedSelection(t *testing.T) {
	a, h, workspace := knowledgeBodyFixture(t)
	// Remove live inputs before asking the registered HTTP handler for history.
	a.mu.Lock()
	delete(a.sessions, "s")
	a.mu.Unlock()
	for _, name := range []string{"topic.md", "basis.txt"} {
		if err := os.Remove(filepath.Join(a.workspace.Name(), name)); err != nil {
			t.Fatal(err)
		}
	}
	query := "/api/knowledge-map/timeline/topics?workspace=" + workspace
	w := request(a, "GET", query+"&before="+h.Snapshots[0].ID+"&after="+h.Snapshots[1].ID, nil)
	requireStatus(t, w, 200)
	var data struct {
		Before []knowledgeConclusionCandidate `json:"before"`
		After  []knowledgeConclusionCandidate `json:"after"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	pick := func(s knowledgeTimeSnapshot, candidates []knowledgeConclusionCandidate, text string) knowledgeConclusionPick {
		for _, c := range candidates {
			if !strings.HasPrefix(c.Quote.Key, "saved_assistant_body:") {
				continue
			}
			if start := strings.Index(c.Quote.Text, text); start >= 0 {
				return knowledgeConclusionPick{Snapshot: s.ID, Node: c.Node, Candidate: c.Token, Start: len(utf16.Encode([]rune(c.Quote.Text[:start]))), End: len(utf16.Encode([]rune(c.Quote.Text[:start] + text)))}
			}
		}
		t.Fatal("saved tail not returned by HTTP", text)
		return knowledgeConclusionPick{}
	}
	bp, ap := pick(h.Snapshots[0], data.Before, "历史尾段甲😀"), pick(h.Snapshots[1], data.After, "历史尾段乙😀")
	requireStatus(t, request(a, "PUT", query, map[string]any{"expected": 0, "action": "upsert", "label": "附录原话对齐", "before": bp, "after": ap}), 200)
	w = request(a, "POST", "/api/knowledge-map/timeline/compare?workspace="+workspace, map[string]string{"before": bp.Snapshot, "after": ap.Snapshot})
	requireStatus(t, w, 200)
	var result knowledgeTimeComparison
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Conclusions.Manual) != 1 || !result.Conclusions.Manual[0].TextChanged || result.Conclusions.Manual[0].Before.Candidate.Quote.Text != "历史尾段甲😀" || result.Conclusions.Manual[0].After.Candidate.Quote.Text != "历史尾段乙😀" {
		t.Fatal("historical tail comparison unavailable")
	}
	if result.Before.AI != nil || result.After.AI != nil || bytes.Contains(w.Body.Bytes(), []byte(strings.Repeat("保存当时的上下文，不读取当前来源。\n", 100))) {
		t.Fatal("unselected full body leaked through compare")
	}
	notes, err := loadKnowledgeTopics(knowledgeTopicFile(a.dataPath, workspace, false), workspace, false, knowledgeTopicKey(a.token))
	if err != nil || len(notes.Topics) != 1 || !knowledgeTopicSelectionCurrent(h.Snapshots[0], notes.Topics[0].Members[0]) {
		t.Fatal("tail note disk restore failed", err)
	}
}

func TestKnowledgeConclusionBodyCaptureAndTailSelection(t *testing.T) {
	s, g := conclusionAISession()
	text := strings.Repeat("历史上下文😀\r\n", 700) + "\n## 结论\n原话。\n\n## 附录\nTAIL-ONLY😀\n[依据]: basis.txt\n"
	s.Messages[1].Content = text
	ai := captureKnowledgeAIConclusions(g, map[string]*Session{s.ID: s}, "scope")
	if err := validateKnowledgeAIHistory(ai, g); err != nil {
		t.Fatal(err)
	}
	b := ai.Records[0].Body
	if ai.Version != 2 || b.Text != text || !b.Complete || b.Hash != hash([]byte(text)) || b.SourceBytes != len(text) {
		t.Fatal("original body not retained", b)
	}
	if strings.Contains(ai.Records[0].Conclusions.Items[0].Text, "TAIL-ONLY") {
		t.Fatal("fixture tail entered conclusion")
	}
	snapshot := knowledgeTimeSnapshot{ID: "Hbody", Digest: strings.Repeat("a", 64), Graph: g, AI: ai}
	candidates, _ := knowledgeConclusionCandidates(snapshot, "S1")
	var chunks string
	var tail knowledgeConclusionCandidate
	for _, c := range candidates {
		if strings.HasPrefix(c.Quote.Key, "saved_assistant_body:") {
			chunks += c.Quote.Text
			if strings.Contains(c.Quote.Text, "TAIL-ONLY") {
				tail = c
			}
		}
	}
	if chunks != text || tail.Token == "" || tail.AI.Run != "r1" {
		t.Fatal("body chunks lost text or attribution")
	}
	start := strings.Index(tail.Quote.Text, "TAIL-ONLY")
	p := knowledgeConclusionPick{Snapshot: snapshot.ID, Node: tail.Node, Candidate: tail.Token, Start: len(utf16.Encode([]rune(tail.Quote.Text[:start]))), End: len(utf16.Encode([]rune(tail.Quote.Text[:start] + "TAIL-ONLY😀")))}
	m, err := knowledgeConclusionSelect(snapshot, p, "fixture")
	if err != nil || m.Candidate.Quote.Text != "TAIL-ONLY😀" || !knowledgeTopicSelectionCurrent(snapshot, m) {
		t.Fatal("tail selection failed", err, m)
	}
	store := knowledgeConclusionTopics{Version: 1, Workspace: g.Workspace, Topics: []knowledgeConclusionTopic{{ID: strings.Repeat("a", 32), Label: "历史尾段", Members: []knowledgeConclusionSelection{m}}}}
	if err := validateKnowledgeTopics(store, g.Workspace, false); err != nil {
		t.Fatal(err)
	}
	plain, _ := json.Marshal(g)
	if bytes.Contains(plain, []byte("TAIL-ONLY")) {
		t.Fatal("body leaked into graph")
	}
}

func TestKnowledgeConclusionBodyBudgetsIntegrityAndLegacy(t *testing.T) {
	s, g := conclusionAISession()
	s.Messages[1].Content = strings.Repeat("😀\n", 20000)
	ai := captureKnowledgeAIConclusions(g, map[string]*Session{s.ID: s}, "scope")
	b := ai.Records[0].Body
	if len(b.Text) > knowledgeConclusionScanBytes || !utf8.ValidString(b.Text) || b.Complete || b.SourceBytes != len(s.Messages[1].Content) {
		t.Fatal("body truncation hidden")
	}
	if err := validateKnowledgeAIHistory(ai, g); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*knowledgeAIConclusionRecord){
		func(r *knowledgeAIConclusionRecord) { r.Body.Hash = strings.Repeat("0", 64) },
		func(r *knowledgeAIConclusionRecord) { r.Body.Complete = true },
		func(r *knowledgeAIConclusionRecord) { r.Body.SourceBytes = 1 },
		func(r *knowledgeAIConclusionRecord) { r.Body = nil },
		func(r *knowledgeAIConclusionRecord) {
			r.ReplyExcerpt = "forged prefix"
			r.ReplyHash = hash([]byte(r.ReplyExcerpt))
		},
	} {
		bad := *ai
		bad.Records = append([]knowledgeAIConclusionRecord(nil), ai.Records...)
		copyBody := *b
		bad.Records[0].Body = &copyBody
		mutate(&bad.Records[0])
		if validateKnowledgeAIHistory(&bad, g) == nil {
			t.Fatal("invalid body accepted")
		}
	}
	legacy := *ai
	legacy.Version = 1
	legacy.Records = append([]knowledgeAIConclusionRecord(nil), ai.Records...)
	legacy.Records[0].Body = nil
	if err := validateKnowledgeAIHistory(&legacy, g); err != nil {
		t.Fatal("legacy rejected", err)
	}
	snap := knowledgeTimeSnapshot{ID: "Hlegacy", Digest: strings.Repeat("a", 64), Graph: g, AI: &legacy}
	candidates, _ := knowledgeConclusionCandidates(snap, "S1")
	for _, c := range candidates {
		if strings.HasPrefix(c.Quote.Key, "saved_assistant_body:") {
			t.Fatal("legacy was backfilled")
		}
	}
	legacy.Records[0].Body = b
	if validateKnowledgeAIHistory(&legacy, g) == nil {
		t.Fatal("legacy body silently accepted")
	}
	// All JSON control characters exercise the worst escaping expansion.
	s.Messages[1].Content = strings.Repeat("\x00", knowledgeConclusionScanBytes)
	ai = captureKnowledgeAIConclusions(g, map[string]*Session{s.ID: s}, "scope")
	if err := validateKnowledgeAIHistory(ai, g); err != nil {
		t.Fatal("valid escaped body rejected", err)
	}
}

func TestKnowledgeConclusionBodyV2UpgradeAndEncryptedRoundTrip(t *testing.T) {
	s, g := conclusionAISession()
	ai := captureKnowledgeAIConclusions(g, map[string]*Session{s.ID: s}, "scope")
	legacy := *ai
	legacy.Version = 1
	legacy.Records = append([]knowledgeAIConclusionRecord(nil), ai.Records...)
	legacy.Records[0].Body = nil
	file := filepath.Join(t.TempDir(), "history.json")
	key := knowledgeTimeKey("body-fixture")
	old, _, err := appendKnowledgeTimeAI(knowledgeTimeStore{Version: 2, Workspace: g.Workspace}, g, &legacy, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if err = saveKnowledgeTime(file, old, key); err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(file)
	loaded, err := loadKnowledgeTime(file, g.Workspace, false, key)
	if err != nil {
		t.Fatal(err)
	}
	s.Messages[1].Content += "\n## 附录\nPRIVATE-HISTORICAL-BODY😀"
	ai = captureKnowledgeAIConclusions(g, map[string]*Session{s.ID: s}, "scope")
	next, changed, err := appendKnowledgeTimeAI(loaded, g, ai, time.Unix(2, 0))
	if err != nil || !changed || next.Version != 3 {
		t.Fatal(err, changed, next.Version)
	}
	if err = saveKnowledgeTime(file, next, key); err != nil {
		t.Fatal(err)
	}
	backupPath := file + ".before-body-v3-" + hash(original)[:12] + ".json"
	backup, err := os.ReadFile(backupPath)
	if err != nil || !bytes.Equal(backup, original) {
		t.Fatal("encrypted backup not byte exact", err)
	}
	info, err := os.Stat(backupPath)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("backup mode", err)
	}
	raw, _ := os.ReadFile(file)
	if bytes.Contains(raw, []byte("PRIVATE-HISTORICAL-BODY")) {
		t.Fatal("plaintext body persisted")
	}
	reloaded, err := loadKnowledgeTime(file, g.Workspace, false, key)
	if err != nil || reloaded.Version != 3 || len(reloaded.Snapshots) != 2 || reloaded.Snapshots[0].AI.Records[0].Body != nil || reloaded.Snapshots[1].AI.Records[0].Body.Text != s.Messages[1].Content {
		t.Fatal("body round trip", err)
	}
	// A pre-body writer accepts only payload1/2 and must reject this envelope's
	// decrypted format before it has an opportunity to drop unknown body fields.
	var envelope knowledgeVectorEnvelope
	_ = json.Unmarshal(raw, &envelope)
	plain, err := openWithKey(key, envelope.Ciphertext, envelope.Nonce)
	if err != nil {
		t.Fatal(err)
	}
	defer zeroBytes(plain)
	var oldReader struct {
		Version int `json:"version"`
	}
	_ = json.Unmarshal(plain, &oldReader)
	if oldReader.Version <= 2 {
		t.Fatal("old writer would accept body history")
	}
	if err = saveKnowledgeTime(file, next, key); err == nil {
		t.Fatal("stale migration replaced current history")
	}
	current, _ := os.ReadFile(file)
	if !bytes.Equal(current, raw) {
		t.Fatal("stale migration damaged history")
	}
	// Document-only or legacy writes must never downgrade the payload.
	retained, _, err := appendKnowledgeTime(reloaded, g, time.Unix(3, 0))
	if err != nil || retained.Version != 3 {
		t.Fatal("format downgrade", err)
	}
}

func TestKnowledgeConclusionBodyDocumentTailAndStableLegacyToken(t *testing.T) {
	text := strings.Repeat("段落😀\n", 800) + "DOC-TAIL"
	n := conclusionDocument(text)
	n.Size = int64(len(text))
	n.ReadState = "ready"
	s := knowledgeTimeSnapshot{ID: "Hdoc", Digest: strings.Repeat("a", 64), Graph: knowledgeGraph{Workspace: "w", Nodes: []knowledgeNode{n}}}
	cs, _ := knowledgeConclusionCandidates(s, n.ID)
	var joined string
	found := false
	for _, c := range cs {
		if c.Quote.Key == "saved_document_excerpt" {
			q := knowledgeConclusion{Key: "saved_document_excerpt", Heading: "文档摘录（未标记结论）", HeadingLine: 1, Line: 1, EndLine: strings.Count(conclusionBytePrefix(text, knowledgeConclusionTextBytes), "\n") + 1, Text: conclusionBytePrefix(text, knowledgeConclusionTextBytes), SourceHash: hash([]byte(text)), Partial: true}
			q.TextHash = hash([]byte(q.Text))
			old := c
			old.Quote = q
			if knowledgeCandidateToken(s, old) != c.Token {
				t.Fatal("existing document token changed")
			}
			found = true
		}
		if strings.HasPrefix(c.Quote.Key, "saved_document_body:") {
			joined += c.Quote.Text
		}
	}
	if !found || joined != text {
		t.Fatal("saved document tail inaccessible")
	}
}

func TestKnowledgeConclusionBodyUpgradeFailurePreservesOriginal(t *testing.T) {
	for _, kind := range []string{"corrupt_backup", "symlink_backup", "stale_source"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			file := filepath.Join(dir, "history.json")
			original := []byte("synthetic encrypted envelope")
			if err := os.WriteFile(file, original, 0600); err != nil {
				t.Fatal(err)
			}
			expected := hash(original)
			backup := file + ".before-body-v3-" + expected[:12] + ".json"
			switch kind {
			case "corrupt_backup":
				if err := os.WriteFile(backup, []byte("wrong backup"), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink_backup":
				if err := os.Symlink(file, backup); err != nil {
					t.Fatal(err)
				}
			case "stale_source":
				expected = strings.Repeat("0", 64)
			}
			if err := backupKnowledgeTimeUpgrade(file, expected, 3); err == nil {
				t.Fatal("unsafe migration backup accepted")
			}
			raw, err := os.ReadFile(file)
			if err != nil || !bytes.Equal(raw, original) {
				t.Fatal("original damaged", err)
			}
		})
	}
}
