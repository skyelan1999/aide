package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func sessionChoice(t *testing.T, a *App, id string, choice SessionModelSelection, revision uint64) {
	t.Helper()
	body := map[string]any{"model": choice.Model, "strategy": choice.Strategy, "profile": choice.Profile, "reasoningEffort": choice.ReasoningEffort, "revision": revision}
	requireStatus(t, request(a, "PUT", "/api/sessions/"+id+"/model-selection", body), 200)
}
func getSelectionSession(t *testing.T, a *App, id string) Session {
	t.Helper()
	w := request(a, "GET", "/api/sessions/"+id, nil)
	requireStatus(t, w, 200)
	var s Session
	if err := json.Unmarshal(w.Body.Bytes(), &s); err != nil {
		t.Fatal(err)
	}
	return s
}
func waitSelectionTask(t *testing.T, a *App, id string) {
	t.Helper()
	end := time.Now().Add(5 * time.Second)
	for time.Now().Before(end) {
		s := getSelectionSession(t, a, id)
		last := s.Runs[len(s.Runs)-1]
		if last.Status != "running" {
			if last.Status != "completed" {
				t.Fatalf("task failed: %s %s", last.Status, last.Error)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("selection task did not finish")
}
func selectionModels(a *App, urlA, urlB string) {
	a.settings = Settings{Model: "model-a", ActiveModel: "model-a", BaseURL: urlA, ReasoningEffort: "auto", Models: []ModelRef{{ID: "model-a", ProviderID: "a", ContextWindow: 65536}, {ID: "model-b", ProviderID: "b", ContextWindow: 131072, Vision: true}}, ModelProviders: []ModelProvider{{ID: "a", BaseURL: urlA, ActiveKeyID: "fixture"}, {ID: "b", BaseURL: urlB, ActiveKeyID: "fixture"}}}
}
func TestSessionModelSelectionAPI(t *testing.T) {
	a := testApp(t)
	selectionModels(a, "http://a.invalid", "http://b.invalid")
	one, two := createSession(t, a), createSession(t, a)
	choice := SessionModelSelection{Strategy: "manual", Profile: "precise", Model: "model-b", ReasoningEffort: "high"}
	sessionChoice(t, a, one.ID, choice, 0)
	first, second := getSelectionSession(t, a, one.ID), getSelectionSession(t, a, two.ID)
	if *first.ModelSelection != choice || second.ModelSelection.Model != "model-a" || second.ModelSelection.ReasoningEffort != "auto" {
		t.Fatal("session choices crossed")
	}
	if a.settings.Model != "model-a" || a.settings.ReasoningEffort != "auto" || a.profileState.Strategy != "auto" {
		t.Fatal("global defaults mutated")
	}
	stale := request(a, "PUT", "/api/sessions/"+one.ID+"/model-selection", map[string]any{"strategy": "auto", "profile": "default", "model": "model-a", "reasoningEffort": "off", "revision": 0})
	requireStatus(t, stale, 409)
	for _, patch := range []map[string]any{{"strategy": "bad", "profile": "default", "model": "model-a", "reasoningEffort": "auto"}, {"strategy": "manual", "profile": "missing", "model": "model-a", "reasoningEffort": "auto"}, {"strategy": "auto", "profile": "default", "model": "missing", "reasoningEffort": "auto"}, {"strategy": "auto", "profile": "default", "model": "model-a", "reasoningEffort": "extreme"}} {
		requireStatus(t, request(a, "PUT", "/api/sessions/"+one.ID+"/model-selection", patch), 400)
	}
	a.mu.Lock()
	path := SessionPath(a.dataPath, one.ID, sessionBucketFor(a.sessions[one.ID]))
	a.mu.Unlock()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var persisted Session
	if err = json.Unmarshal(data, &persisted); err != nil || persisted.ModelSelectionRevision != 1 || *persisted.ModelSelection != choice {
		t.Fatal("choice not persisted", err)
	}
	preview := request(a, "POST", "/api/context-preview", map[string]any{"sessionId": one.ID, "prompt": "", "baseline": true, "mode": "chat"})
	requireStatus(t, preview, 200)
	var cp ContextPreview
	_ = json.Unmarshal(preview.Body.Bytes(), &cp)
	if cp.Model != "model-b" || cp.ContextWindow != 131072 {
		t.Fatalf("preview uses wrong model: %+v", cp)
	}
}
func TestSessionModelSelectionRoutingAndSnapshots(t *testing.T) {
	a := testApp(t)
	var mu sync.Mutex
	counts := map[string]int{}
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	block := true
	provider := func(model, key string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["model"] != model || r.Header.Get("Authorization") != "Bearer "+key {
				t.Errorf("model/provider credential crossed for %s", model)
			}
			if model == "model-b" && body["reasoning_effort"] != "high" {
				t.Error("task reasoning snapshot lost")
			}
			if model == "model-a" {
				thinking, _ := body["thinking"].(map[string]any)
				if thinking["type"] != "disabled" {
					t.Error("model-a should use session off reasoning")
				}
			}
			_, main := body["tools"]
			mu.Lock()
			counts[model]++
			shouldBlock := model == "model-b" && main && block
			if shouldBlock {
				block = false
			}
			mu.Unlock()
			if shouldBlock {
				started <- struct{}{}
				<-release
			}
			parallelReply(w, "A concise answer.")
		}))
	}
	pa, pb := provider("model-a", "fixture-key-a"), provider("model-b", "fixture-key-b")
	defer pa.Close()
	defer pb.Close()
	selectionModels(a, pa.URL, pb.URL)
	for _, id := range []string{"a", "b"} {
		if err := a.vault.Put(modelProviderVaultID(id, "fixture"), VaultTypeModelAPIKey, "fixture", []byte("fixture-key-"+id), ""); err != nil {
			t.Fatal(err)
		}
	}
	s := createSession(t, a)
	choiceB := SessionModelSelection{Strategy: "manual", Profile: "precise", Model: "model-b", ReasoningEffort: "high"}
	sessionChoice(t, a, s.ID, choiceB, 0)
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{"mode": "chat", "prompt": "Explain a concept."}), 202)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("selected provider did not start")
	}
	choiceA := SessionModelSelection{Strategy: "auto", Profile: "default", Model: "model-a", ReasoningEffort: "off"}
	sessionChoice(t, a, s.ID, choiceA, 1)
	close(release)
	waitSelectionTask(t, a, s.ID)
	saved := getSelectionSession(t, a, s.ID)
	origID := saved.Runs[0].ID
	if saved.Runs[0].Model != "model-b" || *saved.Runs[0].ModelSelection != choiceB || saved.ModelSelection.Model != "model-a" {
		t.Fatal("running task changed with session")
	}
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{"mode": "chat", "prompt": "Explain another concept."}), 202)
	waitSelectionTask(t, a, s.ID)
	// A submission snapshot can legitimately differ from the latest composer choice.
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{"mode": "chat", "prompt": "Explain the captured request.", "modelSelection": choiceB}), 202)
	waitSelectionTask(t, a, s.ID)
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs/"+origID+"/retry", nil), 202)
	waitSelectionTask(t, a, s.ID)
	// Resume an original-model checkpoint after the session now chooses model-a.
	a.mu.Lock()
	orig := a.sessions[s.ID].Runs[0]
	orig.Status = "paused"
	orig.CanResume = true
	orig.CheckpointStep = "chat"
	orig.CheckpointMessages = []Message{{Role: "user", Content: "Resume a concise explanation."}}
	a.mu.Unlock()
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs/"+origID+"/resume", nil), 202)
	waitSelectionTask(t, a, s.ID)
	saved = getSelectionSession(t, a, s.ID)
	if saved.Runs[len(saved.Runs)-1].Model != "model-b" || saved.Runs[len(saved.Runs)-1].ModelSelection.ReasoningEffort != "high" {
		t.Fatal("resume lost original choices")
	}
	mu.Lock()
	defer mu.Unlock()
	if counts["model-a"] == 0 || counts["model-b"] < 3 {
		t.Fatalf("routing counts %+v", counts)
	}
}
func TestSessionModelSelectionLegacyAndVision(t *testing.T) {
	a := testApp(t)
	selectionModels(a, "http://a.invalid", "http://b.invalid")
	a.sessions["legacy"] = &Session{ID: "legacy", Runs: []*Task{{ID: "old", Model: "model-b", Strategy: "manual", Profile: "precise"}}}
	s := getSelectionSession(t, a, "legacy")
	if s.ModelSelection.Model != "model-b" || s.ModelSelection.ReasoningEffort != "auto" {
		t.Fatal("legacy choice migration failed")
	}
	image := []MessageImage{{}}
	a.mu.Lock()
	cfgB, err := a.selectionSettingsLocked(*s.ModelSelection)
	cfgA, _ := a.selectionSettingsLocked(SessionModelSelection{Strategy: "auto", Profile: "default", Model: "model-a", ReasoningEffort: "auto"})
	a.mu.Unlock()
	if err != nil || visionGateForConfig(cfgB, image) != nil || visionGateForConfig(cfgA, image) == nil {
		t.Fatal("vision gate ignores selected model")
	}
	a.settings.Models = a.settings.Models[:1]
	_, err = a.selectionSettingsLocked(*s.ModelSelection)
	if err == nil || !strings.Contains(err.Error(), "model-b") {
		t.Fatal("removed model silently substituted")
	}
}

