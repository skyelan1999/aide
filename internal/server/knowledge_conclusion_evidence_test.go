package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func conclusionEvidenceSnapshot(id, content string) knowledgeTimeSnapshot {
	quote := conclusionDocument("## 结论\n采用方案甲，见[原始依据](basis.txt)。")
	basis := knowledgeNode{ID: "F0123456789ab", Kind: "file", Root: "workspace", Region: "workspace", Path: "basis.txt", Text: content, Size: int64(len(content)), ReadState: "ready"}
	return knowledgeTimeSnapshot{ID: id, Graph: knowledgeGraph{Workspace: "w", Nodes: []knowledgeNode{quote, basis}}}
}
func TestKnowledgeConclusionEvidenceChangesWithoutQuoteChange(t *testing.T) {
	a, b := conclusionEvidenceSnapshot("old", "原始依据甲"), conclusionEvidenceSnapshot("new", "原始依据乙")
	out := compareKnowledgeTime(a, b).Conclusions
	if len(out.Changes) != 0 || len(out.Evidence) != 1 || out.EvidenceState != "saved_snapshot_explicit_references" {
		t.Fatalf("lost independent evidence delta: %+v", out)
	}
	r := out.Evidence[0]
	if !r.ContentChanged || r.ReferencesChanged || r.Unknown || r.Kind != "evidence_changed" {
		t.Fatal(r)
	}
	ref := r.Before.References[0]
	if ref.Node != "F0123456789ab" || ref.Excerpt != "原始依据甲" || ref.ExcerptHash != hash([]byte("原始依据甲")) || !r.Before.Historical || r.Before.ModelUse != "not_proven" || r.After.Snapshot != "new" {
		t.Fatal(r)
	}
	if same := compareKnowledgeTime(b, b).Conclusions; len(same.Evidence) != 0 {
		t.Fatal("stable references became changed", same)
	}
	// Moving the paragraph or target modification time is not an evidence edit.
	b.Graph.Nodes[0] = conclusionDocument("\n\n## 结论\n采用方案甲，见[原始依据](basis.txt)。")
	b.Graph.Nodes[1].Modified = "later"
	if same := compareKnowledgeTime(conclusionEvidenceSnapshot("old", "原始依据乙"), b).Conclusions; len(same.Evidence) != 0 || len(same.Changes) != 0 {
		t.Fatal(same)
	}
}
func TestKnowledgeConclusionEvidenceReferencesAndUnknowns(t *testing.T) {
	a, b := conclusionEvidenceSnapshot("old", "甲"), conclusionEvidenceSnapshot("new", "甲")
	b.Graph.Nodes[0] = conclusionDocument("## 结论\n采用方案甲，见[另一依据](other.txt)。")
	b.Graph.Nodes = append(b.Graph.Nodes, knowledgeNode{ID: "F123456789abc", Kind: "file", Root: "workspace", Region: "workspace", Path: "other.txt", Text: "乙", Size: 3, ReadState: "ready"})
	r := compareKnowledgeTime(a, b).Conclusions.Evidence[0]
	if !r.ReferencesChanged || r.ContentChanged || r.Unknown {
		t.Fatal(r)
	}
	for _, state := range []string{"missing", "disabled", "read_error", "partial", "ambiguous", "coverage"} {
		t.Run(state, func(t *testing.T) {
			b := conclusionEvidenceSnapshot("new", "乙")
			switch state {
			case "missing":
				b.Graph.Nodes = b.Graph.Nodes[:1]
			case "disabled":
				b.Graph.Sources = []knowledgeSource{{Region: "workspace", State: "disabled"}}
			case "read_error":
				b.Graph.Nodes[1].ReadState = "unavailable"
			case "partial":
				b.Graph.Nodes[1].Size = 100
			case "ambiguous":
				b.Graph.Nodes = append(b.Graph.Nodes, b.Graph.Nodes[1])
			case "coverage":
				b.Graph.Nodes[1].Text = "甲扩展"
				b.Graph.Nodes[1].Size = 100
			}
			out := compareKnowledgeTime(a, b).Conclusions
			if len(out.Evidence) != 1 || !out.Evidence[0].Unknown {
				t.Fatal("missing/partial scope became certainty", out)
			}
			if state == "coverage" && out.Evidence[0].ContentChanged {
				t.Fatal("extended coverage became content edit", out.Evidence)
			}
		})
	}
}
func TestKnowledgeConclusionEvidenceScopeAndSyntax(t *testing.T) {
	g := conclusionEvidenceSnapshot("saved", "依据").Graph
	from := g.Nodes[0]
	text := "真实[F0123456789ab]，链接[依据](<basis.txt#片段>)。\n```md\n[F0123456789ab] [假](missing.txt)\n```\n`[F111111111111]`\n<!-- [假](secret.txt) -->\n[外部](https://example.invalid/private) [未解析][ref]"
	s := knowledgeConclusionEvidence(text, from, "saved", newKnowledgeConclusionReferenceIndex(g), false)
	if len(s.References) != 3 || !s.Partial {
		t.Fatal(s)
	}
	if s.References[0].Node != "F0123456789ab" || s.References[1].Node != "F0123456789ab" || s.References[2].State != "external_unfetched" {
		t.Fatal(s)
	}
	// A relative reference stays in its own area; an AI relative path must be unique.
	foreign := g.Nodes[1]
	foreign.ID = "F222222222222"
	foreign.Root = "source"
	foreign.Source = "other"
	foreign.Region = "source:other"
	foreign.Text = "FOREIGN"
	g.Nodes = append(g.Nodes, foreign)
	s = knowledgeConclusionEvidence("[依据](basis.txt)", from, "saved", newKnowledgeConclusionReferenceIndex(g), false)
	if len(s.References) != 1 || s.References[0].Root != "workspace" {
		t.Fatal(s)
	}
	s = knowledgeConclusionEvidence("[依据](basis.txt)", knowledgeNode{Kind: "session"}, "saved", newKnowledgeConclusionReferenceIndex(g), false)
	if s.References[0].State != "ambiguous_target" {
		t.Fatal(s)
	}
	s = knowledgeConclusionEvidence("[明确来源](other:basis.txt)", from, "saved", newKnowledgeConclusionReferenceIndex(g), false)
	if s.References[0].Source != "other" {
		t.Fatal(s)
	}
	// Escaped citations and examples are not accepted.
	s = knowledgeConclusionEvidence("\\[F0123456789ab] \\[假](basis.txt)", from, "saved", newKnowledgeConclusionReferenceIndex(g), false)
	if len(s.References) != 0 {
		t.Fatal(s)
	}
	for _, broken := range []string{"[假](basis.txt", "[假](basis.txt(foo))"} {
		s = knowledgeConclusionEvidence(broken, from, "saved", newKnowledgeConclusionReferenceIndex(g), false)
		if len(s.References) != 0 || !s.Partial {
			t.Fatal("incomplete Markdown bound to a real file", s)
		}
	}
	s = knowledgeConclusionEvidence("[真](<basis.txt> \"标题\")", from, "saved", newKnowledgeConclusionReferenceIndex(g), false)
	if len(s.References) != 1 || s.References[0].Root != "workspace" {
		t.Fatal("complete angled target not resolved", s)
	}
	large := strings.Repeat("[重复](basis.txt) ", 1000)
	s = knowledgeConclusionEvidence(large, from, "saved", newKnowledgeConclusionReferenceIndex(g), false)
	if !s.Partial || len(s.References) > 16 {
		t.Fatal("reference budget unbounded", s)
	}
}
func TestKnowledgeConclusionEvidenceAIExactQuestion(t *testing.T) {
	s, g := conclusionAISession()
	s.Messages[1].Content = "## 结论\n采用方案甲，[依据](basis.txt)。"
	g.Nodes = append(g.Nodes, conclusionEvidenceSnapshot("a", "甲").Graph.Nodes[1])
	ai := captureKnowledgeAIConclusions(g, map[string]*Session{s.ID: s}, "scope")
	a := knowledgeTimeSnapshot{ID: "a", Graph: g, AI: ai}
	b := a
	b.ID = "b"
	b.Graph.Nodes = append([]knowledgeNode(nil), g.Nodes...)
	b.Graph.Nodes[1].Text = "乙"
	out := compareKnowledgeTime(a, b).Conclusions
	if len(out.Changes) != 0 || len(out.Evidence) != 1 || !out.Evidence[0].ContentChanged || out.Evidence[0].Session != s.ID {
		t.Fatal(out)
	}
	// A different question is not aligned merely because its heading matches.
	s.Runs[0].Prompt = "不同问题"
	s.Messages[0].Content = s.Runs[0].Prompt
	b.AI = captureKnowledgeAIConclusions(b.Graph, map[string]*Session{s.ID: s}, "scope")
	for _, r := range compareKnowledgeTime(a, b).Conclusions.Evidence {
		if r.Kind == "evidence_changed" {
			t.Fatal("different question was aligned", r)
		}
	}
}
func TestKnowledgeConclusionEvidenceActualIndexDiskHTTP(t *testing.T) {
	a := testApp(t)
	session, _ := conclusionAISession()
	a.mu.Lock()
	session.Runs[0].WorkspaceID = a.wsID()
	session.Messages[1].Content = "## 结论\n保持方案甲，[依据](basis.txt)。"
	a.sessions[session.ID] = session
	a.mu.Unlock()
	// Test only temporary workspace files; no live source read is possible after removal.
	root := a.workspace.Name()
	if err := os.WriteFile(filepath.Join(root, "conclusion.md"), []byte("## 结论\n使用方案甲，[依据](basis.txt)。"), 0600); err != nil {
		t.Fatal(err)
	}
	for i, body := range []string{"实际依据甲", "实际依据乙"} {
		if i == 1 {
			if err := os.WriteFile(filepath.Join(root, "conclusion.md"), []byte("## 结论\n使用方案乙，[依据](basis.txt)。"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(root, "basis.txt"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		_, epoch := a.knowledgeUpdatesWorkspace()
		g := a.knowledgeSnapshotModeCached(context.Background(), false, nil)
		if err := a.recordKnowledgeTime(g, false, epoch); err != nil {
			t.Fatal(err)
		}
	}
	workspace, _ := a.knowledgeUpdatesWorkspace()
	file := knowledgeTimeFile(a.dataPath, workspace, false)
	raw, err := os.ReadFile(file)
	if err != nil || strings.Contains(string(raw), "实际依据") {
		t.Fatal("plaintext history", err)
	}
	store, err := loadKnowledgeTime(file, workspace, false, knowledgeTimeKey(a.token))
	if err != nil || len(store.Snapshots) != 2 {
		t.Fatal(len(store.Snapshots), err)
	}
	for _, p := range []string{"conclusion.md", "basis.txt"} {
		if err := os.Remove(filepath.Join(root, p)); err != nil {
			t.Fatal(err)
		}
	}
	w := request(a, "POST", "/api/knowledge-map/timeline/compare?workspace="+workspace, map[string]string{"before": store.Snapshots[0].ID, "after": store.Snapshots[1].ID})
	requireStatus(t, w, 200)
	var result knowledgeTimeComparison
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range result.Conclusions.Evidence {
		if r.Path == "conclusion.md" {
			found = r.ContentChanged && !r.ReferencesChanged && r.Before.References[0].Excerpt == "实际依据甲" && r.After.References[0].Excerpt == "实际依据乙"
		}
	}
	if !found {
		t.Fatalf("actual saved evidence not compared: %+v", result.Conclusions)
	}
	aiFound := false
	for _, r := range result.Conclusions.Evidence {
		if r.SourceKind == "assistant_conclusion" && r.Session == session.ID && r.ContentChanged {
			aiFound = true
		}
	}
	if !aiFound {
		t.Fatal("unchanged actual AI quote lost basis changes", result.Conclusions)
	}
	if result.Before.AI != nil || result.After.AI != nil {
		t.Fatal("unrelated private AI history leaked through metadata")
	}
	if os.Getenv("AIDE_EXPORT_CONCLUSION_FIXTURE") == "1" {
		raw, _ := json.Marshal(result)
		t.Log("CONCLUSION_FIXTURE=" + string(raw))
	}
	// No evidence sidecar changes v1 document-only append semantics.
	_, _, err = appendKnowledgeTime(store, store.Snapshots[1].Graph, time.Now())
	if err != nil {
		t.Fatal(err)
	}
}

func TestKnowledgeConclusionEvidenceCodeHashAndBudgets(t *testing.T) {
	a, b := conclusionEvidenceSnapshot("a", "同一摘录"), conclusionEvidenceSnapshot("b", "同一摘录")
	a.Graph.Nodes[1].Size = 9000
	b.Graph.Nodes[1].Size = 9000
	a.Graph.Nodes[1].ContentHash = hash([]byte("完整索引代码甲"))
	b.Graph.Nodes[1].ContentHash = hash([]byte("完整索引代码乙"))
	r := compareKnowledgeTime(a, b).Conclusions.Evidence[0]
	if !r.ContentChanged || !r.Unknown || r.Before.References[0].ExcerptHash != r.After.References[0].ExcerptHash {
		t.Fatal("code capture beyond text excerpt missed", r)
	}
	text := "## 结论\n"
	for i := 0; i < 25; i++ {
		text += "[来源](file" + string(rune('a'+i)) + ".txt) "
	}
	s := knowledgeConclusionEvidence(text, a.Graph.Nodes[0], "a", newKnowledgeConclusionReferenceIndex(a.Graph), false)
	if !s.Partial || len(s.References) != 16 {
		t.Fatal("per-quote reference limit failed", s)
	}
	x := newKnowledgeConclusionReferenceIndex(a.Graph)
	x.budget = 1
	s = knowledgeConclusionEvidence("[来源](basis.txt)", a.Graph.Nodes[0], "a", x, false)
	if !s.Partial || s.References[0].State != "comparison_budget" {
		t.Fatal("hash input budget failed", s)
	}
	b.Graph.Workspace = "other"
	out := compareKnowledgeTime(a, b).Conclusions
	if out.EvidenceState != "workspace_identity_unknown" || len(out.Evidence) != 0 {
		t.Fatal("foreign workspace evidence aligned", out)
	}
}
