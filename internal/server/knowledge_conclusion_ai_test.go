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
)

func conclusionAISession() (*Session, knowledgeGraph) {
	r := &Task{ID: "r1", Prompt: "同一问题", WorkspaceID: "scope", Status: "completed", Model: "fixture-model"}
	s := &Session{ID: "s", Number: 1, Title: "会话", Runs: []*Task{r}, Messages: []Message{{Role: "user", SourceRunID: r.ID, Content: r.Prompt}, {Role: "assistant", SourceRunID: r.ID, Content: "## 结论\n甲。\n"}}}
	g := knowledgeGraph{Workspace: "w", Nodes: []knowledgeNode{{ID: "S1", Kind: "session", Session: s.ID}}, Sources: []knowledgeSource{{Region: "sessions", State: "ready"}}}
	return s, g
}

func TestKnowledgeConclusionAICaptureAndAttribution(t *testing.T) {
	s, g := conclusionAISession()
	s.Compact = "## 结论\nCOMPACT-SECRET"
	s.Runs = append(s.Runs, &Task{ID: "foreign", Prompt: "别处问题", WorkspaceID: "other", Status: "completed"})
	s.Messages = append(s.Messages, Message{Role: "tool", SourceRunID: "r1", Content: "## 结论\nTOOL-SECRET"}, Message{Role: "user", SourceRunID: "r1", Content: "## 结论\nUSER-SECRET"}, Message{Role: "assistant", SourceRunID: "foreign", Content: "## 结论\nFOREIGN-SECRET"}, Message{Role: "assistant", Content: "## 结论\nLEGACY-SECRET"})
	ai := captureKnowledgeAIConclusions(g, map[string]*Session{s.ID: s}, "scope")
	if len(ai.Records) != 1 || !ai.Partial || ai.Unattributed != 1 {
		t.Fatalf("capture attribution: %+v", ai)
	}
	r := ai.Records[0]
	if r.Origin != "persisted_assistant" || r.Run != "r1" || !r.QuestionBound || r.Conclusions.Items[0].Text != "甲。" {
		t.Fatal(r)
	}
	b, _ := json.Marshal(ai)
	for _, secret := range []string{"COMPACT-SECRET", "TOOL-SECRET", "FOREIGN-SECRET", "LEGACY-SECRET", "USER-SECRET"} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("wrong role/scope entered AI capture: %s", secret)
		}
	}
	body, partial := knowledgeScopedSessionText(s, "scope")
	if !partial || strings.Contains(body, "FOREIGN-SECRET") || strings.Contains(body, "LEGACY-SECRET") || strings.Contains(body, "COMPACT-SECRET") {
		t.Fatal("map text crossed workspace/legacy boundary", body)
	}
	if err := validateKnowledgeAIHistory(ai, g); err != nil {
		t.Fatal(err)
	}
}

func TestKnowledgeConclusionAIQuestionIsolation(t *testing.T) {
	s, g := conclusionAISession()
	before := captureKnowledgeAIConclusions(g, map[string]*Session{s.ID: s}, "scope")
	r := &Task{ID: "r2", Prompt: "同一问题", WorkspaceID: "scope", Status: "completed", Model: "other-model"}
	s.Runs = append(s.Runs, r)
	s.Messages = append(s.Messages, Message{Role: "user", SourceRunID: r.ID, Content: r.Prompt}, Message{Role: "assistant", SourceRunID: r.ID, Content: "## 结论\n乙。\n"})
	after := captureKnowledgeAIConclusions(g, map[string]*Session{s.ID: s}, "scope")
	if len(after.Records) != 1 || after.Records[0].Run != "r2" {
		t.Fatal("did not keep latest answer per exact input", after)
	}
	out := compareKnowledgeTime(knowledgeTimeSnapshot{Graph: g, AI: before}, knowledgeTimeSnapshot{Graph: g, AI: after}).Conclusions
	if len(out.Changes) != 1 || out.Changes[0].Change.Kind != "changed" || out.Changes[0].BeforeAI.Run != "r1" || out.Changes[0].AfterAI.Run != "r2" || out.Changes[0].SourceKind != "assistant_conclusion" {
		t.Fatalf("same question did not compare: %+v", out)
	}
	r3 := &Task{ID: "r3", Prompt: "另一个问题", WorkspaceID: "scope", Status: "completed"}
	s.Runs = append(s.Runs, r3)
	s.Messages = append(s.Messages, Message{Role: "user", SourceRunID: r3.ID, Content: r3.Prompt}, Message{Role: "assistant", SourceRunID: r3.ID, Content: "## 结论\n丙。\n"})
	next := captureKnowledgeAIConclusions(g, map[string]*Session{s.ID: s}, "scope")
	out = compareKnowledgeTime(knowledgeTimeSnapshot{Graph: g, AI: after}, knowledgeTimeSnapshot{Graph: g, AI: next}).Conclusions
	for _, change := range out.Changes {
		if change.Change.Kind == "changed" {
			t.Fatalf("different question became changed conclusion: %+v", change)
		}
	}
	if len(out.Changes) != 1 || out.Changes[0].Change.Kind != "observed" {
		t.Fatal(out)
	}
}

