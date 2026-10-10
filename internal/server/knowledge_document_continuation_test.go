package server

import (
	"context"
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
