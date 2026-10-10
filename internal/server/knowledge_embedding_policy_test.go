package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestKnowledgeEmbeddingPolicyPersistence(t *testing.T) {
	a := testApp(t)
	get := request(a, "GET", "/api/knowledge-map/embedding-policy", nil)
	requireStatus(t, get, 200)
	var current struct {
		Policy      knowledgeEmbeddingPolicy `json:"policy"`
		WorkspaceID string                   `json:"workspaceId"`
		Revision    string                   `json:"revision"`
		HasKey      bool                     `json:"hasKey"`
	}
	if json.Unmarshal(get.Body.Bytes(), &current) != nil || current.Policy.Enabled {
		t.Fatal("invalid default")
	}
	policy := knowledgeEmbeddingPolicy{Enabled: true, BaseURL: "http://127.0.0.1:9998/v1", Model: "fixture", VectorWeight: .75}
	body := map[string]any{"policy": policy, "workspaceId": current.WorkspaceID, "revision": current.Revision, "apiKey": "fixture-private-key"}
	put := request(a, "PUT", "/api/knowledge-map/embedding-policy", body)
	requireStatus(t, put, 200)
	if strings.Contains(put.Body.String(), "fixture-private-key") {
		t.Fatal("key returned")
	}
	if json.Unmarshal(put.Body.Bytes(), &current) != nil || !current.HasKey {
		t.Fatal("key missing")
	}
	requireStatus(t, request(a, "PUT", "/api/knowledge-map/embedding-policy", body), 409)
	raw, err := os.ReadFile(a.embeddingPolicyPath())
	if err != nil || strings.Contains(string(raw), "fixture-private-key") {
		t.Fatal("plaintext key persisted", err)
	}
	loaded, err := a.loadEmbeddingPolicy()
	if err != nil || loaded.Model != "fixture" || loaded.VectorWeight != .75 || loaded.Generation != 1 {
		t.Fatal(loaded, err)
	}
	body = map[string]any{"policy": loaded, "workspaceId": current.WorkspaceID, "revision": current.Revision, "clearKey": true}
	clear := request(a, "PUT", "/api/knowledge-map/embedding-policy", body)
	requireStatus(t, clear, 200)
	if a.vault.Has(a.embeddingVaultID()) {
		t.Fatal("key retained")
	}
}

