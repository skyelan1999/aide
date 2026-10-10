package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func indexPolicyResponse(t *testing.T, a *App) map[string]any {
	t.Helper()
	r := request(a, "GET", "/api/knowledge-map/index-policy", nil)
	requireStatus(t, r, 200)
	var v map[string]any
	if err := json.Unmarshal(r.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	return v
}
func TestKnowledgeIndexPolicyPersistenceAndConflict(t *testing.T) {
	a := testApp(t)
	v := indexPolicyResponse(t, a)
	p := defaultKnowledgeIndexPolicy()
	p.FileBudget = 50
	p.LanguageService = "go-types"
	p.PriorityPaths["workspace"] = []string{"important"}
	body := map[string]any{"policy": p, "workspaceId": v["workspaceId"], "revision": v["revision"]}
	oldEpoch := a.wsRevision
	requireStatus(t, request(a, "PUT", "/api/knowledge-map/index-policy", body), 200)
	if a.wsRevision <= oldEpoch {
		t.Fatal("cache epoch unchanged")
	}
	saved := indexPolicyResponse(t, a)
	if saved["policy"].(map[string]any)["languageService"] != "go-types" {
		t.Fatal("service not persisted", saved)
	}
	if saved["revision"] == v["revision"] {
		t.Fatal("revision unchanged")
	}
	requireStatus(t, request(a, "PUT", "/api/knowledge-map/index-policy", body), 409)
	a.mu.Lock()
	originalPath := a.wsConfig.Workspace.Path
	a.wsConfig.Workspace.Path = originalPath + "/another"
	isolated, isolatedErr := a.loadKnowledgeIndexPolicy()
	a.wsConfig.Workspace.Path = originalPath
	a.mu.Unlock()
	if isolatedErr != nil || isolated.FileBudget != 800 {
		t.Fatal("workspace policy leaked", isolated, isolatedErr)
	}
	a.mu.Lock()
	loaded, err := a.loadKnowledgeIndexPolicy()
	a.mu.Unlock()
	if err != nil || loaded.FileBudget != 50 {
		t.Fatal(loaded, err)
	}
	body["revision"] = saved["revision"]
	body["workspaceId"] = "stale"
	requireStatus(t, request(a, "PUT", "/api/knowledge-map/index-policy", body), 409)
	for _, invalid := range []string{"../escape", ".hidden", "node_modules/pkg", "a/../b", "/absolute"} {
		p.PriorityPaths["workspace"] = []string{invalid}
		if p.validate() == nil {
			t.Fatal("accepted", invalid)
		}
	}
}
func TestKnowledgeIndexPriorityBudget(t *testing.T) {
	a := testApp(t)
	for i := 0; i < 60; i++ {
		p := filepath.Join(a.workPath, "ordinary", string(rune('a'+i))+".md")
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("ordinary"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(a.workPath, "zz-focus"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.workPath, "zz-focus", "important.md"), []byte("priority"), 0600); err != nil {
		t.Fatal(err)
	}
	p := defaultKnowledgeIndexPolicy()
	p.FileBudget = 50
	p.PriorityPaths["workspace"] = []string{"zz-focus/important.md"}
	v := indexPolicyResponse(t, a)
	requireStatus(t, request(a, "PUT", "/api/knowledge-map/index-policy", map[string]any{"policy": p, "workspaceId": v["workspaceId"], "revision": v["revision"]}), 200)
	graph := a.knowledgeSnapshot(context.Background())
	found := false
	for _, node := range graph.Nodes {
		if node.Root == "workspace" && node.Path == "zz-focus/important.md" {
			found = true
		}
	}
	c := coverageSource(t, graph, "workspace").Coverage
	if !found || c.Files < 1 || c.Files > c.FileBudget || c.FileBudget > 50 || !c.Progressive || c.Pending == 0 {
		t.Fatal("priority file missing or budget wrong", found, c)
	}
}

func TestKnowledgeIndexScopePersistenceAndGraph(t *testing.T) {
	a := testApp(t)
	os.MkdirAll(filepath.Join(a.workPath, "docs", "deep"), 0700)
	os.WriteFile(filepath.Join(a.workPath, "docs", "deep", "included.md"), []byte("inside"), 0600)
	os.WriteFile(filepath.Join(a.workPath, "outside.md"), []byte("outside"), 0600)
	p := defaultKnowledgeIndexPolicy()
	p.ScopePaths = map[string][]string{"workspace": {"docs/deep"}}
	v := indexPolicyResponse(t, a)
	requireStatus(t, request(a, "PUT", "/api/knowledge-map/index-policy", map[string]any{"policy": p, "workspaceId": v["workspaceId"], "revision": v["revision"]}), 200)
	a.mu.Lock()
	loaded, err := a.loadKnowledgeIndexPolicy()
	a.mu.Unlock()
	if err != nil || len(loaded.ScopePaths["workspace"]) != 1 {
		t.Fatal(loaded, err)
	}
	for _, cached := range []bool{false, true} {
		var g knowledgeGraph
		if cached {
			g = a.knowledgeSnapshotModeCached(context.Background(), false, newKnowledgeScanCache(nil))
		} else {
			g = a.knowledgeSnapshot(context.Background())
		}
		found := false
		for _, n := range g.Nodes {
			if n.Root != "workspace" {
				continue
			}
			if n.Path == "outside.md" {
				t.Fatal("scope leaked", cached)
			}
			if n.Path == "docs/deep/included.md" {
				found = true
			}
		}
		if !found {
			t.Fatal("scope missing", cached)
		}
		c := coverageSource(t, g, "workspace").Coverage
		if len(c.ScopePaths) != 1 || c.ScopePaths[0] != "docs/deep" {
			t.Fatal(c)
		}
	}
	p.PriorityPaths["workspace"] = []string{"outside.md"}
	if p.validate() == nil {
		t.Fatal("out of scope priority accepted")
	}
	p.PriorityPaths = map[string][]string{}
	for _, invalid := range []string{"../escape", ".secret", "node_modules/pkg", "/absolute", "docs/../src"} {
		p.ScopePaths["workspace"] = []string{invalid}
		if p.validate() == nil {
			t.Fatal("unsafe scope accepted", invalid)
		}
	}
}

func TestKnowledgeIndexLanguageServiceValidation(t *testing.T) {
	p := defaultKnowledgeIndexPolicy()
	p.LanguageService = "external-command"
	if p.validate() == nil {
		t.Fatal("unknown service accepted")
	}
	p.LanguageService = "go-types"
	if p.validate() != nil {
		t.Fatal("Go service rejected")
	}
}
