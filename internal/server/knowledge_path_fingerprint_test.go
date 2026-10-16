package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestKnowledgePathIndexedTextFingerprint(t *testing.T) {
	for _, extra := range []int64{0, 20000} {
		t.Run(fmt.Sprintf("unobserved_tail_%d", extra), func(t *testing.T) {
			body := "\u03b1 title\r\nreference.go\r\n"
			source := referenceNode("F1", "report.md", body)
			source.Size = int64(len(body)) + extra
			target := referenceNode("F2", "reference.go", "package reference\n")
			target.Size = int64(len(target.Text))
			graph := knowledgeGraph{Workspace: "W", Nodes: []knowledgeNode{source, target}, Edges: []knowledgeEdge{{From: "F1", To: "F2", Kind: "mention", Line: 2, Column: 1}}}
			result, err := findKnowledgePath(context.Background(), graph, knowledgePathRequest{From: "F1", To: "F2", Revision: "R"})
			if err != nil || !result.Found {
				t.Fatalf("path missing: %+v %v", result, err)
			}
			raw, err := json.Marshal(result.Nodes[0])
			if err != nil {
				t.Fatal(err)
			}
			var node map[string]any
			if err := json.Unmarshal(raw, &node); err != nil {
				t.Fatal(err)
			}
			hash := sha256.Sum256([]byte(body))
			if node["indexedTextHash"] != fmt.Sprintf("%x", hash) || node["indexedTextBytes"] != float64(len(body)) {
				t.Fatalf("path source position is not bound to exact observed UTF-8/CRLF bytes: hash=%v bytes=%v", node["indexedTextHash"], node["indexedTextBytes"])
			}
			if extra > 0 && node["contentHash"] != nil {
				t.Fatal("indexed prefix falsely labeled as full-file hash")
			}
		})
	}
	source := referenceNode("F1", "source.go", "snippet")
	source.ContentHash = strings.Repeat("a", 64)
	graph := knowledgeGraph{Nodes: []knowledgeNode{source}, Edges: []knowledgeEdge{}}
	result, err := findKnowledgePath(context.Background(), graph, knowledgePathRequest{From: "F1", To: "F1"})
	if err != nil || result.Nodes[0].ContentHash != source.ContentHash {
		t.Fatal("existing full-file identity lost")
	}
}
