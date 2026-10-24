package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func installTestAssistantAgent(t *testing.T, a *App, code string) {
	t.Helper()
	dir := filepath.Join(a.pluginsPath, "test-agent")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.js"), []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.pluginRegistry.Plugins = append(a.pluginRegistry.Plugins, PluginManifest{ID: "test-agent", Name: "Test agent", Main: "index.js", Enabled: true, AssistantAgent: &PluginAssistantAgent{Protocol: 1}})
	a.mu.Unlock()
	unlockAssistantSessionForTest(t, a)
}
func TestAssistantAgentReplacementAndDisable(t *testing.T) {
	a := testApp(t)
	installTestAssistantAgent(t, a, `module.exports={apply(){},assistantAgent:async input=>({action:'finish',reply:'replacement-only'})}`)
	var calls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"builtin-only"}}]}`))
	}))
	defer model.Close()
	cfg := Settings{BaseURL: model.URL, Model: "test"}
	decision, err := a.runAssistantAgenticLoop(context.Background(), cfg, "synthetic input", "", "text")
	if err != nil || decision.Reply != "replacement-only" || calls.Load() != 0 {
		t.Fatalf("takeover failed: %v %v %d", decision, err, calls.Load())
	}
	a.mu.Lock()
	a.pluginRegistry.Plugins[0].Enabled = false
	a.mu.Unlock()
	decision, err = a.runAssistantAgenticLoop(context.Background(), cfg, "synthetic input", "", "text")
	if err != nil || decision.Reply != "builtin-only" || calls.Load() != 1 {
		t.Fatalf("restore failed: %v %v %d", decision, err, calls.Load())
	}
}
func TestAssistantAgentLockDuplicateAndSecretIPC(t *testing.T) {
	a := testApp(t)
	installTestAssistantAgent(t, a, `module.exports={apply(){},assistantAgent:async (input,api)=>({action:'finish',reply:api.storage.key.length===44?'private-context-ready':'bad'})}`)
	b, err := a.selectedAssistantAgent()
	if err != nil {
		t.Fatal(err)
	}
	got, err := a.assistantAgentCall(context.Background(), *b, map[string]any{"phase": "pulse"})
	if err != nil || got.Reply != "private-context-ready" {
		t.Fatal(got, err)
	}
	a.clearAssistantUnlock()
	if _, err = a.assistantAgentCall(context.Background(), *b, map[string]any{}); err == nil {
		t.Fatal("locked context allowed")
	}
	a.mu.Lock()
	a.pluginRegistry.Plugins = append(a.pluginRegistry.Plugins, PluginManifest{ID: "another", Enabled: true, AssistantAgent: &PluginAssistantAgent{Protocol: 1}})
	a.mu.Unlock()
	if _, err = a.selectedAssistantAgent(); err == nil {
		t.Fatal("duplicate providers accepted")
	}
}
func TestAssistantAgentRejectsToolsAndCancelsDisable(t *testing.T) {
	a := testApp(t)
	installTestAssistantAgent(t, a, `module.exports={apply(){},assistantAgent:async input=>input.phase==='slow'?new Promise(r=>setTimeout(()=>r({action:'finish',reply:'late'}),2000)):{action:'generate',messages:[{role:'user',content:'synthetic'}],tools:['run_shell']}}`)
	if _, err := a.runAssistantAgenticLoop(context.Background(), Settings{}, "synthetic", "", "text"); err == nil || !strings.Contains(err.Error(), "unavailable tool") {
		t.Fatal(err)
	}
	b, _ := a.selectedAssistantAgent()
	done := make(chan error, 1)
	go func() {
		_, e := a.assistantAgentCall(context.Background(), *b, map[string]any{"phase": "slow"})
		done <- e
	}()
	time.Sleep(150 * time.Millisecond)
	a.mu.Lock()
	a.pluginRegistry.Plugins[0].Enabled = false
	a.mu.Unlock()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("stale reply accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("disable failed to cancel")
	}
}
func TestAssistantAgentFailureDoesNotDispatchFallback(t *testing.T) {
	a := testApp(t)
	installTestAssistantAgent(t, a, `module.exports={apply(){},assistantAgent:async()=>{throw Error('synthetic failure')}}`)
	a.mu.Lock()
	a.settings.BaseURL = "http://127.0.0.1:1"
	a.settings.Model = "synthetic"
	before := len(a.sessions)
	a.mu.Unlock()
	response := request(a, "POST", "/api/sessions/"+assistantSessionID(t, a)+"/assistant-message", map[string]any{"text": "synthetic direct message"})
	if response.Code != 503 {
		t.Fatalf("active provider error status %d", response.Code)
	}
	a.mu.Lock()
	after := len(a.sessions)
	a.mu.Unlock()
	if after != before {
		t.Fatal("failed provider silently dispatched")
	}
}
func TestAssistantAgentWorkspaceAndSettingsInvalidate(t *testing.T) {
	a := testApp(t)
	installTestAssistantAgent(t, a, `module.exports={apply(){},assistantAgent:async()=>({action:'finish',reply:'test'})}`)
	b, _ := a.selectedAssistantAgent()
	a.mu.Lock()
	a.pluginRegistry.Plugins[0].Settings = map[string]any{"changed": true}
	a.mu.Unlock()
	if a.assistantAgentCurrent(*b) {
		t.Fatal("settings change accepted stale binding")
	}
	b, _ = a.selectedAssistantAgent()
	a.mu.Lock()
	a.wsRevision++
	a.mu.Unlock()
	if a.assistantAgentCurrent(*b) {
		t.Fatal("workspace revision change accepted stale binding")
	}
}
