package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The optional read-only repository probe uses exactly the reference adapter,
// including the built-in source aliases and production per-source budgets.
func TestKnowledgeReferenceRepositoryProgressive(t *testing.T) {
	a := testApp(t)
	directory := os.Getenv("AIDE_TEST_REFERENCE_REPOSITORY")
	sourcePath := os.Getenv("AIDE_TEST_REFERENCE_SOURCE_PATH")
	expected := 300
	region := "context"
	if sourcePath != "" {
		expected = 1000
		region = "source:repository-probe"
		a.mu.Lock()
		a.sourceRegistry.Sources = append(a.sourceRegistry.Sources, Source{ID: "repository-probe", Name: "repository-probe", Type: "local", Enabled: true, Config: SourceConfig{Path: sourcePath}})
		a.mu.Unlock()
	} else {
		if directory == "" {
			directory = t.TempDir()
			for i := 0; i < expected; i++ {
				p := filepath.Join(directory, fmt.Sprintf("module-%02d", i/10))
				if err := os.MkdirAll(p, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(p, fmt.Sprintf("file-%03d.go", i)), []byte("package fixture\nfunc Example() {}\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
		} else {
			expected = 1000
		}
		reference, err := a.openLocalSourceRoot(context.Background(), directory)
		if err != nil {
			t.Fatal(err)
		}
		a.mu.Lock()
		a.reference.Close()
		a.reference = reference
		a.mu.Unlock()
	}
	var previous *knowledgeScanCache
	last := 0
	for batch := 0; batch < 12; batch++ {
		cache := newKnowledgeScanCache(previous)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		graph := a.knowledgeSnapshotModeCached(ctx, false, cache)
		cancel()
		if cache.err != nil {
			t.Fatalf("batch %d failed: %v", batch, cache.err)
		}
		count := 0
		for _, n := range graph.Nodes {
			if n.Region == region && n.Kind == "file" {
				count++
			}
		}
		if count < last {
			t.Fatalf("reference lost nodes: %d -> %d", last, count)
		}
		t.Logf("batch=%d referenceFiles=%d totalNodes=%d", batch, count, len(graph.Nodes))
		for _, source := range graph.Sources {
			if source.Region == region {
				t.Logf("sourceState=%s message=%q", source.State, source.Message)
			}
		}
		last = count
		previous = cache
		if count >= expected {
			return
		}
	}
	t.Fatalf("reference did not publish expected files: %d want >=%d", last, expected)
}