func TestKnowledgeConclusionAIUnheadedAndIncomplete(t *testing.T) {
	s, g := conclusionAISession()
	s.Messages[1].Content = "没有标题的原始回答甲。"
	before := captureKnowledgeAIConclusions(g, map[string]*Session{s.ID: s}, "scope")
	s.Messages[1].Content = "没有标题的原始回答乙。"
	after := captureKnowledgeAIConclusions(g, map[string]*Session{s.ID: s}, "scope")
	out := compareKnowledgeTime(knowledgeTimeSnapshot{Graph: g, AI: before}, knowledgeTimeSnapshot{Graph: g, AI: after}).Conclusions
	if !after.Records[0].Unclassified || len(out.Changes) != 1 || out.Changes[0].Change.Kind != "reply_changed" || out.Changes[0].SourceKind != "assistant_reply" {
		t.Fatal(out)
	}
	s.Runs[0].Status = "cancelled"
	s.Messages[1].Content = "## 结论\n中断输出。"
	partial := captureKnowledgeAIConclusions(g, map[string]*Session{s.ID: s}, "scope")
	if !partial.Records[0].Conclusions.Partial {
		t.Fatal("cancelled answer became complete", partial)
	}
	out = compareKnowledgeTime(knowledgeTimeSnapshot{Graph: g}, knowledgeTimeSnapshot{Graph: g, AI: partial}).Conclusions
	if out.AIState != "legacy_capture_unknown" {
		t.Fatal(out)
	}
	for _, change := range out.Changes {
		if change.Change.Kind == "changed" || change.Change.Kind == "added" {
			t.Fatal("legacy missing capture inferred new fact", change)
		}
	}
}

func TestKnowledgeConclusionAIHistoryBeyondGraphPrefix(t *testing.T) {
	a := testApp(t)
	s, _ := conclusionAISession()
	a.mu.Lock()
	scope := a.wsID()
	s.Runs[0].WorkspaceID = scope
	s.Messages[1].Content = strings.Repeat("前言", 600) + "\n\n## 结论\nPRIVATE-TAIL-A"
	a.sessions[s.ID] = s
	a.mu.Unlock()
	workspace, epoch := a.knowledgeUpdatesWorkspace()
	g := a.knowledgeSnapshotModeCached(context.Background(), false, nil)
	b, _ := json.Marshal(g)
	if strings.Contains(string(b), "PRIVATE-TAIL-A") {
		t.Fatal("full AI data escaped through graph payload")
	}
	if err := a.recordKnowledgeTime(g, false, epoch); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	s.Messages[1].Content = strings.Repeat("前言", 600) + "\n\n## 结论\nPRIVATE-TAIL-B"
	a.mu.Unlock()
	next := a.knowledgeSnapshotModeCached(context.Background(), false, nil)
	if err := a.recordKnowledgeTime(next, false, epoch); err != nil {
		t.Fatal(err)
	}
	store, err := loadKnowledgeTime(knowledgeTimeFile(a.dataPath, workspace, false), workspace, false, knowledgeTimeKey(a.token))
	if err != nil {
		t.Fatal(err)
	}
	if len(store.Snapshots) != 2 {
		t.Fatal("AI-only change beyond800 chars lost", len(store.Snapshots))
	}
	out := compareKnowledgeTime(store.Snapshots[0], store.Snapshots[1]).Conclusions
	if len(out.Changes) != 1 || out.Changes[0].Change.Before.Text != "PRIVATE-TAIL-A" || out.Changes[0].Change.After.Text != "PRIVATE-TAIL-B" {
		t.Fatalf("AI history lost source quotes: %+v", out)
	}
	bad := *store.Snapshots[1].AI
	bad.Workspace = "other"
	if _, changed, err := appendKnowledgeTimeAI(store, next, &bad, time.Now()); err == nil || changed {
		t.Fatal("foreign AI sidecar accepted")
	}
}

