package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

func knowledgeReferenceFixture(t *testing.T) (*App, knowledgeTimeStore, string) {
	t.Helper()
	a := testApp(t)
	workspace, epoch := a.knowledgeUpdatesWorkspace()
	session, _ := conclusionAISession()
	a.mu.Lock()
	session.Runs[0].WorkspaceID = a.wsID()
	a.sessions[session.ID] = session
	a.mu.Unlock()
	for i, word := range []string{"甲", "乙"} {
		title := []string{"original", "revised"}[i]
		definition := "[ref]: basis.txt \"" + title + "\"\n"
		document := "# 报告\n## 结论\n采用方案甲，见[依据][ref]。\n\n## 附录\n\n" + definition
		for name, body := range map[string]string{"topic.md": document, "basis.txt": "保存的依据" + word} {
			if err := os.WriteFile(filepath.Join(a.workspace.Name(), name), []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
		}
		a.mu.Lock()
		session.Messages[1].Content = "## 结论\n采用方案甲，见[依据][ref]。\n\n## 附录\n" + strings.Repeat("这段上下文位于结论之外。\n", 300) + "\n" + definition
		a.mu.Unlock()
		g := a.knowledgeSnapshotModeCached(context.Background(), false, nil)
		if err := a.recordKnowledgeTime(g, false, epoch); err != nil {
			t.Fatal(err)
		}
	}
	h, err := loadKnowledgeTime(knowledgeTimeFile(a.dataPath, workspace, false), workspace, false, knowledgeTimeKey(a.token))
	if err != nil || len(h.Snapshots) != 2 {
		t.Fatal("reference fixture history", err, len(h.Snapshots))
	}
	// The history API must not borrow a present-day assistant's definitions.
	a.mu.Lock()
	delete(a.sessions, session.ID)
	a.mu.Unlock()
	return a, h, workspace
}

func TestKnowledgeConclusionMarkdownIndexedDiskHTTP(t *testing.T) {
	a, h, workspace := knowledgeReferenceFixture(t)
	for _, name := range []string{"topic.md", "basis.txt"} {
		if err := os.Remove(filepath.Join(a.workspace.Name(), name)); err != nil {
			t.Fatal(err)
		}
	}
	w := request(a, "POST", "/api/knowledge-map/timeline/compare?workspace="+workspace, map[string]string{"before": h.Snapshots[0].ID, "after": h.Snapshots[1].ID})
	requireStatus(t, w, 200)
	var result knowledgeTimeComparison
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Conclusions.Changes) != 0 || len(result.Conclusions.Evidence) != 2 {
		t.Fatal("actual history did not retain independent AI/document references", w.Body.String())
	}
	kinds := map[string]bool{}
	for _, row := range result.Conclusions.Evidence {
		kinds[row.SourceKind] = true
		if !row.ReferencesChanged || !row.ContentChanged || row.Unknown || len(row.Before.References) != 1 || len(row.After.References) != 1 || row.Before.Context != "saved_source" {
			t.Fatal("actual reference difference incomplete", row)
		}
		old, next := row.Before.References[0], row.After.References[0]
		if old.Reference != "[依据][ref]" || old.Target != "basis.txt" || !strings.Contains(old.Definition, "original") || !strings.Contains(next.Definition, "revised") || old.DefinitionHash == next.DefinitionHash || old.ExcerptHash == next.ExcerptHash {
			t.Fatal("historical definition/original basis not retained", old, next)
		}
	}
	if !kinds["document"] || !kinds["assistant_conclusion"] || result.Before.AI != nil || result.After.AI != nil {
		t.Fatal("source attribution or metadata isolation lost")
	}
}

func markdownEvidence(t *testing.T, body string) knowledgeConclusionEvidenceSet {
	t.Helper()
	s := conclusionEvidenceSnapshot("Hmd", "saved basis")
	s.Graph.Nodes[0] = conclusionDocument(body)
	items, _ := knowledgeConclusionEvidenceItems(s)
	if len(items) != 1 {
		t.Fatalf("expected one quote, got %d", len(items))
	}
	return knowledgeItemEvidence(items[0], s, newKnowledgeConclusionReferenceIndex(s.Graph))
}

