package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkbenchManualSaveConflictAndProposalSeparation(t *testing.T) {
	a, key := bridgeFixture(t)
	// Declare a native project extension, then reopen to get an unchanged digest.
	for i := range a.pluginRegistry.Plugins {
		if a.pluginRegistry.Plugins[i].ID == "workbench-fixture" {
			a.pluginRegistry.Plugins[i].Workbench.FileExtensions = []string{".pp"}
		}
	}
	opened := request(a, "POST", "/api/plugins/workbench-fixture/workbench/open", map[string]any{})
	requireStatus(t, opened, 200)
	var registration struct {
		URL    string `json:"url"`
		Bridge struct {
			Key string `json:"instanceId"`
		} `json:"bridge"`
	}
	json.Unmarshal(opened.Body.Bytes(), &registration)
	key = registration.Bridge.Key
	requireStatus(t, workbenchRequest(a, "GET", registration.URL, false), 200)

	invoke := func(id, method string, args map[string]any) int {
		w := request(a, "POST", "/api/plugins/workbench-fixture/workbench/"+key+"/call", map[string]any{"protocol": 1, "requestId": id, "method": method, "arguments": args})
		return w.Code
	}
	if c := invoke("target", "file.target", map[string]any{"path": "manual.pp"}); c != 200 {
		t.Fatal(c)
	}
	if c := invoke("save", "file.save.commit", map[string]any{"path": "manual.pp", "content": "project", "hash": ""}); c != 200 {
		t.Fatal(c)
	}
	b, e := os.ReadFile(filepath.Join(a.workPath, "manual.pp"))
	if e != nil || string(b) != "project" {
		t.Fatal(e, string(b))
	}
	if c := invoke("conflict", "file.save.commit", map[string]any{"path": "manual.pp", "content": "lost", "hash": ""}); c != 409 {
		t.Fatal(c)
	}
	if c := invoke("extension", "file.save.commit", map[string]any{"path": "other.txt", "content": "bad", "hash": ""}); c != 403 {
		t.Fatal(c)
	}
	if c := invoke("escape", "file.target", map[string]any{"path": "../other.pp"}); c != 400 {
		t.Fatal(c)
	}
	// An ordinary AI-style tool call still creates a proposal and never writes.
	w := bridgeCall(a, key, "proposal", "bridge_write", map[string]any{"path": "ai.pp", "content": "proposal"})
	requireStatus(t, w, 200)
	var v map[string]any
	json.Unmarshal(w.Body.Bytes(), &v)
	if v["status"] != "proposal_pending" {
		t.Fatal(w.Body.String())
	}
	if _, e := os.Stat(filepath.Join(a.workPath, "ai.pp")); !os.IsNotExist(e) {
		t.Fatal("AI proposal bypassed approval")
	}
	a.mu.Lock()
	a.pluginWorkbenchBridges[key].Ticket.Gen--
	a.mu.Unlock()
	if c := invoke("revoked", "file.target", map[string]any{"path": "manual.pp"}); c != 403 {
		t.Fatal(c)
	}
}
