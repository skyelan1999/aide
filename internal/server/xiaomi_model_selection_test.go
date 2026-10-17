package server

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestXiaomiConfiguredModelSelection(t *testing.T) {
	a := testApp(t)
	selectionModels(a, "https://provider-a.invalid/v1", "https://provider-b.invalid/v1")
	for _, id := range []string{"a", "b"} {
		if err := a.vault.Put(modelProviderVaultID(id, "fixture"), VaultTypeModelAPIKey, "fixture", []byte("fixture-key-"+id), ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.vault.Put(VaultIDXiaomiModelAPIKey, VaultTypeModelAPIKey, "custom", []byte("custom-fixture-key"), ""); err != nil {
		t.Fatal(err)
	}
	w := request(a, "PUT", "/api/xiaomi/model", map[string]any{"source": "configured", "model": "model-b", "apiKey": "must-not-be-stored", "clearKey": true})
	requireStatus(t, w, 200)
	if strings.Contains(w.Body.String(), "fixture-key") {
		t.Fatal("credential in response")
	}
	a.mu.Lock()
	cfg, err := a.xiaomiModelConfigLocked()
	a.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "model-b" || cfg.BaseURL != "https://provider-b.invalid/v1" || cfg.APIKey != "fixture-key-b" || cfg.ActiveModel != "model-b" {
		t.Fatalf("wrong selected model/provider/credential: %s %s", cfg.Model, cfg.BaseURL)
	}
	if a.settings.Model != "model-a" || a.settings.ActiveModel != "model-a" {
		t.Fatal("changed workbench default")
	}
	key, _ := a.vault.Get(VaultIDXiaomiModelAPIKey)
	if string(key) != "custom-fixture-key" {
		t.Fatal("custom credential changed by configured selection")
	}
	data, err := os.ReadFile(SettingsPath(a.dataPath))
	if err != nil {
		t.Fatal(err)
	}
	var saved Settings
	if err = json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.XiaomiModelSource != "configured" || saved.XiaomiModel != "model-b" || strings.Contains(string(data), "fixture-key") {
		t.Fatal("selection not safely persisted")
	}
	// Changing the workbench default must not retarget the assistant.
	a.mu.Lock()
	a.settings.Model = "model-b"
	a.settings.ActiveModel = "model-b"
	a.settings.XiaomiModel = "model-a"
	cfg, err = a.xiaomiModelConfigLocked()
	a.mu.Unlock()
	if err != nil || cfg.Model != "model-a" || cfg.APIKey != "fixture-key-a" {
		t.Fatal("assistant did not retain its own configured selection", err)
	}
	requireStatus(t, request(a, "PUT", "/api/xiaomi/model", map[string]any{"source": "configured", "model": "missing"}), 400)
	if a.settings.XiaomiModel != "model-a" {
		t.Fatal("invalid selection mutated settings")
	}
	a.mu.Lock()
	a.settings.Models = nil
	_, err = a.xiaomiModelConfigLocked()
	a.mu.Unlock()
	if err == nil {
		t.Fatal("removed configured model silently inherited the default")
	}
}

func TestXiaomiModelSourceCompatibility(t *testing.T) {
	a := testApp(t)
	selectionModels(a, "https://provider-a.invalid/v1", "https://provider-b.invalid/v1")
	requireStatus(t, request(a, "PUT", "/api/xiaomi/model", map[string]any{"source": "custom", "baseURL": "https://custom.invalid/v1", "model": "custom-model", "apiKey": "fixture-custom-key"}), 200)
	a.mu.Lock()
	cfg, err := a.xiaomiModelConfigLocked()
	a.mu.Unlock()
	if err != nil || cfg.Model != "custom-model" || cfg.APIKey != "fixture-custom-key" {
		t.Fatal("custom source regressed", err)
	}
	requireStatus(t, request(a, "PUT", "/api/xiaomi/model", map[string]any{"source": "inherit"}), 200)
	a.mu.Lock()
	cfg, err = a.xiaomiModelConfigLocked()
	a.mu.Unlock()
	if err != nil || cfg.Model != "model-a" {
		t.Fatal("inherit source regressed", err)
	}
	requireStatus(t, request(a, "PUT", "/api/xiaomi/model", map[string]any{"source": "configured", "model": "model-b"}), 200)
	a.mu.Lock()
	a.vault.Lock()
	a.mu.Unlock()
	// No selected-provider key exists yet: local/no-key configurations remain valid.
	a.mu.Lock()
	_, err = a.xiaomiModelConfigLocked()
	a.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
}
