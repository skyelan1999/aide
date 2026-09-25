package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigBackupExportImport(t *testing.T) {
	a := testApp(t)
	// 配置一份含 API Key 的设置
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{
		"baseURL": "https://api.example.com", "model": "m1", "apiKey": "secret-key-123",
		"sandboxMode": "workspace-write", "voiceAssistantName": "小秘", "activeModel": "m1",
	}), 200)

	// 导出（不含敏感）：API Key 应被剔除，其余设置保留
	w := request(a, "POST", "/api/config/export", map[string]any{"includeSecrets": false})
	requireStatus(t, w, 200)
	var bk configBackup
	if err := json.Unmarshal(w.Body.Bytes(), &bk); err != nil {
		t.Fatal(err)
	}
	if bk.Format != "aide-config-backup" {
		t.Fatal("bad format")
	}
	var s Settings
	_ = json.Unmarshal(bk.Settings, &s)
	if s.APIKey != "" {
		t.Fatal("非敏感导出不应包含 apiKey")
	}
	if s.Model != "m1" || s.SandboxMode != "workspace-write" || s.VoiceAssistantName != "小秘" {
		t.Fatalf("非敏感设置丢失: %+v", s)
	}

	// 导出（含敏感）：API Key 保留
	w2 := request(a, "POST", "/api/config/export", map[string]any{"includeSecrets": true})
	var bk2 configBackup
	_ = json.Unmarshal(w2.Body.Bytes(), &bk2)
	var s2 Settings
	_ = json.Unmarshal(bk2.Settings, &s2)
	if s2.APIKey != "secret-key-123" {
		t.Fatal("含敏感导出应保留 apiKey")
	}

	// 改坏非敏感配置（apiKey 因补全仍保留）
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{
		"baseURL": "https://other.example", "model": "mX",
	}), 200)

	// 导入非敏感备份：非敏感设置恢复，密钥保留当前、不被空值清掉
	wi := request(a, "POST", "/api/config/import", map[string]any{
		"backup": json.RawMessage(w.Body.Bytes()), "importSecrets": false,
	})
	requireStatus(t, wi, 200)
	a.mu.Lock()
	cur := a.settings
	a.mu.Unlock()
	if cur.Model != "m1" || cur.SandboxMode != "workspace-write" || cur.VoiceAssistantName != "小秘" {
		t.Fatalf("导入未恢复非敏感设置: %+v", cur)
	}
	if cur.APIKey != "secret-key-123" {
		t.Fatal("未导入敏感时应保留当前 apiKey")
	}
	if _, err := os.Stat(filepath.Join(ConfigBackupsDir(a.dataPath), "settings.json.pre-import")); err != nil {
		t.Fatal("导入前应生成回滚点")
	}
}

func TestConfigBackupImportSecretsSwitch(t *testing.T) {
	a := testApp(t)
	// 手工构造一份含 apiKey 的备份
	imp := Settings{BaseURL: "https://api.example.com", Model: "m1", APIKey: "backup-key", ActiveModel: "m1"}
	raw, _ := json.Marshal(imp)
	bk, _ := json.Marshal(configBackup{
		Format: configBackupFormat, FormatVersion: configBackupVersion,
		AppVersion: "test", IncludeSecrets: true, Settings: raw,
	})

	// importSecrets=false：密钥不采用备份（保持空）
	w0 := request(a, "POST", "/api/config/import", map[string]any{
		"backup": json.RawMessage(bk), "importSecrets": false,
	})
	requireStatus(t, w0, 200)
	a.mu.Lock()
	k0 := a.settings.APIKey
	a.mu.Unlock()
	if k0 != "" {
		t.Fatalf("importSecrets=false 不应导入 apiKey, got %q", k0)
	}

	// importSecrets=true：密钥采用备份值
	w1 := request(a, "POST", "/api/config/import", map[string]any{
		"backup": json.RawMessage(bk), "importSecrets": true,
	})
	requireStatus(t, w1, 200)
	a.mu.Lock()
	k1 := a.settings.APIKey
	a.mu.Unlock()
	if k1 != "backup-key" {
		t.Fatalf("importSecrets=true 应导入 apiKey, got %q", k1)
	}

	// 拒绝非 aide 备份
	bad := request(a, "POST", "/api/config/import", map[string]any{
		"backup": json.RawMessage(`{"format":"something-else"}`),
	})
	requireStatus(t, bad, 400)
}