func TestSessionModelSelectionLateConfiguration(t *testing.T) {
	a := testApp(t)
	s := createSession(t, a)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { parallelReply(w, "A concise explanation.") }))
	defer provider.Close()
	a.mu.Lock()
	a.settings.Model = "first-model"
	a.settings.ActiveModel = "first-model"
	a.settings.BaseURL = provider.URL
	a.mu.Unlock()
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{"mode": "chat", "prompt": "Explain a short concept."}), 202)
	waitSelectionTask(t, a, s.ID)
	saved := getSelectionSession(t, a, s.ID)
	if saved.ModelSelection.Model != "first-model" || saved.Runs[0].ModelSelection.Model != "first-model" || saved.ModelSelectionRevision != 1 {
		t.Fatal("first usable model was not frozen")
	}
	a.mu.Lock()
	a.settings.Model = "second-model"
	original := &Task{Model: "first-model", ModelSelection: &SessionModelSelection{Strategy: "auto", Profile: "default", Model: "", ReasoningEffort: "high"}}
	choice := a.taskModelSelectionLocked(original)
	a.mu.Unlock()
	if choice.Model != "first-model" || choice.ReasoningEffort != "high" {
		t.Fatal("legacy empty model changed an existing task")
	}
	if getSelectionSession(t, a, s.ID).ModelSelection.Model != "first-model" {
		t.Fatal("global edit changed existing session choice")
	}
}

