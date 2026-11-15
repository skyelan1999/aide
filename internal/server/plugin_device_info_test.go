package server

import (
	"aide/internal/messaging"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPluginDeviceInfoExistingIdentityOnly(t *testing.T) {
	a := testApp(t)
	before, err := a.pluginDeviceInfo()
	if err != nil || before["available"] != false || len(before) != 2 {
		t.Fatal(before, err)
	}
	// Repeated metadata reads must not create the messaging identity.
	again, err := a.pluginDeviceInfo()
	if err != nil || again["available"] != false || a.messages != nil {
		t.Fatal("metadata opted into messaging", again, err)
	}
	seed, err := a.messageVaultSecret("message:device", VaultTypeMessageIdentity)
	if err != nil {
		t.Fatal(err)
	}
	defer zeroBytes(seed)
	identity, _ := messaging.FromSeed(seed)
	got, err := a.pluginDeviceInfo()
	if err != nil || got["code"] != identity.Device().Code || got["available"] != true || len(got) != 2 {
		t.Fatal("wrong or excessive public metadata", got, err)
	}
	if a.messages != nil {
		t.Fatal("reading an existing code enabled communication")
	}
}

func TestPluginDeviceInfoThroughRealNodeHost(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Fatal("Node is required to verify the plugin device-info API")
	}
	a := testApp(t)
	seed, err := a.messageVaultSecret("message:device", VaultTypeMessageIdentity)
	if err != nil {
		t.Fatal(err)
	}
	defer zeroBytes(seed)
	identity, _ := messaging.FromSeed(seed)
	socket, closeSources, _, err := a.startPluginSources(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer closeSources()
	root := t.TempDir()
	plugin := filepath.Join(root, "fixture")
	if err := os.Mkdir(plugin, 0700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"manifest.json": `{"id":"fixture","main":"index.js"}`,
		"index.js":      `module.exports={apply(ctx){ctx.tool({name:'probe',handler:async(args,api)=>({info:await api.deviceInfo(),capability:api.capabilities.includes('plugin-device-info-v1')})});}}`,
	} {
		if err := os.WriteFile(filepath.Join(plugin, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	request, _ := json.Marshal(map[string]any{"plugin": "fixture", "tool": "probe", "args": map[string]any{}, "workspace": a.workPath, "sourceSocket": socket})
	output := filepath.Join(root, "out.json")
	if raw, err := exec.Command("node", "-e", pluginHostJS, "call", root, string(request), output).CombinedOutput(); err != nil {
		t.Fatal(err, string(raw))
	}
	raw, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		OK     bool `json:"ok"`
		Result struct {
			Info       map[string]any `json:"info"`
			Capability bool           `json:"capability"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &result); err != nil || !result.OK || !result.Result.Capability || len(result.Result.Info) != 2 || result.Result.Info["code"] != identity.Device().Code {
		t.Fatal(string(raw), err)
	}
	for _, forbidden := range []string{"fingerprint", "publicKey", "message:device", "seed", "contacts"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatal("excess metadata exposed", forbidden)
		}
	}
	if _, err := a.writeLockState(true); err != nil {
		t.Fatal(err)
	}
	if _, err := exec.Command("node", "-e", pluginHostJS, "call", root, string(request), output).CombinedOutput(); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(output)
	if err != nil || !strings.Contains(string(raw), `"ok":false`) || strings.Contains(string(raw), identity.Device().Code) {
		t.Fatal("revoked source capability returned device info", string(raw), err)
	}
}
