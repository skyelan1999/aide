package server

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestCustodyKeysStayEncryptedAndMetadataOnly(t *testing.T) {
	a := testApp(t)
	secret := "synthetic-custody-key-never-send-001"
	w := request(a, "PUT", "/api/account/custody-keys", map[string]any{"name": "Acceptance fixture", "secret": secret})
	requireStatus(t, w, 200)
	if strings.Contains(w.Body.String(), secret) {
		t.Fatal("save response returned custody key plaintext")
	}
	var saved struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &saved); err != nil || saved.ID == "" {
		t.Fatalf("invalid save response: %v %s", err, w.Body.String())
	}
	plain, err := a.vault.Get(saved.ID)
	if err != nil {
		t.Fatalf("vault did not retain encrypted entry: %v", err)
	}
	if string(plain) != secret {
		t.Fatal("vault value mismatch")
	}
	zeroBytes(plain)
	data := request(a, "GET", "/api/account/custody-keys", nil)
	requireStatus(t, data, 200)
	if strings.Contains(data.Body.String(), secret) || !strings.Contains(data.Body.String(), "Acceptance fixture") {
		t.Fatalf("list must expose metadata only: %s", data.Body.String())
	}
	for _, endpoint := range []string{"/api/config", "/api/account/custody-keys"} {
		check := request(a, "GET", endpoint, nil)
		if strings.Contains(check.Body.String(), secret) {
			t.Fatalf("%s leaked plaintext", endpoint)
		}
	}
	vaultBytes, err := os.ReadFile(a.vault.Path())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(vaultBytes), secret) {
		t.Fatal("vault file contains plaintext")
	}

	backup := request(a, "POST", "/api/config/export", map[string]any{"includeSecrets": true})
	requireStatus(t, backup, 200)
	if strings.Contains(backup.Body.String(), secret) || strings.Contains(backup.Body.String(), VaultTypeCustodyKey) {
		t.Fatalf("config backup transported local-only key: %s", backup.Body.String())
	}

	del := request(a, "DELETE", "/api/account/custody-keys/"+saved.ID, nil)
	requireStatus(t, del, 200)
	if a.vault.Has(saved.ID) {
		t.Fatal("custody key was not deleted")
	}
}

func TestCustodyKeysRequireUnlockedVaultAndRespectNamespace(t *testing.T) {
	a := testApp(t)
	a.vault.Lock()
	w := request(a, "PUT", "/api/account/custody-keys", map[string]any{"name": "fixture", "secret": "synthetic"})
	requireStatus(t, w, 423)
	if strings.Contains(w.Body.String(), "synthetic") {
		t.Fatal("locked response leaked key")
	}

	// Even a valid vault entry in another namespace cannot be deleted through this API.
	a.vault.Unlock(a.accessTokenVaultKey())
	if err := a.vault.Put(VaultIDModelAPIKey, VaultTypeModelAPIKey, "model", []byte("model-fixture"), ""); err != nil {
		t.Fatal(err)
	}
	if err := a.vault.Save(); err != nil {
		t.Fatal(err)
	}
	del := request(a, "DELETE", "/api/account/custody-keys/"+VaultIDModelAPIKey, nil)
	requireStatus(t, del, 404)
	if !a.vault.Has(VaultIDModelAPIKey) {
		t.Fatal("custody endpoint deleted a model key")
	}
}
