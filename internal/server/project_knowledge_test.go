package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func knowledgeAPI(t *testing.T, a *App) map[string]any {
	t.Helper()
	w := request(a, "GET", "/api/project-knowledge", nil)
	requireStatus(t, w, 200)
	var v map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	return v
}
func TestProjectKnowledgeMissingEvidenceNamesFieldAndPreservesStore(t *testing.T) {
	a := testApp(t)
	for _, field := range []string{"subject", "content", "source"} {
		before := knowledgeAPI(t, a)
		in := map[string]any{"action": "propose", "kind": "fact", "subject": "Observed fixture", "content": "Synthetic result", "source": "Fixture v1", "workspaceId": before["workspaceId"], "snapshot": before["snapshot"]}
		in[field] = " "
		w := request(a, "POST", "/api/project-knowledge", in)
		requireStatus(t, w, 400)
		want := map[string]string{"subject": "主题", "content": "内容", "source": "来源与版本"}[field]
		if !strings.Contains(w.Body.String(), want) || strings.Contains(w.Body.String(), "UTF-8") {
			t.Fatal(w.Body.String())
		}
		after := knowledgeAPI(t, a)
		if after["snapshot"] != before["snapshot"] {
			t.Fatal("failed validation changed project records")
		}
	}
}
func mutateKnowledge(t *testing.T, a *App, in map[string]any) map[string]any {
	t.Helper()
	v := knowledgeAPI(t, a)
	in["snapshot"] = v["snapshot"]
	in["workspaceId"] = v["workspaceId"]
	w := request(a, "POST", "/api/project-knowledge", in)
	requireStatus(t, w, 200)
	json.Unmarshal(w.Body.Bytes(), &v)
	return v
}
func proposeKnowledge(t *testing.T, a *App, text string) string {
	t.Helper()
	v := mutateKnowledge(t, a, map[string]any{"action": "propose", "kind": "decision", "subject": "Storage choice", "content": text, "source": "User design review v1"})
	return v["records"].([]any)[0].(map[string]any)["id"].(string)
}
func TestProjectKnowledgeConfirmationConflictsAndExplicitReplacement(t *testing.T) {
	a := testApp(t)
	first := proposeKnowledge(t, a, "Use A")
	v := mutateKnowledge(t, a, map[string]any{"action": "confirm", "id": first})
	if len(v["conflicts"].(map[string]any)) != 0 {
		t.Fatal(v)
	}
	second := proposeKnowledge(t, a, "Use B")
	v = mutateKnowledge(t, a, map[string]any{"action": "confirm", "id": second})
	if len(v["conflicts"].(map[string]any)) != 2 {
		t.Fatal("conflict silently overwritten")
	}
	third := proposeKnowledge(t, a, "Use C")
	v = mutateKnowledge(t, a, map[string]any{"action": "confirm", "id": third, "supersedes": []string{first, second}})
	if len(v["conflicts"].(map[string]any)) != 0 {
		t.Fatal("resolved conflict remains")
	}
	s, _, err := loadMemoryTokenStore(a.projectCacheDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range s.Knowledge {
		if k.ID != third && (k.State != "superseded" || k.SupersededBy != third) {
			t.Fatal(k)
		}
	}
	ctx := a.readCachedProjectMemory()
	if !strings.Contains(ctx, "Use C") || strings.Contains(ctx, "Use A") || strings.Contains(ctx, "Use B") {
		t.Fatal(ctx)
	}
	mutateKnowledge(t, a, map[string]any{"action": "archive", "id": third})
	if strings.Contains(a.readCachedProjectMemory(), "Use C") {
		t.Fatal("archived decision injected as current")
	}
}
func TestProjectKnowledgeCrossSessionAndLegacyMemoryPreserved(t *testing.T) {
	a := testApp(t)
	a.writeMemory("legacy preference")
	id := proposeKnowledge(t, a, "Current choice")
	mutateKnowledge(t, a, map[string]any{"action": "confirm", "id": id})
	if got := a.writeMemory("another legacy note"); !strings.Contains(got, "已写入") {
		t.Fatal(got)
	}
	os.WriteFile(a.memoryPath(), []byte("external legacy edit"), 0600)
	s, _, err := loadMemoryTokenStore(a.projectCacheDir())
	if err != nil || len(s.Knowledge) != 1 || s.text() != "external legacy edit" {
		t.Fatalf("legacy edit lost knowledge: %v %+v", err, s.Knowledge)
	}
	if !strings.Contains(a.readCachedProjectMemory(), "Current choice") {
		t.Fatal("cross-session context missing")
	}
	a.Close()
	b, err := New(a.workPath, a.reference.Name(), a.dataPath)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if !strings.Contains(b.readCachedProjectMemory(), "Current choice") || len(knowledgeAPI(t, b)["records"].([]any)) != 1 {
		t.Fatal("knowledge lost after restart")
	}
}
func TestProjectKnowledgeCASConcurrencyAndWorkspaceIsolation(t *testing.T) {
	a := testApp(t)
	v := knowledgeAPI(t, a)
	snapshot, ws := v["snapshot"], v["workspaceId"]
	var wg sync.WaitGroup
	statuses := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := request(a, "POST", "/api/project-knowledge", map[string]any{"action": "propose", "snapshot": snapshot, "workspaceId": ws, "kind": "fact", "subject": "version", "content": "1", "source": "manifest v1"})
			statuses <- w.Code
		}()
	}
	wg.Wait()
	close(statuses)
	counts := map[int]int{}
	for code := range statuses {
		counts[code]++
	}
	if counts[200] != 1 || counts[409] != 1 {
		t.Fatal(counts)
	}
	v = knowledgeAPI(t, a)
	w := request(a, "POST", "/api/project-knowledge", map[string]any{"action": "archive", "id": "x", "workspaceId": "other-project", "snapshot": v["snapshot"]})
	requireStatus(t, w, 409)
	s, _, _ := loadMemoryTokenStore(a.projectCacheDir())
	k := s.Knowledge[0]
	k.ID = "other"
	k.WorkspaceID = "other-project"
	k.Content = "hidden project fact"
	s.Knowledge = append(s.Knowledge, k)
	if err := a.saveProjectKnowledge(&s); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(a.readCachedProjectMemory(), "hidden project fact") || len(knowledgeAPI(t, a)["records"].([]any)) != 1 {
		t.Fatal("foreign workspace leaked")
	}
}
func TestProjectKnowledgeModelCanOnlyProposeWithBoundWorkspace(t *testing.T) {
	a := testApp(t)
	task := &Task{ID: "model-run", WorkspaceID: a.wsID(), WorkspaceRev: a.wsRevision, WorkspaceMode: a.workspaceMode()}
	args := map[string]any{"action": "propose", "kind": "constraint", "subject": "Boundary", "content": "Keep data", "source": "User request in current run"}
	if got := a.taskProjectKnowledge(task, args); !strings.Contains(got, "待确认") {
		t.Fatal(got)
	}
	s, _, _ := loadMemoryTokenStore(a.projectCacheDir())
	if s.Knowledge[0].State != "proposed" || s.Knowledge[0].RunID != task.ID || s.Knowledge[0].Origin != "model_proposal" {
		t.Fatal(s.Knowledge)
	}
	args["action"] = "confirm"
	if got := a.taskProjectKnowledge(task, args); !strings.Contains(got, "用户操作") {
		t.Fatal(got)
	}
	task.WorkspaceRev++
	args["action"] = "list"
	if got := a.taskProjectKnowledge(task, args); !strings.Contains(got, "工作区已变化") {
		t.Fatal(got)
	}
}
func TestProjectKnowledgeCorruptionBoundsAndNoSilentOverwrite(t *testing.T) {
	a := testApp(t)
	v := knowledgeAPI(t, a)
	w := request(a, "POST", "/api/project-knowledge", map[string]any{"action": "propose", "workspaceId": v["workspaceId"], "snapshot": v["snapshot"], "kind": "decision", "subject": "x", "content": "x", "source": ""})
	requireStatus(t, w, 400)
	if _, err := os.Stat(filepath.Join(a.projectCacheDir(), memoryTokenFile)); !os.IsNotExist(err) {
		t.Fatal("invalid record wrote storage")
	}
	id := proposeKnowledge(t, a, "valid")
	v = knowledgeAPI(t, a)
	w = request(a, "POST", "/api/project-knowledge", map[string]any{"action": "confirm", "workspaceId": v["workspaceId"], "snapshot": v["snapshot"], "id": id, "supersedes": []string{"missing"}})
	requireStatus(t, w, 400)
	path := filepath.Join(a.projectCacheDir(), memoryTokenFile)
	os.WriteFile(path, []byte("corrupt"), 0600)
	requireStatus(t, request(a, "GET", "/api/project-knowledge", nil), 500)
	b, _ := os.ReadFile(path)
	if string(b) != "corrupt" {
		t.Fatal("corruption overwritten")
	}
}