func TestKnowledgeConclusionMarkdownReferenceDefinitions(t *testing.T) {
	for _, tt := range []struct{ name, usage, definition string }{
		{"full", "[依据][ref]", "[REF]: basis.txt \"original\""},
		{"collapsed", "[ref][]", "[ref]: <basis.txt>"},
		{"shortcut", "[ref]", "[ref]: basis.txt"},
		{"image", "![ref][]", "[ref]: basis.txt"},
		{"multiline label", "[依据][Ref\n label]", "[ref label]: basis.txt"},
		{"unicode case fold", "[ẞ]", "[SS]: basis.txt"},
		{"escaped label", "[依据][a\\*b]", "[a\\*b]: basis.txt"},
		{"duplicate first wins", "[ref]", "[ref]: basis.txt\n[ref]: wrong.txt"},
		{"multiline destination title", "[依据][ref]", "[ref]:\n  basis.txt\n  \"title on\n  two lines\""},
		{"container definition", "[依据][ref]", "> [ref]: basis.txt"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body := "## 结论\n采用方案，见" + tt.usage + "。\n\n## 后续\n\n" + tt.definition + "\n"
			set := markdownEvidence(t, body)
			if len(set.References) != 1 {
				t.Fatalf("reference missing: %+v", set)
			}
			r := set.References[0]
			if r.Kind != "markdown_reference" || r.Reference != tt.usage || r.Target != "basis.txt" || r.Node != "F0123456789ab" || r.DefinitionLine != 6+strings.Count(tt.usage, "\n") || r.SourceLine != 2 || len(r.DefinitionHash) != 64 || r.Definition == "" || set.Context != "saved_source" || set.SourceExcerptHash != hash([]byte(body)) {
				t.Fatalf("wrong reference provenance: %+v / %+v", r, set)
			}
		})
	}
	before := "[ref]: basis.txt\n\n## 结论\n[ref]"
	if set := markdownEvidence(t, before); len(set.References) != 1 || set.References[0].DefinitionLine != 1 || set.References[0].SourceLine != 4 {
		t.Fatal(set)
	}
}

func TestKnowledgeConclusionMarkdownIndependentDefinitionChanges(t *testing.T) {
	makeSnapshot := func(id, target, title, basis string) knowledgeTimeSnapshot {
		s := conclusionEvidenceSnapshot(id, basis)
		s.Graph.Nodes[0] = conclusionDocument("## 结论\n见[依据][ref]。\n\n## 附录\n\n[ref]: " + target + " \"" + title + "\"\n")
		other := s.Graph.Nodes[1]
		other.ID = "F111111111111"
		other.Path = "other.txt"
		s.Graph.Nodes = append(s.Graph.Nodes, other)
		return s
	}
	a := makeSnapshot("a", "basis.txt", "one", "甲")
	for _, tt := range []struct {
		name, target, title, basis string
		references, content        bool
	}{
		{"target", "other.txt", "one", "甲", true, false},
		{"title", "basis.txt", "two", "甲", true, false},
		{"basis", "basis.txt", "one", "乙", false, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			b := makeSnapshot("b", tt.target, tt.title, tt.basis)
			out := compareKnowledgeTime(a, b).Conclusions
			if len(out.Changes) != 0 || len(out.Evidence) != 1 {
				t.Fatalf("unchanged quote lost independent evidence change: %+v", out)
			}
			r := out.Evidence[0]
			if r.ReferencesChanged != tt.references || r.ContentChanged != tt.content || r.Unknown {
				t.Fatal(r)
			}
		})
	}
	b := a
	b.Graph.Nodes = append([]knowledgeNode(nil), a.Graph.Nodes...)
	b.Graph.Nodes[0] = conclusionDocument("## 结论\n见[依据][ref]。\n\n## 附录\n\n\n\n[ref]: basis.txt \"one\"\n\n[unused]: missing.txt\n")
	if out := compareKnowledgeTime(a, b).Conclusions; len(out.Evidence) != 0 || len(out.Changes) != 0 {
		t.Fatal("moving/unused definition became basis change", out)
	}
}

func TestKnowledgeConclusionMarkdownSyntaxDoesNotInventUsage(t *testing.T) {
	for _, body := range []string{
		"## 结论\n仅文字。\n[unused]: basis.txt\n",   // cannot interrupt the paragraph
		"## 结论\n仅文字。\n\n[unused]: basis.txt\n", // real definition, unused
		"## 结论\n```md\n[ref]\n```\n\n## 附录\n\n[ref]: basis.txt\n",
		"## 结论\n`[ref]`，\\[ref]。\n\n## 附录\n\n[ref]: basis.txt\n",
		"## 结论\n<!-- [ref] -->\n\n## 附录\n\n[ref]: basis.txt\n",
	} {
		if set := markdownEvidence(t, body); len(set.References) != 0 {
			t.Fatalf("example/definition became used evidence: %+v", set)
		}
	}
	set := markdownEvidence(t, "## 结论\n[F0123456789ab]。\n")
	if len(set.References) != 1 || set.References[0].Kind != "node_id" {
		t.Fatal("explicit node reference lost", set)
	}
	set = markdownEvidence(t, "## 结论\n[ref]\n\n## 附录\n\n```md\n[ref]: basis.txt\n```\n")
	if len(set.References) != 0 {
		t.Fatal("code definition used", set)
	}
	set = knowledgeConclusionEvidence("[unused]: basis.txt", conclusionDocument(""), "H", newKnowledgeConclusionReferenceIndex(conclusionEvidenceSnapshot("a", "x").Graph), false)
	if len(set.References) != 0 {
		t.Fatal("legacy definition counted without a use", set)
	}
	set = markdownEvidence(t, "## 结论\n[ref]\n\n## 后续\n[未定义][missing]\n\n[ref]: basis.txt\n")
	if len(set.References) != 1 || set.Partial {
		t.Fatal("unrelated unresolved prose contaminated the selected quote", set)
	}
}

