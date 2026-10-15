package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
)

func TestKnowledgeConclusionCatalogNonMarkdownSavedText(t *testing.T) {
	for _, format := range []string{".txt", ".go", ".pdf", ".docx", ""} {
		t.Run(format, func(t *testing.T) {
			n := conclusionDocument("## 结论\n这不是自动结论😀\n[例](basis.txt)\n\n[例]: basis.txt\n")
			n.Path = "saved" + format
			n.Format = format
			if format == ".pdf" || format == ".docx" {
				n.Size = 9000
			}
			s := knowledgeTimeSnapshot{ID: "Hplain", Digest: strings.Repeat("a", 64), Graph: knowledgeGraph{Workspace: "w", Nodes: []knowledgeNode{n}}}
			cs, _ := knowledgeConclusionCandidates(s, n.ID)
			if len(cs) == 0 {
				t.Fatal("saved non-Markdown text excluded")
			}
			c := cs[0]
			if c.SourceKind != "document_excerpt" || !strings.Contains(c.Quote.Heading, "非Markdown") {
				t.Fatal(c)
			}
			p := knowledgeConclusionPick{Snapshot: s.ID, Node: c.Node, Candidate: c.Token, End: len(utf16.Encode([]rune(c.Quote.Text)))}
			m, err := knowledgeConclusionSelect(s, p, "fixture")
			if err != nil {
				t.Fatal(err)
			}
			set := knowledgeItemEvidence(knowledgeSelectionEvidenceItem("manual", m), s, newKnowledgeConclusionReferenceIndex(s.Graph))
			if set.Context != "source_not_markdown" || len(set.References) != 0 || !set.Partial {
				t.Fatal("non-Markdown text interpreted as Markdown citation", set)
			}
			out := compareKnowledgeTime(s, s).Conclusions
			if len(out.Changes) != 0 || out.Sources != 0 {
				t.Fatal("non-Markdown headings became automatic conclusions", out)
			}
			if (format == ".pdf" || format == ".docx") && !c.Quote.Partial {
				t.Fatal("extracted text claimed complete original document")
			}
		})
	}
}

func TestKnowledgeConclusionCatalogLargeNodeFilterReachability(t *testing.T) {
	a := testApp(t)
	workspace, _ := a.knowledgeUpdatesWorkspace()
	g := knowledgeGraph{Workspace: workspace, Nodes: []knowledgeNode{}, Sources: []knowledgeSource{{Region: "workspace", State: "ready"}}}
	for i := 0; i < 1000; i++ {
		body := fmt.Sprintf("historical item %04d first", i)
		g.Nodes = append(g.Nodes, knowledgeNode{ID: fmt.Sprintf("F%012x", i), Kind: "file", Root: "workspace", Region: "workspace", Path: fmt.Sprintf("item-%04d.txt", i), Text: body, Size: int64(len(body)), ReadState: "ready"})
	}
	store, _, err := appendKnowledgeTime(knowledgeTimeStore{Version: 1, Workspace: workspace}, g, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	g.Nodes = append([]knowledgeNode(nil), g.Nodes...)
	g.Nodes[999].Text = "historical item 0999 after"
	g.Nodes[999].Size = int64(len(g.Nodes[999].Text))
	store, _, err = appendKnowledgeTime(store, g, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	before, after := store.Snapshots[0], store.Snapshots[1]
	cs, partial := knowledgeConclusionCandidates(before, "")
	if !partial || len(cs) != 128 {
		t.Fatal("unfiltered catalog not bounded", len(cs), partial)
	}
	last := g.Nodes[999]
	for _, c := range cs {
		if c.Node == last.ID {
			t.Fatal("fixture did not put target beyond first list")
		}
	}
	// Every retained source, not a sampled subset, must be reachable by the
	// existing bounded node filter without consulting the live filesystem.
	for _, n := range before.Graph.Nodes {
		filtered, _ := knowledgeConclusionCandidates(before, n.ID)
		if len(filtered) == 0 || filtered[0].Node != n.ID || filtered[0].Quote.Text != n.Text {
			t.Fatal("retained source inaccessible", n.ID)
		}
	}
	if err = saveKnowledgeTime(knowledgeTimeFile(a.dataPath, workspace, false), store, knowledgeTimeKey(a.token)); err != nil {
		t.Fatal(err)
	}
	query := "/api/knowledge-map/timeline/topics?workspace=" + workspace
	read := query + "&before=" + before.ID + "&after=" + after.ID + "&beforeNode=" + last.ID + "&afterNode=" + last.ID
	w := request(a, "GET", read, nil)
	requireStatus(t, w, 200)
	var response struct {
		Before, After []knowledgeConclusionCandidate
	}
	if err = json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Before) == 0 || len(response.After) == 0 || response.Before[0].Node != last.ID {
		t.Fatal("HTTP node filter unreachable")
	}
	pick := func(s knowledgeTimeSnapshot, c knowledgeConclusionCandidate) knowledgeConclusionPick {
		return knowledgeConclusionPick{Snapshot: s.ID, Node: c.Node, Candidate: c.Token, End: len(utf16.Encode([]rune(c.Quote.Text)))}
	}
	bp, ap := pick(before, response.Before[0]), pick(after, response.After[0])
	requireStatus(t, request(a, "PUT", query, map[string]any{"expected": 0, "action": "upsert", "label": "last retained source", "before": bp, "after": ap}), 200)
	w = request(a, "POST", "/api/knowledge-map/timeline/compare?workspace="+workspace, map[string]string{"before": bp.Snapshot, "after": ap.Snapshot})
	requireStatus(t, w, 200)
	var out knowledgeTimeComparison
	if err = json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Conclusions.Manual) != 1 || !out.Conclusions.Manual[0].TextChanged || len(out.Conclusions.Changes) != 0 {
		t.Fatal("last source comparison failed")
	}
}

