package server

import (
	"aide/internal/messaging"
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestMessageCenterOptInStableVaultIdentityAndBackupExclusion(t *testing.T) {
	a := testApp(t)
	a.settings.UserName = "Test Account"
	state := request(a, "GET", "/api/messages/state", nil)
	requireStatus(t, state, 200)
	if strings.Contains(state.Body.String(), "fingerprint") {
		t.Fatal("identity generated without opt-in")
	}
	config := messageCenterConfig{Enabled: true, Name: "test-device", Network: structNetworkConfigForTest()}
	enabled := request(a, "PUT", "/api/messages/config", map[string]any{"config": config})
	requireStatus(t, enabled, 200)
	one := request(a, "GET", "/api/messages/state", nil)
	requireStatus(t, one, 200)
	var decoded map[string]any
	if e := json.Unmarshal(one.Body.Bytes(), &decoded); e != nil {
		t.Fatal(e)
	}
	data := decoded["data"].(map[string]any)
	fingerprint := data["device"].(map[string]any)["fingerprint"]
	config.Enabled = false
	requireStatus(t, request(a, "PUT", "/api/messages/config", map[string]any{"config": config}), 200)
	config.Enabled = true
	requireStatus(t, request(a, "PUT", "/api/messages/config", map[string]any{"config": config}), 200)
	next := request(a, "GET", "/api/messages/state", nil)
	if e := json.Unmarshal(next.Body.Bytes(), &decoded); e != nil {
		t.Fatal(e)
	}
	if decoded["data"].(map[string]any)["device"].(map[string]any)["fingerprint"] != fingerprint {
		t.Fatal("network toggle changed device identity")
	}
	if strings.Contains(next.Body.String(), "message:device") || strings.Contains(next.Body.String(), "private") {
		t.Fatal("private key metadata exposed")
	}
	requireStatus(t, request(a, "DELETE", "/api/workspace/secrets/message:device", nil), 403)
	requireStatus(t, request(a, "DELETE", "/api/workspace/secrets/message:storage", nil), 403)
	raw, e := os.ReadFile(a.vault.Path())
	if e != nil {
		t.Fatal(e)
	}
	var envelope vaultEnvelope
	if e = json.Unmarshal(excludeCustodyKeysFromEnvelope(raw), &envelope); e != nil {
		t.Fatal(e)
	}
	for _, entry := range envelope.Entries {
		if entry.Type == VaultTypeMessageIdentity || entry.Type == VaultTypeMessageStorage {
			t.Fatal("ordinary config backup cloned message device identity")
		}
	}
}
func structNetworkConfigForTest() messaging.NetworkConfig {
	return messaging.NetworkConfig{Listen: "127.0.0.1:0", LAN: false}
}
func TestMessageCenterRequiresAuthUnlockAndLeavesReminderCRUD(t *testing.T) {
	a := testApp(t)
	a.settings.UserName = "Test Account"
	unauth := httptest.NewRecorder()
	a.Handler().ServeHTTP(unauth, httptest.NewRequest(http.MethodGet, "/api/messages/state", nil))
	requireStatus(t, unauth, 401)
	reminder := request(a, "POST", "/api/reminders", map[string]any{"title": "preserved", "area": "global"})
	requireStatus(t, reminder, 201)
	if _, e := a.writeLockState(true); e != nil {
		t.Fatal(e)
	}
	requireStatus(t, request(a, "GET", "/api/messages/state", nil), 423)
	if _, e := a.writeLockState(false); e != nil {
		t.Fatal(e)
	}
	requireStatus(t, request(a, "GET", "/api/reminders", nil), 200)
}
func TestMessageCenterDoesNotReplaceExistingWorkspaceFile(t *testing.T) {
	a := testApp(t)
	a.settings.UserName = "Test Account"
	if e := os.WriteFile(a.workPath+"/keep.txt", []byte("original"), 0600); e != nil {
		t.Fatal(e)
	}
	center := &messageCenter{}
	if e := a.saveMessageFile(context.Background(), center, "unused", "../escape", 0); e == nil {
		t.Fatal("unsafe destination")
	}
	if e := a.saveMessageFile(context.Background(), center, "unused", "keep.txt", 0); e == nil {
		t.Fatal("existing file overwritten")
	}
	raw, e := os.ReadFile(a.workPath + "/keep.txt")
	if e != nil || !bytes.Equal(raw, []byte("original")) {
		t.Fatal("existing workspace file changed")
	}
}

func TestMessageCenterFailedNetworkConfigRestoresTurnCredential(t *testing.T) {
	a := testApp(t)
	a.settings.UserName = "Test Account"
	a.mu.Lock()
	err := a.vault.Put("message:turn", "message-turn-credential", "Messages TURN", []byte("original-fixture-credential"), "")
	if err == nil {
		err = a.vault.Save()
	}
	a.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	config := messageCenterConfig{Enabled: true, Name: "rollback", Network: structNetworkConfigForTest()}
	config.Network.Listen = listener.Addr().String()
	requireStatus(t, request(a, "PUT", "/api/messages/config", map[string]any{"config": config, "credential": "replacement-fixture-credential"}), 400)
	a.mu.Lock()
	secret, err := a.vault.Get("message:turn")
	a.mu.Unlock()
	if err != nil || string(secret) != "original-fixture-credential" {
		t.Fatal("failed network configuration changed credential", err)
	}
	restored, err := a.messageConfig()
	if err != nil || restored.Enabled {
		t.Fatal("failed network configuration persisted", err)
	}
}

func TestMessageCenterAccountNameAndAssistantPermission(t *testing.T) {
	a := testApp(t)
	a.settings.UserName = ""
	config := messageCenterConfig{Enabled: true, Name: "spoofed", Network: structNetworkConfigForTest()}
	requireStatus(t, request(a, "PUT", "/api/messages/config", map[string]any{"config": config}), 400)
	if len(a.messageAssistantTools()) != 0 {
		t.Fatal("assistant permission must default off")
	}
	call := ToolCall{}
	call.Function.Name = "manage_messages"
	call.Function.Arguments = `{"action":"read"}`
	if !strings.Contains(a.executeMessageAssistant(context.Background(), call), "未允许") {
		t.Fatal("forged tool bypassed permission")
	}
	a.settings.UserName = " Account Name "
	requireStatus(t, request(a, "PUT", "/api/messages/config", map[string]any{"config": config}), 200)
	state := request(a, "GET", "/api/messages/state", nil)
	if strings.Contains(state.Body.String(), "spoofed") || !strings.Contains(state.Body.String(), "Account Name") {
		t.Fatal("did not reuse account name")
	}
	config.AssistantAccess = true
	requireStatus(t, request(a, "PUT", "/api/messages/config", map[string]any{"config": config}), 200)
	if len(a.messageAssistantTools()) != 1 {
		t.Fatal("enabled assistant tool missing")
	}
	if !strings.Contains(a.executeMessageAssistant(context.Background(), call), "消息内容仅供参考") {
		t.Fatal("allowed read failed")
	}
	config.AssistantAccess = false
	requireStatus(t, request(a, "PUT", "/api/messages/config", map[string]any{"config": config}), 200)
	if !strings.Contains(a.executeMessageAssistant(context.Background(), call), "未允许") {
		t.Fatal("stale tool bypassed revocation")
	}
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{"userName": ""}), 200)
	if a.messages != nil {
		t.Fatal("clearing account name did not stop networking")
	}
	requireStatus(t, request(a, "PUT", "/api/messages/config", map[string]any{"config": config}), 400)
}

