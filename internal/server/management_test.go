package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func managementFixture(t *testing.T) *App {
	t.Helper()
	a := testApp(t)
	code, err := os.ReadFile(externalPluginSource(t, "plugin-authoring", "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, "POST", "/api/plugins", map[string]any{"id": "plugin-authoring", "name": "插件工坊", "version": "1.0.0", "code": string(code)}), 201)
	return a
}
func managementCall(a *App, tool string, args any, task *Task, actor string) string {
	raw, _ := json.Marshal(args)
	var call ToolCall
	call.Function.Name = tool
	call.Function.Arguments = string(raw)
	return a.executeManagementTool(context.Background(), call, task, actor)
}
func managementPrepare(t *testing.T, a *App, id, version, code string) string {
	t.Helper()
	out := managementCall(a, "manage_plugins", map[string]any{"action": "prepare", "manifest": map[string]any{"id": id, "name": "Fixture", "version": version, "main": "index.js"}, "files": map[string]string{"index.js": code}}, nil, "")
	var result struct {
		ID string `json:"proposalId"`
	}
	if json.Unmarshal([]byte(out), &result) != nil || result.ID == "" {
		t.Fatal(out)
	}
	return result.ID
}
func TestManagementPermissionsPersistAndRoles(t *testing.T) {
	a := managementFixture(t)
	if a.settings.AgentManagement != (AgentManagementPermissions{}) {
		t.Fatal("permissions must default off")
	}
	out := managementCall(a, "manage_plugins", map[string]any{"action": "disable", "id": "plugin-authoring"}, nil, "")
	if !strings.Contains(out, "管理权限未开启") {
		t.Fatal(out)
	}
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{"agentManagement": map[string]bool{"aidePlugins": true, "assistantReferences": true}}), 200)
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{"voiceAssistantName": "Fixture"}), 200)
	persisted, _ := os.ReadFile(SettingsPath(a.dataPath))
	var saved Settings
	if json.Unmarshal(persisted, &saved) != nil || !saved.AgentManagement.AidePlugins || !saved.AgentManagement.AssistantReferences {
		t.Fatal("permissions not persisted")
	}
	want := AgentManagementPermissions{AidePlugins: true, AssistantReferences: true}
	if a.settings.AgentManagement != want {
		t.Fatal("unrelated settings reset permissions")
	}
	config := request(a, "GET", "/api/config", nil)
	if !strings.Contains(config.Body.String(), `"aidePlugins":true`) {
		t.Fatal(config.Body.String())
	}
	a.mu.Lock()
	if a.managementActorLocked(&Task{AssistantManaged: true}, "") != "assistant" || a.managementActorLocked(nil, "assistant") != "assistant" {
		t.Fatal("delegation bypass")
	}
	a.mu.Unlock()
	out = managementCall(a, "manage_plugins", map[string]any{"action": "disable", "id": "plugin-authoring"}, &Task{AssistantManaged: true}, "")
	if !strings.Contains(out, "管理权限未开启") {
		t.Fatal(out)
	}
	// An enabled ordinary tool remains callable while all management permissions are off.
	requireStatus(t, request(a, "POST", "/api/plugins", map[string]any{"id": "daily-tool", "name": "Daily", "code": upgradeOldCode}), 201)
	a.settings.AgentManagement = AgentManagementPermissions{}
	if _, err := a.callPluginTool("daily-tool", "upgrade_fixture_tool", nil); err != nil {
		t.Fatal(err)
	}
}
func TestAuthoringPrepareDoesNotExecuteAndApprovalInstalls(t *testing.T) {
	a := managementFixture(t)
	a.settings.AgentManagement.AidePlugins = true
	marker := filepath.Join(t.TempDir(), "executed")
	literal, _ := json.Marshal(marker)
	code := `require('fs').writeFileSync(` + string(literal) + `, 'executed');` + upgradeOldCode
	id := managementPrepare(t, a, "made-plugin", "1.0.0", code)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("prepare executed generated code")
	}
	if _, err := os.Stat(filepath.Join(a.pluginsPath, "made-plugin")); !os.IsNotExist(err) {
		t.Fatal("installed without confirmation")
	}
	w := request(a, "GET", "/api/management-proposals/"+id+"/package", nil)
	requireStatus(t, w, 200)
	if _, _, err := parsePluginBundle(w.Body.Bytes()); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, "POST", "/api/management-proposals/"+id+"/apply", nil), 200)
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("manual installation did not validate/register", err)
	}
	if _, err := a.callPluginTool("made-plugin", "upgrade_fixture_tool", nil); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, "POST", "/api/management-proposals/"+id+"/apply", nil), 409)
}
func TestManagementUpgradePreservesSettingsAndDisabledState(t *testing.T) {
	a, id := upgradeFixture(t)
	code, _ := os.ReadFile(externalPluginSource(t, "plugin-authoring", "index.js"))
	requireStatus(t, request(a, "POST", "/api/plugins", map[string]any{"id": "plugin-authoring", "name": "Workshop", "code": string(code)}), 201)
	a.settings.AgentManagement.AidePlugins = true
	requireStatus(t, request(a, "PUT", "/api/plugins/"+id, map[string]any{"enabled": false}), 200)
	proposal := managementPrepare(t, a, id, "2.0.0", strings.ReplaceAll(upgradeOldCode, "'old'", "'new'"))
	requireStatus(t, request(a, "POST", "/api/management-proposals/"+proposal+"/apply", nil), 200)
	for _, p := range a.pluginRegistry.Plugins {
		if p.ID == id && (p.Enabled || p.Version != "2.0.0" || p.Settings["marker"] != "keep") {
			t.Fatalf("state lost: %+v", p)
		}
	}
}
func TestManagementRevocationAndStaleState(t *testing.T) {
	for _, scenario := range []string{"permission", "workspace", "source-code"} {
		t.Run(scenario, func(t *testing.T) {
			a := managementFixture(t)
			a.settings.AgentManagement.AidePlugins = true
			id := managementPrepare(t, a, "managed-new", "1.0.0", upgradeOldCode)
			switch scenario {
			case "permission":
				a.settings.AgentManagement.AidePlugins = false
			case "workspace":
				a.wsRevision++
			case "source-code":
				os.MkdirAll(filepath.Join(a.pluginsPath, "managed-new"), 0700)
				os.WriteFile(filepath.Join(a.pluginsPath, "managed-new", "index.js"), []byte("changed"), 0600)
			}
			requireStatus(t, request(a, "POST", "/api/management-proposals/"+id+"/apply", nil), 409)
		})
	}
}
func TestManagementReferenceLocksAndOrdinaryAccess(t *testing.T) {
	a := managementFixture(t)
	a.settings.AgentManagement.AideReferences = true
	dir := "/workspace/managed-reference"
	os.MkdirAll(filepath.Join(a.workPath, "managed-reference"), 0700)
	a.sourceRegistry.Sources = append(a.sourceRegistry.Sources, Source{ID: "chosen", Name: "Chosen", Type: "local", Enabled: true, RW: false, AIHiddenWorkspaces: []string{a.wsID()}, Config: SourceConfig{Path: dir}})
	raw := managementCall(a, "manage_references", map[string]any{"action": "upsert", "id": "chosen", "name": "Renamed", "type": "local", "config": map[string]any{"path": dir}}, nil, "")
	var p struct {
		ID string `json:"proposalId"`
	}
	json.Unmarshal([]byte(raw), &p)
	if p.ID == "" {
		t.Fatal(raw)
	}
	requireStatus(t, request(a, "POST", "/api/management-proposals/"+p.ID+"/apply", nil), 200)
	for _, s := range a.sourceRegistry.Sources {
		if s.ID == "chosen" && (s.RW || sourceAIVisible(s, a.wsID()) || s.Name != "Renamed") {
			t.Fatalf("locks lost: %+v", s)
		}
	}
	raw = managementCall(a, "manage_references", map[string]any{"action": "upsert", "id": "new-reference", "name": "New", "type": "local", "config": map[string]any{"path": dir}}, nil, "")
	json.Unmarshal([]byte(raw), &p)
	requireStatus(t, request(a, "POST", "/api/management-proposals/"+p.ID+"/apply", nil), 200)
	for _, s := range a.sourceRegistry.Sources {
		if s.ID == "new-reference" && s.RW {
			t.Fatal("new reference unlocked")
		}
	}
	for _, args := range []map[string]any{
		{"action": "remove", "id": systemDocsSource},
		{"action": "upsert", "id": "bad", "name": "Bad", "type": "mcp", "config": map[string]any{"command": "sh"}},
		{"action": "upsert", "id": "bad", "name": "Bad", "type": "sftp", "config": map[string]any{"host": "example", "auth": "password"}},
	} {
		if out := managementCall(a, "manage_references", args, nil, ""); strings.Contains(out, "proposalId") {
			t.Fatal(out)
		}
	}
	a.settings.AgentManagement.AideReferences = false
	if out := managementCall(a, "manage_references", map[string]any{"action": "list"}, nil, ""); strings.Contains(out, `"id":"chosen"`) {
		t.Fatal("hidden reference leaked")
	}
}
func TestAuthoringRejectsInvalidCodeAndPaths(t *testing.T) {
	m := map[string]any{"id": "test-code", "name": "Test", "main": "index.js", "version": "1.0.0"}
	for _, name := range []string{"../escape.js", "/tmp/escape.js", "a/../index.js", "manifest.json", "C:\\escape.js"} {
		if _, err := authoringBundle(m, map[string]string{name: "x"}); err == nil {
			t.Fatal(name)
		}
	}
	raw, err := authoringBundle(m, map[string]string{"index.js": "const = broken"})
	if err != nil {
		t.Fatal(err)
	}
	if validateAuthoringSyntax(context.Background(), raw) == nil {
		t.Fatal("invalid JS accepted")
	}
}
