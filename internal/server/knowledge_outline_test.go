package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKnowledgeOutlineDeepHeadingAndBounds(t *testing.T) {
	prefix := strings.Repeat("ordinary body, not cached by outline\n", 400)
	body := prefix + "```md\n# forged heading\n```\n## 深层恢复策略\r\n"
	o := knowledgeExtractOutline(strings.NewReader(body), "notes.md")
	if len(o.Entries) != 1 || o.Entries[0].Line != 404 || o.Entries[0].Text != "## 深层恢复策略" || o.Truncated {
		t.Fatalf("%+v", o)
	}
	if o.ContentHash != fmt.Sprintf("%x", sha256.Sum256([]byte(body))) {
		t.Fatal("missing complete stream digest")
	}
	raw, _ := json.Marshal(o)
	if bytes.Contains(raw, []byte("ordinary body")) || !knowledgeOutlineValid(o) {
		t.Fatal(string(raw))
	}
	for _, body := range []string{strings.Repeat("## title\n", 100), strings.Repeat("x\n", knowledgeOutlineReadBytes), strings.Repeat("x", 20000)} {
		bounded := knowledgeExtractOutline(strings.NewReader(body), "notes.md")
		if !bounded.Truncated || !knowledgeOutlineValid(bounded) {
			t.Fatalf("bounds %+v", bounded)
		}
	}
	if e := knowledgeExtractOutline(strings.NewReader("Title\n=====\n\nOther\n-----\n"), "notes.rst"); len(e.Entries) != 2 || e.Entries[1].Line != 4 {
		t.Fatal(e)
	}
}
func TestKnowledgeOutlineDeclarationCandidates(t *testing.T) {
	cases := map[string]string{"main.go": "// func Fake() {}\n/*\nfunc Comment() {}\n*/\nfunc (r *Reader) Restore(ctx context.Context) error {\n}", "main.py": "# def fake():\nasync def restore(ctx):\n    pass", "main.js": "// function fake() {}\nexport async function restore(ctx) {\n}\nconst recover = (x) => x;"}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			o := knowledgeExtractOutline(strings.NewReader(body), name)
			if len(o.Entries) == 0 || !knowledgeOutlineValid(o) {
				t.Fatal(o)
			}
			for _, e := range o.Entries {
				if strings.Contains(e.Text, "Fake") || strings.Contains(e.Text, "Comment") || strings.Contains(e.Text, "fake") || e.Kind != "declaration-candidate" {
					t.Fatal(e)
				}
			}
		})
	}
	if e := knowledgeExtractOutline(strings.NewReader("# title"), "secret.bin"); len(e.Entries) != 0 {
		t.Fatal(e)
	}
}
func TestKnowledgeOutlineCachePersistenceEditDelete(t *testing.T) {
	a := testApp(t)
	file := filepath.Join(a.workPath, "deep.md")
	body := strings.Repeat("plain body\n", 1000) + "## TailTitle\n"
	if err := os.WriteFile(file, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	var last *knowledgeScanCache
	get := func() knowledgeGraph {
		t.Helper()
		c := newKnowledgeScanCache(last)
		g := a.knowledgeSnapshotModeCached(context.Background(), false, c)
		if c.err != nil {
			t.Fatal(c.err)
		}
		last = c
		return g
	}
	find := func(g knowledgeGraph) *knowledgeNode {
		for i := range g.Nodes {
			if g.Nodes[i].Root == "workspace" && g.Nodes[i].Path == "deep.md" {
				return &g.Nodes[i]
			}
		}
		return nil
	}
	first := find(get())
	if first == nil || len(first.Outline.Entries) != 1 || first.Outline.Entries[0].Line != 1001 || len(first.Text) > 8192 {
		t.Fatal(first)
	}
	second := find(get())
	if second == nil || &first.Outline.Entries[0] != &second.Outline.Entries[0] {
		t.Fatal("unchanged file outline not reused")
	}
	// Exercise the encrypted local checkpoint codec, not merely a JSON struct.
	for _, cp := range last.localCheckpoints {
		if cp.Area.root != "workspace" {
			continue
		}
		p := localPayload("fixture-outline", cp.View, last.files, cp.Area.location)
		if err := validateKnowledgeLocalCursor(&p); err != nil {
			t.Fatal(err)
		}
		key := bytes.Repeat([]byte{7}, 32)
		dest := filepath.Join(t.TempDir(), "cursor.json")
		if err := saveKnowledgeLocalCursor(dest, p, key); err != nil {
			t.Fatal(err)
		}
		disk, _ := os.ReadFile(dest)
		if bytes.Contains(disk, []byte("TailTitle")) {
			t.Fatal("plaintext checkpoint")
		}
		loaded, err := loadKnowledgeLocalCursor(dest, "fixture-outline", key)
		if err != nil {
			t.Fatal(err)
		}
		_, materials := loaded.restore(cp.Area.location)
		if len(materials[cp.Area.location+"\x00deep.md"].outline.Entries) != 1 {
			t.Fatal("outline lost on reload")
		}
	}
	if err := os.WriteFile(file, []byte(body+"## ChangedTitle\n"), 0600); err != nil {
		t.Fatal(err)
	}
	changed := find(get())
	if changed == nil || len(changed.Outline.Entries) != 2 {
		t.Fatal(changed)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		get()
	}
	if find(get()) != nil {
		t.Fatal("deleted file retained")
	}
}

func TestKnowledgeOutlineAIProjection(t *testing.T) {
	o := knowledgeOutline{Entries: []knowledgeOutlineEntry{{Kind: "heading", Line: 1, Text: "private unrelated title"}, {Kind: "heading", Line: 2, Text: "restore A"}, {Kind: "heading", Line: 3, Text: "restore B"}, {Kind: "heading", Line: 4, Text: "restore C"}, {Kind: "heading", Line: 5, Text: "restore D"}}}
	matches := knowledgeOutlineMatches(o, "restore")
	raw, _ := json.Marshal(matches)
	if len(matches.Entries) != 3 || bytes.Contains(raw, []byte("private")) || bytes.Contains(raw, []byte("restore D")) || len(o.Entries) != 5 {
		t.Fatal(string(raw))
	}
	if got := knowledgeOutlineMatches(o, "absent"); len(got.Entries) != 0 {
		t.Fatal(got)
	}
}

func TestKnowledgeOutlineViewBudget(t *testing.T) {
	c := newKnowledgeScanCache(nil)
	c.outlineBytes = knowledgeOutlineViewBudget - 100
	memo := knowledgeFileMemo{outline: knowledgeOutline{Entries: []knowledgeOutlineEntry{{Kind: "heading", Line: 1, Text: "First title"}, {Kind: "heading", Line: 2, Text: "Second title"}}}}
	kept := c.keepMaterial("one", memo)
	if len(kept.outline.Entries) != 1 || !kept.outline.Truncated || c.outlineBytes > knowledgeOutlineViewBudget {
		t.Fatal(kept, c.outlineBytes)
	}
	if len(memo.outline.Entries) != 2 {
		t.Fatal("modified previous immutable observation")
	}
	before := c.outlineBytes
	c.keepMaterial("one", kept)
	if c.outlineBytes != before {
		t.Fatal("alias double counted")
	}
}

func TestKnowledgeOutlinePartialDigestNotFullFile(t *testing.T) {
	body := strings.Repeat("x\n", knowledgeOutlineReadBytes)
	out := knowledgeExtractOutline(strings.NewReader(body), "large.md")
	if out.ContentHash != "" || !out.Truncated {
		t.Fatal(out)
	}
}
