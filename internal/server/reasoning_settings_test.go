package server

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestReasoningDefaultWithoutModelAndReload(t *testing.T) {
	a := testApp(t)
	for _, effort := range []string{"high", "medium", "low", "off", "auto"} {
		requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{"reasoningEffort": effort}), 200)
		w := request(a, "GET", "/api/config", nil)
		requireStatus(t, w, 200)
		var cfg map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
			t.Fatal(err)
		}
		if cfg["reasoningEffort"] != effort || cfg["configured"] != false || cfg["model"] != "" {
			t.Fatalf("pre-model preference: %v", cfg)
		}
	}
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{"reasoningEffort": "invalid"}), 400)
	if a.settings.ReasoningEffort != "auto" {
		t.Fatal("invalid value mutated settings")
	}
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{"reasoningEffort": "high"}), 200)
	work, data := a.workspace.Name(), a.dataPath
	a.Close()
	reloaded, err := New(work, filepath.Join(t.TempDir(), "reference"), data)
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.Close()
	if reloaded.settings.ReasoningEffort != "high" || reloaded.settings.Model != "" || len(reloaded.settings.Models) != 0 {
		t.Fatal("reload lost default or created a model")
	}
}

func TestReasoningDefaultPreservesSessionChoice(t *testing.T) {
	a := testApp(t)
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{"baseURL": "https://fixture.invalid/v1", "model": "fixture-model"}), 200)
	s := createSession(t, a)
	requireStatus(t, request(a, "PUT", "/api/sessions/"+s.ID+"/model-selection", map[string]any{"strategy": "auto", "profile": "default", "model": "fixture-model", "reasoningEffort": "high", "revision": 0}), 200)
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{"reasoningEffort": "off"}), 200)
	w := request(a, "GET", "/api/sessions/"+s.ID, nil)
	requireStatus(t, w, 200)
	var saved Session
	if err := json.Unmarshal(w.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if a.settings.Model != "fixture-model" || a.settings.ReasoningEffort != "off" || saved.ModelSelection == nil || saved.ModelSelection.ReasoningEffort != "high" {
		t.Fatal("global preference overwrote model or session choice")
	}
}