func TestKnowledgeConclusionMarkdownAIBodyAndLegacyIsolation(t *testing.T) {
	s, g := conclusionAISession()
	basis := conclusionEvidenceSnapshot("a", "甲").Graph.Nodes[1]
	g.Nodes = append(g.Nodes, basis)
	s.Messages[1].Content = "## 结论\n[依据][ref]\n\n## 附录\n" + strings.Repeat("历史上下文。\n", 1000) + "\n[ref]: basis.txt\n"
	ai := captureKnowledgeAIConclusions(g, map[string]*Session{s.ID: s}, "scope")
	a := knowledgeTimeSnapshot{ID: "a", Graph: g, AI: ai}
	b := a
	b.ID = "b"
	b.Graph.Nodes = append([]knowledgeNode(nil), g.Nodes...)
	b.Graph.Nodes[1].Text = "乙"
	b.Graph.Nodes[1].Size = int64(len("乙"))
	out := compareKnowledgeTime(a, b).Conclusions
	if len(out.Changes) != 0 || len(out.Evidence) != 1 || !out.Evidence[0].ContentChanged || out.Evidence[0].Before.References[0].Target != "basis.txt" {
		t.Fatal("AI saved definition outside conclusion lost", out)
	}
	legacy := *ai
	legacy.Version = 1
	legacy.Records = append([]knowledgeAIConclusionRecord(nil), ai.Records...)
	legacy.Records[0].Body = nil
	a.AI = &legacy
	b.AI = &legacy
	a.Graph.Nodes[0].Text = "[ref]: basis.txt"
	b.Graph.Nodes[0].Text = "[ref]: basis.txt"
	out = compareKnowledgeTime(a, b).Conclusions
	if len(out.Evidence) != 1 || !out.Evidence[0].Unknown || len(out.Evidence[0].Before.References) != 0 || out.Evidence[0].Before.Context != "selected_excerpt_only" {
		t.Fatal("mixed session text backfilled legacy AI definition", out)
	}
}

func TestKnowledgeConclusionMarkdownManualSelectionContext(t *testing.T) {
	s, g := conclusionAISession()
	g.Nodes = append(g.Nodes, conclusionEvidenceSnapshot("a", "甲").Graph.Nodes[1])
	s.Messages[1].Content = "长行重复[依据][ref] 原话 [依据][ref]\r\n\r\n```md\r\n[依据][ref]\r\n```\r\n\r\n[ref]: basis.txt\r\n"
	ai := captureKnowledgeAIConclusions(g, map[string]*Session{s.ID: s}, "scope")
	snapshot := knowledgeTimeSnapshot{ID: "Hmanual", Digest: strings.Repeat("a", 64), Graph: g, AI: ai}
	candidates, _ := knowledgeConclusionCandidates(snapshot, "S1")
	for _, c := range candidates {
		if !strings.HasPrefix(c.Quote.Key, "saved_assistant_body:") {
			continue
		}
		for _, tt := range []struct {
			start int
			want  int
		}{
			{strings.Index(c.Quote.Text, "[依据][ref]"), 1},
			{strings.Index(c.Quote.Text, " 原话 ") + len(" 原话 "), 1},
			{strings.LastIndex(c.Quote.Text, "[依据][ref]"), 0},
		} {
			prefix := c.Quote.Text[:tt.start]
			p := knowledgeConclusionPick{Snapshot: snapshot.ID, Node: c.Node, Candidate: c.Token, Start: len(utf16.Encode([]rune(prefix))), End: len(utf16.Encode([]rune(prefix + "[依据][ref]")))}
			m, err := knowledgeConclusionSelect(snapshot, p, "fixture")
			if err != nil {
				t.Fatal(err)
			}
			item := knowledgeSelectionEvidenceItem("manual", m)
			evidence := knowledgeItemEvidence(item, snapshot, newKnowledgeConclusionReferenceIndex(g))
			if len(evidence.References) != tt.want || evidence.Context != "saved_source" {
				t.Fatalf("manual offset/code context lost: %+v", evidence)
			}
		}
		return
	}
	t.Fatal("body candidate missing")
}