func TestSessionModelSelectionLegacyProviderRoute(t *testing.T) {
	cfg := Settings{Model: "active", BaseURL: "http://other.invalid", Models: []ModelRef{{ID: "legacy"}}, ModelProviders: []ModelProvider{{ID: "default", BaseURL: "http://legacy.invalid"}}}
	selected, err := settingsForSelectedModel(cfg, "legacy")
	if err != nil || selected.BaseURL != "http://legacy.invalid" {
		t.Fatal("legacy default-provider route used another endpoint", err)
	}
}

func TestSessionModelSelectionSteerKeepsOriginalTask(t *testing.T) {
	a := testApp(t)
	original := &Task{ID: "running-a", Model: "a", Status: "running", Steer: make(chan string, 1), ModelSelection: &SessionModelSelection{Model: "a", Strategy: "auto", Profile: "default", ReasoningEffort: "high"}}
	a.mu.Lock()
	a.settings.Model = "a"
	a.settings.Models = []ModelRef{{ID: "a"}}
	a.sessions["steer-session"] = &Session{ID: "steer-session", ModelSelection: &SessionModelSelection{Model: "removed-b", Strategy: "auto", Profile: "default", ReasoningEffort: "off"}, Runs: []*Task{original}}
	a.mu.Unlock()
	requireStatus(t, request(a, "POST", "/api/sessions/steer-session/runs", map[string]any{"mode": "chat", "prompt": "Add a detail to the current task."}), 202)
	a.mu.Lock()
	defer a.mu.Unlock()
	if original.Model != "a" || original.ModelSelection.ReasoningEffort != "high" || len(original.Steers) != 1 || len(a.sessions["steer-session"].Runs) != 1 {
		t.Fatal("new composer choice changed an in-flight interjection")
	}
}