func TestKnowledgeConclusionAIUpgradePreservesEncryptedV1(t *testing.T) {
	s, g := conclusionAISession()
	key := knowledgeTimeKey("fixture-key")
	file := filepath.Join(t.TempDir(), "history.json")
	old := knowledgeTimeStore{Version: 1, Workspace: g.Workspace, Snapshots: []knowledgeTimeSnapshot{}}
	old, _, err := appendKnowledgeTime(old, g, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if err := saveKnowledgeTime(file, old, key); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := loadKnowledgeTime(file, g.Workspace, false, key)
	if err != nil {
		t.Fatal(err)
	}
	ai := captureKnowledgeAIConclusions(g, map[string]*Session{s.ID: s}, "scope")
	next, changed, err := appendKnowledgeTimeAI(loaded, g, ai, time.Unix(2, 0))
	if err != nil || !changed || next.Version != 2 {
		t.Fatal(changed, err, next.Version)
	}
	if err := saveKnowledgeTime(file, next, key); err != nil {
		t.Fatal(err)
	}
	backups, err := filepath.Glob(file + ".before-ai-v2-*.json")
	if err != nil || len(backups) != 1 {
		t.Fatal(backups, err)
	}
	backup, err := os.ReadFile(backups[0])
	if err != nil || !bytes.Equal(original, backup) {
		t.Fatal("encrypted original not preserved", err)
	}
	legacy, err := loadKnowledgeTime(backups[0], g.Workspace, false, key)
	if err != nil || legacy.Version != 1 {
		t.Fatal(legacy.Version, err)
	}
	current, err := loadKnowledgeTime(file, g.Workspace, false, key)
	if err != nil || current.Version != 2 || len(current.Snapshots) != 2 || current.Snapshots[1].AI == nil {
		t.Fatal("format2 not readable", err)
	}
	if err := saveKnowledgeTime(file, next, key); err == nil {
		t.Fatal("stale v1 upgrade replaced newer history")
	}
	if reloaded, err := loadKnowledgeTime(file, g.Workspace, false, key); err != nil || len(reloaded.Snapshots) != 2 {
		t.Fatal("stale upgrade damaged current history", err)
	}
}

func TestKnowledgeConclusionAIBudgetsAndUnboundQuestions(t *testing.T) {
	s, g := conclusionAISession()
	s.Messages = s.Messages[1:]
	ai := captureKnowledgeAIConclusions(g, map[string]*Session{s.ID: s}, "scope")
	if len(ai.Records) != 1 || ai.Records[0].QuestionBound || !ai.Partial {
		t.Fatal("missing initial prompt silently bound", ai)
	}
	s, g = conclusionAISession()
	for i := 0; i < 10; i++ {
		id := string(rune('a' + i))
		r := &Task{ID: id, Prompt: id, WorkspaceID: "scope", Status: "completed"}
		s.Runs = append(s.Runs, r)
		s.Messages = append(s.Messages, Message{Role: "user", SourceRunID: id, Content: id}, Message{Role: "assistant", SourceRunID: id, Content: "## 结论\n" + strings.Repeat("证据", 3000)})
	}
	ai = captureKnowledgeAIConclusions(g, map[string]*Session{s.ID: s}, "scope")
	if len(ai.Records) > 4 || !ai.Partial || knowledgeAIHistoryBytes(ai) > 72<<10 {
		t.Fatal("AI capture not bounded", len(ai.Records), knowledgeAIHistoryBytes(ai))
	}
	if err := validateKnowledgeAIHistory(ai, g); err != nil {
		t.Fatal(err)
	}
}
