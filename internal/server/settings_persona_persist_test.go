package server

import (
	"encoding/json"
	"testing"
)

// TestSettingsPartialPutPreservesFields 局部 settings PUT（只改某一项，如无障碍/切模型）
// 不得清空性格启用态、沙箱、轮次、输入设备；VoiceInputDevice 显式传空应能重置为系统默认并保持。
func TestSettingsPartialPutPreservesFields(t *testing.T) {
	a := testApp(t)
	// 基础配置
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{
		"baseURL": "https://api.example.com", "model": "m1",
	}), 200)
	// 启用小秘性格
	requireStatus(t, request(a, "PUT", "/api/personality", map[string]any{
		"id": "xiaomi", "enabled": true, "prompt": "性格原文-小秘",
	}), 200)
	// 同时启用 aide 性格（两个性格的开关都应被记住）
	requireStatus(t, request(a, "PUT", "/api/personality", map[string]any{
		"id": "aide", "enabled": true, "prompt": "性格原文-aide",
	}), 200)
	// 沙箱 / 轮次 / 设备
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{
		"sandboxMode": "workspace-write", "toolMaxRounds": 80,
		"voiceInputDevice": "mic1", "activeModel": "m1",
	}), 200)
	// 无关局部保存（只带 activeModel，模拟无障碍自动保存/切模型）
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{"activeModel": "m1"}), 200)

	// 性格启用态与提示词保留
	w := request(a, "GET", "/api/personality?id=xiaomi", nil)
	requireStatus(t, w, 200)
	var p map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &p)
	if p["enabled"] != true || p["prompt"] != "性格原文-小秘" {
		t.Fatalf("personality lost after partial PUT: %v", p)
	}
	// aide 性格启用态与提示词同样保留
	wa := request(a, "GET", "/api/personality?id=aide", nil)
	requireStatus(t, wa, 200)
	var pa map[string]any
	_ = json.Unmarshal(wa.Body.Bytes(), &pa)
	if pa["enabled"] != true || pa["prompt"] != "性格原文-aide" {
		t.Fatalf("aide personality lost after partial PUT: %v", pa)
	}
	// 沙箱/轮次/设备保留
	a.mu.Lock()
	sb := a.settings.SandboxMode
	rounds := a.settings.ToolMaxRounds
	dev := a.settings.VoiceInputDevice
	a.mu.Unlock()
	if sb != "workspace-write" || rounds != 80 || dev != "mic1" {
		t.Fatalf("settings reset unexpectedly: sandbox=%q rounds=%d device=%q", sb, rounds, dev)
	}
	// 显式传空 device → 重置为系统默认
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{
		"voiceInputDevice": "", "activeModel": "m1",
	}), 200)
	a.mu.Lock()
	dev2 := a.settings.VoiceInputDevice
	a.mu.Unlock()
	if dev2 != "" {
		t.Fatalf("explicit empty device should reset, got %q", dev2)
	}
	// 重置后再做无关局部 PUT：空设备应保持、不被旧值回填
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{"activeModel": "m1"}), 200)
	a.mu.Lock()
	dev3 := a.settings.VoiceInputDevice
	a.mu.Unlock()
	if dev3 != "" {
		t.Fatalf("device should stay empty after another partial PUT, got %q", dev3)
	}
}