func knowledgeTextFixture(t *testing.T) (*App, knowledgeTimeStore, string) {
	t.Helper()
	a := testApp(t)
	workspace, epoch := a.knowledgeUpdatesWorkspace()
	for _, name := range []string{"topic.md", "basis.txt"} {
		if err := os.WriteFile(filepath.Join(a.workspace.Name(), name), []byte("fixture source"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, word := range []string{"纯文本甲😀", "纯文本乙😀"} {
		if err := os.WriteFile(filepath.Join(a.workspace.Name(), "note.txt"), []byte("## 结论\n"+word+"\n[例](basis.txt)\n"), 0600); err != nil {
			t.Fatal(err)
		}
		g := a.knowledgeSnapshotModeCached(context.Background(), false, nil)
		if err := a.recordKnowledgeTime(g, false, epoch); err != nil {
			t.Fatal(err)
		}
	}
	h, err := loadKnowledgeTime(knowledgeTimeFile(a.dataPath, workspace, false), workspace, false, knowledgeTimeKey(a.token))
	if err != nil || len(h.Snapshots) != 2 {
		t.Fatal("text fixture history", err, len(h.Snapshots))
	}
	if err := os.Remove(filepath.Join(a.workspace.Name(), "note.txt")); err != nil {
		t.Fatal(err)
	}
	return a, h, workspace
}

func TestKnowledgeConclusionCatalogUnavailableTextRemainsUnknown(t *testing.T) {
	for _, kind := range []string{"missing_path", "not_captured", "invalid_utf8", "read_error", "source_unavailable", "duplicate"} {
		t.Run(kind, func(t *testing.T) {
			n := knowledgeNode{ID: "Ftext", Kind: "file", Root: "workspace", Region: "workspace", Path: "note.txt", Text: "saved text", Size: 10, ReadState: "ready"}
			s := knowledgeTimeSnapshot{ID: "H", Graph: knowledgeGraph{Workspace: "w", Nodes: []knowledgeNode{n}}}
			switch kind {
			case "missing_path":
				s.Graph.Nodes[0].Path = ""
			case "not_captured":
				s.Graph.Nodes[0].Text = ""
			case "invalid_utf8":
				s.Graph.Nodes[0].Text = string([]byte{0xff})
			case "read_error":
				s.Graph.Nodes[0].ReadState = "read_error"
			case "source_unavailable":
				s.Graph.Sources = []knowledgeSource{{Region: "workspace", State: "unavailable"}}
			case "duplicate":
				s.Graph.Nodes = append(s.Graph.Nodes, n)
			}
			cs, partial := knowledgeConclusionCandidates(s, "")
			if len(cs) != 0 || !partial {
				t.Fatal("unavailable source became selectable/complete", kind, len(cs), partial)
			}
		})
	}
}