func TestKnowledgeEmbeddingRAGIntegration(t *testing.T) {
	a := testApp(t)
	var calls atomic.Int32
	var unavailable atomic.Bool
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if unavailable.Load() {
			w.WriteHeader(503)
			fmt.Fprint(w, "private diagnostics")
			return
		}
		calls.Add(1)
		var body struct {
			Input []string `json:"input"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("body invalid")
		}
		data := []map[string]any{}
		for i, text := range body.Input {
			v := []float64{1, 0}
			if strings.Contains(text, "battery runtime is") {
				v = []float64{0, 1}
			}
			data = append(data, map[string]any{"index": i, "embedding": v})
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer s.Close()
	for name, text := range map[string]string{"literal.txt": "battery runtime is twenty minutes", "semantic.txt": "endurance and power use"} {
		if err := os.WriteFile(filepath.Join(a.workPath, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	g := a.knowledgeSnapshot(context.Background())
	base := documentRequest{Query: "battery runtime", Mode: "rag", Root: "workspace"}
	result, err := a.retrieveDocuments(context.Background(), g, base)
	if err != nil || calls.Load() != 0 || len(result.Hits) != 1 {
		t.Fatal("default should stay local", result, err)
	}
	p := knowledgeEmbeddingPolicy{Enabled: true, BaseURL: s.URL, Model: "fixture", VectorWeight: .75}
	if err := os.MkdirAll(filepath.Dir(a.embeddingPolicyPath()), 0700); err != nil {
		t.Fatal(err)
	}
	if err := atomicJSON(a.embeddingPolicyPath(), p); err != nil {
		t.Fatal(err)
	}
	result, err = a.retrieveDocuments(context.Background(), g, base)
	if err != nil || calls.Load() != 1 || len(result.Hits) != 2 || result.Hits[0].File.Path != "semantic.txt" || !strings.Contains(result.Engine, "weighted-rrf") {
		t.Fatal("hybrid path", result, err)
	}
	if result.Hits[0].Hash != hash([]byte("endurance and power use")) || result.Hits[0].Locator == "" {
		t.Fatal("provenance lost")
	}
	base.Mode = "original"
	_, err = a.retrieveDocuments(context.Background(), g, base)
	if err != nil || calls.Load() != 1 {
		t.Fatal("literal search contacted provider", err)
	}
	unavailable.Store(true)
	base.Mode = "rag"
	result, err = a.retrieveDocuments(context.Background(), g, base)
	if err != nil || len(result.Hits) != 1 || !strings.Contains(strings.Join(result.Warnings, " "), "回退") || strings.Contains(strings.Join(result.Warnings, " "), "private diagnostics") {
		t.Fatal("fallback", result, err)
	}
}

func TestKnowledgeEmbeddingRejectsChangedWorkspace(t *testing.T) {
	a := testApp(t)
	p := knowledgeEmbeddingPolicy{Enabled: true, Model: "fixture", VectorWeight: .5}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		a.wsRevision++
		a.mu.Unlock()
		fmt.Fprint(w, `{"data":[{"index":0,"embedding":[1]},{"index":1,"embedding":[1]}]}`)
	}))
	defer s.Close()
	p.BaseURL = s.URL
	os.MkdirAll(filepath.Dir(a.embeddingPolicyPath()), 0700)
	atomicJSON(a.embeddingPolicyPath(), p)
	_, _, _, err := a.hybridDocumentHits(context.Background(), knowledgeID(a.wsID(), "scope", "", "."), "query", []documentHit{{Text: "document"}})
	if err == nil || !strings.Contains(err.Error(), "已变化") {
		t.Fatal("stale result accepted", err)
	}
}

func TestKnowledgeEmbeddingCacheReuseAndInvalidation(t *testing.T) {
	a := testApp(t)
	var inputs atomic.Int32
	var dimension atomic.Int32
	dimension.Store(2)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		inputs.Store(int32(len(body.Input)))
		data := []map[string]any{}
		for i := range body.Input {
			v := make([]float64, dimension.Load())
			v[0] = 1
			data = append(data, map[string]any{"index": i, "embedding": v})
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer s.Close()
	p := knowledgeEmbeddingPolicy{Enabled: true, BaseURL: s.URL, Model: "fixture", VectorWeight: .5, Generation: 1}
	if err := os.MkdirAll(filepath.Dir(a.embeddingPolicyPath()), 0700); err != nil {
		t.Fatal(err)
	}
	if err := atomicJSON(a.embeddingPolicyPath(), p); err != nil {
		t.Fatal(err)
	}
	workspace := knowledgeID(a.wsID(), "scope", "", ".")
	chunks := []documentHit{{ID: "one", Hash: "one", Text: "battery evidence", Locator: "1"}, {ID: "two", Hash: "two", Text: "power evidence", Locator: "2"}}
	query := func(expected int32) {
		t.Helper()
		hits, engine, w, err := a.hybridDocumentHits(context.Background(), workspace, "battery", chunks)
		if err != nil || w != "" || !strings.Contains(engine, "weighted-rrf") || len(hits) != 2 || inputs.Load() != expected {
			t.Fatal("cache request", inputs.Load(), engine, w, err)
		}
	}
	query(3) // Query + both documents.
	query(1) // Only the fresh query; document vectors reused.
	a.mu.Lock()
	a.knowledgeVectors = nil
	a.mu.Unlock()
	query(1) // Encrypted disk reload after memory loss.
	chunks[1].Text = "changed source content"
	chunks[1].Hash = "changed"
	query(2) // Query + changed document only.
	p.Model = "new-model"
	p.Generation++
	if err := atomicJSON(a.embeddingPolicyPath(), p); err != nil {
		t.Fatal(err)
	}
	query(3)
	dimension.Store(3) // Provider silently changed dimension under the same model.
	_, engine, w, err := a.hybridDocumentHits(context.Background(), workspace, "battery", chunks)
	if err != nil || !strings.Contains(w, "回退") || strings.Contains(engine, "weighted-rrf") {
		t.Fatal("dimension mismatch did not invalidate", engine, w, err)
	}
	query(3) // All document vectors regenerated after invalidation.
}
