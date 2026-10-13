package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKnowledgeDocumentContinuation(t *testing.T) {
	a := testApp(t)
	seedKnowledgeLocalRestart(t, a)
	var g knowledgeGraph
	for i := 0; i < 25; i++ {
		g = sharedSnapshot(t, a)
		if sharedTargetCount(g) == 120 {
			break
		}
	}
	if sharedTargetCount(g) != 120 {
		t.Fatal("fixture incomplete")
	}
	in := documentRequest{Query: "confidential", Mode: "original", Workspace: g.Workspace, Root: "workspace"}
	seen := map[string]bool{}
	cursor := ""
	last := 0
	for batch := 0; batch < 10; batch++ {
		in.Cursor = cursor
		r, err := a.retrieveDocuments(context.Background(), g, in)
		if err != nil {
			t.Fatal(err)
		}
		if r.Files > 32 || r.Scanned <= last || r.Scanned-last > 32 {
			t.Fatalf("bad batch bounds %+v", r)
		}
		for _, h := range r.Hits {
			if seen[h.File.ID] {
				t.Fatal("duplicate file across batches")
			}
			seen[h.File.ID] = true
		}
		last = r.Scanned
		cursor = r.NextCursor
		if cursor == "" {
			if r.Scanned != r.Total {
				t.Fatal("false completion")
			}
			break
		}
		if batch == 0 {
			changed := in
			changed.Query = "different"
			changed.Cursor = cursor
			if _, err := a.retrieveDocuments(context.Background(), g, changed); err == nil {
				t.Fatal("changed query accepted cursor")
			}
			changed = in
			changed.Cursor = cursor
			changed.Region = "source:other"
			if _, err := a.retrieveDocuments(context.Background(), g, changed); err == nil {
				t.Fatal("changed filter accepted cursor")
			}
			altered := g
			altered.Nodes = append([]knowledgeNode(nil), g.Nodes...)
			altered.Nodes[0].Origin = "changed"
			// Ensure a file origin is changed, rather than a directory ignored by retrieval.
			for i := range altered.Nodes {
				if altered.Nodes[i].Kind == "file" {
					altered.Nodes[i].Origin = "changed"
					break
				}
			}
			changed = in
			changed.Cursor = cursor
			if _, err := a.retrieveDocuments(context.Background(), altered, changed); err == nil {
				t.Fatal("changed catalogue accepted cursor")
			}
		}
	}
	if len(seen) != 120 {
		t.Fatalf("only %d files found", len(seen))
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.retrieveDocuments(cancelled, g, in); err == nil {
		t.Fatal("cancelled request completed")
	}
	in.Cursor = "invalid"
	if _, err := a.retrieveDocuments(context.Background(), g, in); err == nil {
		t.Fatal("malformed cursor accepted")
	}
}

func TestKnowledgeDocumentContinuationKeepsAllOriginalMatches(t *testing.T) {
	a := testApp(t)
	var body strings.Builder
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&body, "needle match %03d\n", i)
	}
	if err := os.WriteFile(filepath.Join(a.workPath, "many.txt"), []byte(body.String()), 0600); err != nil {
		t.Fatal(err)
	}
	g := a.knowledgeSnapshot(context.Background())
	in := documentRequest{Query: "needle", Mode: "original", Workspace: g.Workspace, Root: "workspace", Path: "many.txt"}
	seen := map[string]bool{}
	cursor := ""
	wantSizes := []int{40, 40, 20}
	for page, want := range wantSizes {
		in.Cursor = cursor
		result, err := a.retrieveDocuments(context.Background(), g, in)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Hits) != want {
			t.Fatalf("page %d: got %d hits, want %d (offset %d)", page+1, len(result.Hits), want, result.MatchOffset)
		}
		if result.MatchOffset != page*40 {
			t.Fatalf("page %d: match continuation offset=%d, want %d", page+1, result.MatchOffset, page*40)
		}
		if result.Chunks > 50 {
			t.Fatalf("page %d rescanned too many chunks for 40 results: chunks=%d", page+1, result.Chunks)
		}
		for _, hit := range result.Hits {
			if seen[hit.ID] {
				t.Fatalf("duplicate hit %s on page %d", hit.ID, page+1)
			}
			seen[hit.ID] = true
		}
		cursor = result.NextCursor
		if page < len(wantSizes)-1 && cursor == "" {
			t.Fatalf("page %d ended before all matches were returned", page+1)
		}
	}
	if cursor != "" || len(seen) != 100 {
		t.Fatalf("continuation did not finish exactly 100 matches: cursor=%q hits=%d", cursor, len(seen))
	}
}

func TestKnowledgeDocumentContinuationResumesInsideChunkLimitedFile(t *testing.T) {
	a := testApp(t)
	paths := []string{"search-a.txt", "search-b.txt", "search-c.txt"}
	for _, name := range paths {
		body := strings.Repeat("x", 899994) + "needle"
		if err := os.WriteFile(filepath.Join(a.workPath, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	g := a.knowledgeSnapshot(context.Background())
	ids := make([]string, 0, len(paths))
	for _, name := range paths {
		found := false
		for _, node := range g.Nodes {
			if node.Kind == "file" && node.Path == name {
				ids = append(ids, node.ID)
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("indexed test file %q not found", name)
		}
	}
	in := documentRequest{Query: "needle", Mode: "original", Workspace: g.Workspace, Root: "workspace", IDs: ids}
	first, err := a.retrieveDocuments(context.Background(), g, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Hits) != 2 || first.NextCursor == "" || !first.Truncated {
		t.Fatalf("first bounded page should return two matches and continue inside the third file: hits=%d scanned=%d total=%d cursor=%q truncated=%v", len(first.Hits), first.Scanned, first.Total, first.NextCursor, first.Truncated)
	}
	raw, err := base64.RawURLEncoding.DecodeString(first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	var cursor struct {
		Position    int
		HitOffset   int
		ChunkOffset int
	}
	if err := json.Unmarshal(raw, &cursor); err != nil {
		t.Fatal(err)
	}
	if cursor.Position != 2 || cursor.HitOffset != 2 || cursor.ChunkOffset == 0 {
		t.Fatalf("cursor did not preserve partial-file position: %+v", cursor)
	}
	in.Cursor = first.NextCursor
	second, err := a.retrieveDocuments(context.Background(), g, in)
	if err != nil {
		t.Fatal(err)
	}
	firstPaths := map[string]bool{}
	for _, hit := range first.Hits {
		firstPaths[hit.File.Path] = true
	}
	if len(second.Hits) != 1 || firstPaths[second.Hits[0].File.Path] || second.NextCursor != "" || second.Scanned != second.Total {
		t.Fatalf("continuation lost or duplicated the final file match: hits=%+v scanned=%d total=%d cursor=%q", second.Hits, second.Scanned, second.Total, second.NextCursor)
	}
	if !containsPath(paths, second.Hits[0].File.Path) {
		t.Fatalf("continuation returned unexpected file %q", second.Hits[0].File.Path)
	}
}

func containsPath(paths []string, target string) bool {
	for _, p := range paths {
		if p == target {
			return true
		}
	}
	return false
}