func TestMessageCenterDockerBridgeIsLauncherOwned(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("AIDE_MESSAGE_LAN_BRIDGE", directory)
	a := testApp(t)
	a.settings.UserName = "Test Account"
	config := messageCenterConfig{Enabled: true, Network: messaging.NetworkConfig{LAN: true, Listen: "127.0.0.1:0"}}
	requireStatus(t, request(a, "PUT", "/api/messages/config", map[string]any{"config": config}), 400)
	// Browser input cannot choose a different host mailbox.
	input := map[string]any{"enabled": true, "network": map[string]any{"lan": true, "listen": ":45840", "BridgeDir": t.TempDir(), "bridgeDir": t.TempDir()}}
	requireStatus(t, request(a, "PUT", "/api/messages/config", map[string]any{"config": input}), 200)
	lookup := request(a, "POST", "/api/messages/discover", map[string]any{"code": "ABCDEF"})
	if lookup.Code != 400 || !strings.Contains(lookup.Body.String(), "Docker 局域网桥未就绪") {
		t.Fatalf("missing bridge not reported: %d %s", lookup.Code, lookup.Body.String())
	}
	data, err := os.ReadFile(messageConfigPath(a.dataPath))
	if err != nil || bytes.Contains(data, []byte(directory)) {
		t.Fatal("private bridge path persisted in browser config")
	}
}