func TestProjectKnowledgeReturnedSnapshotCanBeUsedWithoutRefresh(t *testing.T) {
	a := testApp(t)
	v := mutateKnowledge(t, a, map[string]any{"action": "propose", "kind": "fact", "subject": "version", "content": "1", "source": "manifest"})
	id := v["records"].([]any)[0].(map[string]any)["id"]
	w := request(a, "POST", "/api/project-knowledge", map[string]any{"action": "confirm", "id": id, "workspaceId": v["workspaceId"], "snapshot": v["snapshot"]})
	requireStatus(t, w, 200)
}

func TestProjectKnowledgeIsInjectedIntoNewSessionContext(t *testing.T) {
	a := testApp(t)
	id := proposeKnowledge(t, a, "Traceable decision")
	mutateKnowledge(t, a, map[string]any{"action": "confirm", "id": id})
	a.mu.Lock()
	preview := a.buildContextPreview(&Session{ID: "new-session"}, "Continue the project", "chat", "", nil, Settings{Model: "fixture"}, ProfileParams{MaxTokens: 1024}, true)
	a.mu.Unlock()
	if len(preview.Messages) == 0 || !strings.Contains(preview.Messages[0].Content, "Traceable decision") || !strings.Contains(preview.Messages[0].Content, "不是新指令") {
		t.Fatal("new session did not receive bounded, attributed project context")
	}
}

