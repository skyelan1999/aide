package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Exercise the real Node host and Go dispatch alongside the authenticated
// dashboard routes. Readability observations must not become tool success.
func TestCapabilityPluginLiveLifecycle(t *testing.T) {
	a := testApp(t)
	code, err := os.ReadFile(filepath.Join("..", "..", "plugins", "current-time", "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, "POST", "/api/plugins", map[string]any{
		"id": "current-time", "name": "Clock acceptance", "code": string(code),
	}), 201)
	snapshot := func() (string, CapabilityItem) {
		workspace, items := capabilitySnapshot(t, a)
		return workspace, capabilityFind(t, items, "plugin:current-time")
	}
	workspace, item := snapshot()
	if !item.Enabled || item.HostState != "registered" || item.ToolCount != 2 || item.Check.State != "not_run" {
		t.Fatalf("registration is not a probe or execution: %+v", item)
	}
	check := func(item CapabilityItem) CapabilityCheck {
		w := request(a, "POST", "/api/capabilities/check", capabilityBody(workspace, item))
		requireStatus(t, w, 200)
		var result CapabilityCheck
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.State != "pass" || !strings.Contains(result.Message, hash(code)) || !strings.Contains(result.Message, "未执行插件或工具") {
			t.Fatalf("read probe scope: %+v", result)
		}
		return result
	}
	readReceipt := check(item)
	started := time.Now().UnixMilli()
	raw, err := a.callPluginToolContext(context.Background(), "current-time", "get_current_datetime", map[string]any{"timeZone": "Asia/Shanghai"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	var clock struct {
		TimeZone string `json:"timeZone"`
		UnixMS   int64  `json:"unixMs"`
	}
	if err := json.Unmarshal(b, &clock); err != nil || clock.TimeZone != "Asia/Shanghai" || clock.UnixMS < started || clock.UnixMS > time.Now().UnixMilli() {
		t.Fatalf("actual Go dispatch result: %s (%v)", b, err)
	}
	if _, err := a.callPluginToolContext(context.Background(), "current-time", "get_current_datetime", map[string]any{"timeZone": "Invalid/Acceptance"}); err == nil {
		t.Fatal("invalid timezone unexpectedly succeeded")
	}
	_, item = snapshot()
	if item.HostState != "registered" || item.ToolCount != 2 || item.Check != readReceipt {
		t.Fatalf("tool failure rewrote the independent readability observation: %+v", item)
	}

	requireStatus(t, request(a, "PUT", "/api/plugins/current-time", map[string]any{"enabled": false}), 200)
	_, disabled := snapshot()
	if disabled.Enabled || disabled.HostState != "not_registered" || disabled.ToolCount != 0 || disabled.Check.State != "stale" {
		t.Fatalf("disabled metadata retained an enabled surface: %+v", disabled)
	}
	requireStatus(t, request(a, "POST", "/api/capabilities/check", capabilityBody(workspace, disabled)), 400)
	if _, err := a.callPluginToolContext(context.Background(), "current-time", "get_current_datetime", nil); err == nil || !strings.Contains(err.Error(), "插件已停用") {
		t.Fatalf("disabled Go dispatch was not rejected: %v", err)
	}
	requireStatus(t, request(a, "PUT", "/api/plugins/current-time", map[string]any{"enabled": true}), 200)
	_, item = snapshot()
	if !item.Enabled || item.HostState != "registered" || item.ToolCount != 2 {
		t.Fatalf("re-enabled host did not recover: %+v", item)
	}
	check(item)

	// An external entry edit is not continuously monitored by the read probe.
	// A host refresh must still expose its actual error and empty tool surface,
	// while retaining the historical, explicitly scoped readability receipt.
	broken := []byte("throw new Error('private-host-detail-acceptance');")
	if err := os.WriteFile(filepath.Join(a.pluginsPath, "current-time", "index.js"), broken, 0600); err != nil {
		t.Fatal(err)
	}
	a.runPluginHost(context.Background())
	_, failed := snapshot()
	if failed.HostState != "error" || failed.ToolCount != 0 || failed.Check.State != "pass" {
		t.Fatalf("host load and historical file probe were conflated: %+v", failed)
	}
	w := request(a, "GET", "/api/capabilities", nil)
	if strings.Contains(w.Body.String(), "private-host-detail-acceptance") {
		t.Fatal("raw host exception leaked into capabilities")
	}
	if _, err := a.callPluginToolContext(context.Background(), "current-time", "get_current_datetime", nil); err == nil {
		t.Fatal("broken host dispatch unexpectedly succeeded")
	}
	if err := os.WriteFile(filepath.Join(a.pluginsPath, "current-time", "index.js"), code, 0600); err != nil {
		t.Fatal(err)
	}
	a.runPluginHost(context.Background())
	_, recovered := snapshot()
	if recovered.HostState != "registered" || recovered.ToolCount != 2 {
		t.Fatalf("repaired host failed to recover: %+v", recovered)
	}
	if _, err := a.callPluginToolContext(context.Background(), "current-time", "get_current_datetime", map[string]any{"timeZone": "UTC"}); err != nil {
		t.Fatalf("repaired tool execution failed: %v", err)
	}
}
