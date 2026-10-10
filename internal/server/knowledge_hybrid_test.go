package server

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestKnowledgeHybridFusionPreservesEvidence(t *testing.T) {
	chunks := []documentHit{{ID: "literal", Text: "battery runtime", Hash: "digest-a", Locator: "page 1", File: knowledgeNode{Source: "workspace"}}, {ID: "semantic", Text: "endurance and power use", Hash: "digest-b", Locator: "page 2", File: knowledgeNode{Source: "reference"}}}
	lexical := documentKeywordRanks(chunks, "battery runtime")
	vectors, err := documentVectorRanks([]float64{1, 0}, [][]float64{{0, 1}, {1, 0}}, 2)
	if err != nil {
		t.Fatal(err)
	}
	hits, err := documentFuseRanks(chunks, lexical, vectors, .75, 12)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 || hits[0].ID != "semantic" || hits[0].Hash != "digest-b" || hits[0].Locator != "page 2" || hits[0].File.Source != "reference" {
		t.Fatal(hits)
	}
	if chunks[1].Score != 0 {
		t.Fatal("mutated source snapshot")
	}
	literalOnly, err := documentFuseRanks(chunks, lexical, vectors, 0, 12)
	if err != nil || len(literalOnly) != 1 || literalOnly[0].ID != "literal" {
		t.Fatal(literalOnly, err)
	}
}
func TestKnowledgeHybridVectorValidation(t *testing.T) {
	for _, test := range []struct {
		q []float64
		v [][]float64
		n int
	}{{nil, nil, 0}, {[]float64{1}, nil, 1}, {[]float64{1}, [][]float64{{1, 2}}, 1}, {[]float64{math.NaN()}, [][]float64{{1}}, 1}, {[]float64{1}, [][]float64{{math.Inf(1)}}, 1}} {
		if _, err := documentVectorRanks(test.q, test.v, test.n); err == nil {
			t.Fatal("invalid vectors admitted", test)
		}
	}
	ranks, err := documentVectorRanks([]float64{1e200, 1e200}, [][]float64{{1e200, 1e200}, {0, 0}, {-1, -1}}, 3)
	if err != nil || len(ranks) != 1 || ranks[0].Index != 0 || math.Abs(ranks[0].Score-1) > 1e-12 {
		t.Fatal(ranks, err)
	}
}
func TestKnowledgeHybridRejectsStaleRankIndices(t *testing.T) {
	chunks := []documentHit{{ID: "one"}}
	for _, ranks := range [][]documentRank{{{1, 1}}, {{0, math.NaN()}}, {{0, 1}, {0, 1}}, {{0, 0}}} {
		if _, err := documentFuseRanks(chunks, ranks, nil, .5, 12); err == nil {
			t.Fatal("invalid ranks admitted", ranks)
		}
	}
	a, err := documentFuseRanks([]documentHit{{ID: "a"}, {ID: "b"}}, []documentRank{{0, 1}}, []documentRank{{1, 1}}, .5, 12)
	if err != nil {
		t.Fatal(err)
	}
	b, err := documentFuseRanks([]documentHit{{ID: "a"}, {ID: "b"}}, []documentRank{{0, 1}}, []documentRank{{1, 1}}, .5, 12)
	if err != nil || !reflect.DeepEqual(a, b) || a[0].ID != "a" {
		t.Fatal("unstable tie", a, b, err)
	}
}

func TestKnowledgeHybridKeywordRetrievalCompatibility(t *testing.T) {
	a := testApp(t)
	body := []byte("battery runtime is twenty minutes\nOther details")
	if err := os.WriteFile(filepath.Join(a.workPath, "runtime.txt"), body, 0600); err != nil {
		t.Fatal(err)
	}
	g := a.knowledgeSnapshot(context.Background())
	for _, mode := range []string{"original", "rag"} {
		result, err := a.retrieveDocuments(context.Background(), g, documentRequest{Query: "battery runtime", Mode: mode, Root: "workspace", Path: "runtime.txt"})
		if err != nil || len(result.Hits) != 1 {
			t.Fatal(mode, result, err)
		}
		hit := result.Hits[0]
		if hit.File.Path != "runtime.txt" || hit.Hash != hash(body) || hit.Locator == "" || hit.ID == "" {
			t.Fatal("lost provenance", hit)
		}
		if mode == "rag" && result.Engine != "local-tfidf-retrieval; no embeddings" {
			t.Fatal("misreported vector capability", result.Engine)
		}
	}
	result, err := a.retrieveDocuments(context.Background(), g, documentRequest{Query: "BATTERY RUNTIME", Mode: "original", Root: "workspace", Path: "runtime.txt"})
	if err != nil || len(result.Hits) != 0 {
		t.Fatal("literal search changed case behavior", result, err)
	}
}