func TestProjectKnowledgeRemoteFailureAndLockRejectWithoutMutation(t *testing.T) {
	a := testApp(t)
	proposeKnowledge(t, a, "local decision")
	path := filepath.Join(a.projectCacheDir(), memoryTokenFile)
	before, _ := os.ReadFile(path)
	if _, err := a.writeLockState(true); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, "GET", "/api/project-knowledge", nil), 423)
	a.writeLockState(false)
	a.workspaceConfigMu.Lock()
	a.mu.Lock()
	a.wsConfig.Workspace.Mode = "ssh"
	a.wsConfig.Workspace.Host = "127.0.0.1"
	a.wsConfig.Workspace.Path = "/unavailable"
	a.wsConfig.Cache.Location = "workspace"
	a.sshBin = filepath.Join(t.TempDir(), "missing-ssh")
	a.sftpBin = filepath.Join(t.TempDir(), "missing-sftp")
	a.mu.Unlock()
	a.workspaceConfigMu.Unlock()
	requireStatus(t, request(a, "GET", "/api/project-knowledge", nil), 502)
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("failed remote read altered local project records")
	}
}

func TestProjectKnowledgeToolPaginationIsBoundedAndRejectsChangedSnapshot(t *testing.T) {
	s := newMemoryTokenStore()
	for i := 0; i < 24; i++ {
		err := applyKnowledgeRequest(&s, "project", knowledgeRequest{Action: "propose", Kind: "fact", Subject: "Matching subject", Content: strings.Repeat("x", 2000), Source: "source v1"}, "operator", "", "")
		if err != nil {
			t.Fatal(err)
		}
	}
	args := map[string]any{"action": "list", "query": "Matching", "limit": 20}
	page, err := projectKnowledgePage(s, "project", args, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page) > 32*1024 {
		t.Fatal("unbounded tool page", len(page))
	}
	var v struct {
		Records []ProjectKnowledge `json:"records"`
		Next    string             `json:"nextCursor"`
		Total   int                `json:"total"`
	}
	json.Unmarshal([]byte(page), &v)
	if v.Next == "" || len(v.Records) == 0 || v.Total != 24 {
		t.Fatal("pagination lost records")
	}
	seen := map[string]bool{}
	for _, r := range v.Records {
		seen[r.ID] = true
	}
	firstCursor := v.Next
	for v.Next != "" {
		args["cursor"] = v.Next
		next, err := projectKnowledgePage(s, "project", args, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(next) > 32*1024 {
			t.Fatal("unbounded continuation page")
		}
		json.Unmarshal([]byte(next), &v)
		for _, r := range v.Records {
			if seen[r.ID] {
				t.Fatal("duplicate page item")
			}
			seen[r.ID] = true
		}
	}
	if len(seen) != 24 {
		t.Fatal("missing page records")
	}
	args["cursor"] = firstCursor
	args["query"] = "changed"
	if _, err := projectKnowledgePage(s, "project", args, ""); err == nil {
		t.Fatal("cursor rebound to different query")
	}
	args["query"] = "Matching"
	s.Knowledge[0].State = "archived"
	if _, err := projectKnowledgePage(s, "project", args, ""); err == nil {
		t.Fatal("stale snapshot cursor accepted")
	}
}