func TestKnowledgeConclusionMarkdownBudgetsAndUnknownAnchor(t *testing.T) {
	s := conclusionEvidenceSnapshot("H", "x")
	s.Graph.Nodes[0] = conclusionDocument("## 结论\n[ref]\n\n## 后续\n\n[ref]: basis.txt\n")
	items, _ := knowledgeConclusionEvidenceItems(s)
	x := newKnowledgeConclusionReferenceIndex(s.Graph)
	x.parseBudget = 0
	set := knowledgeItemEvidence(items[0], s, x)
	if set.Context != "source_parse_budget" || !set.Partial || len(set.References) != 0 {
		t.Fatal(set)
	}
	bad := items[0]
	bad.conclusion.Line = 99
	set = knowledgeItemEvidence(bad, s, newKnowledgeConclusionReferenceIndex(s.Graph))
	if set.Context != "source_anchor_unknown" || !set.Partial {
		t.Fatal(set)
	}
	x = newKnowledgeConclusionReferenceIndex(s.Graph)
	_ = knowledgeItemEvidence(items[0], s, x)
	remaining := x.parseBudget
	_ = knowledgeItemEvidence(items[0], s, x)
	if x.parseBudget != remaining || len(x.markdown) != 1 {
		t.Fatal("same source was parsed repeatedly")
	}
}

func TestKnowledgeConclusionMarkdownLegacySelectionDoesNotLoseCodeContext(t *testing.T) {
	s, g := conclusionAISession()
	g.Nodes = append(g.Nodes, conclusionEvidenceSnapshot("a", "甲").Graph.Nodes[1])
	s.Messages[1].Content = "历史答复原话\n\n```md\n[依据][ref]\n```\n\n[ref]: basis.txt\n"
	ai := captureKnowledgeAIConclusions(g, map[string]*Session{s.ID: s}, "scope")
	ai.Version = 1
	ai.Records[0].Body = nil
	snapshot := knowledgeTimeSnapshot{ID: "Hlegacy", Digest: strings.Repeat("a", 64), Graph: g, AI: ai}
	cs, _ := knowledgeConclusionCandidates(snapshot, "S1")
	for _, c := range cs {
		if c.Quote.Key != "unclassified_reply" {
			continue
		}
		start := strings.Index(c.Quote.Text, "[依据][ref]")
		prefix := c.Quote.Text[:start]
		p := knowledgeConclusionPick{Snapshot: snapshot.ID, Node: c.Node, Candidate: c.Token, Start: len(utf16.Encode([]rune(prefix))), End: len(utf16.Encode([]rune(prefix + "[依据][ref]")))}
		m, err := knowledgeConclusionSelect(snapshot, p, "fixture")
		if err != nil {
			t.Fatal(err)
		}
		set := knowledgeItemEvidence(knowledgeSelectionEvidenceItem("manual", m), snapshot, newKnowledgeConclusionReferenceIndex(g))
		if len(set.References) != 0 || set.Context != "selected_excerpt_only" || !set.Partial {
			t.Fatal("legacy code example became a citation", set)
		}
		return
	}
	t.Fatal("legacy candidate missing")
}

func TestKnowledgeConclusionMarkdownEmptyDestinationCannotBindLiteralUsageFile(t *testing.T) {
	s := conclusionEvidenceSnapshot("H", "甲")
	s.Graph.Nodes[0] = conclusionDocument("## 结论\n[r]\n\n## 附录\n\n[r]: <>\n")
	s.Graph.Nodes[1].Path = "[r]"
	items, _ := knowledgeConclusionEvidenceItems(s)
	set := knowledgeItemEvidence(items[0], s, newKnowledgeConclusionReferenceIndex(s.Graph))
	if len(set.References) != 1 || set.References[0].Node != "" || set.References[0].State == "saved_excerpt" || !set.Partial {
		t.Fatal("empty destination bound the literal usage as filename", set)
	}
}

func TestKnowledgeConclusionMarkdownDefinitionPreservesOriginalCRLF(t *testing.T) {
	definition := "[ref label]: basis.txt \"first\r\nsecond\""
	usage := "[依据][ref\r\n label]"
	set := markdownEvidence(t, "## 结论\r\n"+usage+"\r\n\r\n## 附录\r\n\r\n"+definition+"\r\n")
	if len(set.References) != 1 {
		t.Fatal(set)
	}
	r := set.References[0]
	if r.Reference != usage || r.Definition != definition || r.DefinitionHash != hash([]byte(definition)) || r.SourceLine != 2 || r.DefinitionLine != 7 {
		t.Fatal("definition/usage bytes were normalized instead of preserved", r)
	}
}
