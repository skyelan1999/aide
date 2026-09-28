package server

// 模型 API Key 加密保险库（#迁移）单元测试：
//   - 无密码：key 加密入 vault、settings.json 无明文、/api/config 不回显、模型可取 key
//   - 有密码：账户密码不再重复锁定凭证，访问令牌可在重启后自动解锁
//   - 旧明文迁移：从含明文 settings 检出→加密入库→落盘擦除，幂等
//   - vault.enc 落盘不可读；clearKey 删除；导出不含明文

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// settingsJSON 读出落盘 settings.json 原文。
func settingsJSONOnDisk(t *testing.T, a *App) string {
	t.Helper()
	b, err := os.ReadFile(SettingsPath(a.dataPath))
	if err != nil {
		t.Fatalf("读 settings.json 失败: %v", err)
	}
	return string(b)
}

// 1) 无密码场景：PUT /settings 存 key → 加密入 vault，settings.json 无明文，/api/config 不回显。
func TestNoPasswordKeyEncryptedInVault(t *testing.T) {
	a := testApp(t)
	if !a.vaultIsUnlocked() {
		t.Fatal("无密码时 vault 应由机器密钥自动解锁")
	}
	w := request(a, "PUT", "/api/settings", map[string]any{
		"baseURL": "https://api.deepseek.com",
		"model":   "deepseek-chat",
		"apiKey":  "sk-nopw-secret-001",
	})
	requireStatus(t, w, 200)
	if !a.hasModelAPIKey() {
		t.Fatal("vault 应已包含模型 key")
	}
	// 落盘 settings.json 绝无明文
	if s := settingsJSONOnDisk(t, a); strings.Contains(s, "sk-nopw-secret-001") {
		t.Fatalf("settings.json 泄露明文: %s", s)
	}
	// 解出的 key 正确
	key, err := a.modelAPIKeyLocked()
	if err != nil || key != "sk-nopw-secret-001" {
		t.Fatalf("modelAPIKeyLocked = %q, %v", key, err)
	}
	// vault.enc 落盘不可读
	if vb, err := os.ReadFile(a.vault.Path()); err == nil && strings.Contains(string(vb), "sk-nopw-secret-001") {
		t.Fatalf("vault.enc 泄露明文: %s", vb)
	}
	// /api/config：hasKey=true、models[].hasApiKey=true、无明文回显
	cw := request(a, "GET", "/api/config", nil)
	requireStatus(t, cw, 200)
	if strings.Contains(cw.Body.String(), "sk-nopw-secret-001") {
		t.Fatalf("/api/config 回显明文: %s", cw.Body.String())
	}
	var cfg map[string]any
	_ = json.Unmarshal(cw.Body.Bytes(), &cfg)
	if cfg["hasKey"] != true {
		t.Fatalf("config.hasKey 应为 true: %v", cfg["hasKey"])
	}
	models, _ := cfg["models"].([]any)
	if len(models) == 0 {
		t.Fatal("config.models 不应为空")
	}
	m0, _ := models[0].(map[string]any)
	if m0["hasApiKey"] != true {
		t.Fatalf("models[0].hasApiKey 应为 true: %v", m0)
	}
	if cfg["vaultUnlocked"] != true {
		t.Fatalf("config.vaultUnlocked 应为 true: %v", cfg["vaultUnlocked"])
	}
}

// 2) clearKey 删除 vault 中的 key。
func TestClearKeyRemovesFromVault(t *testing.T) {
	a := testApp(t)
	_ = request(a, "PUT", "/api/settings", map[string]any{
		"baseURL": "https://api.deepseek.com", "model": "deepseek-chat", "apiKey": "sk-tmp-002",
	})
	if !a.hasModelAPIKey() {
		t.Fatal("前置：key 应已入库")
	}
	w := request(a, "PUT", "/api/settings", map[string]any{
		"baseURL": "https://api.deepseek.com", "model": "deepseek-chat", "clearKey": true,
	})
	requireStatus(t, w, 200)
	if a.hasModelAPIKey() {
		t.Fatal("clearKey 后 vault 不应再有模型 key")
	}
	if key, _ := a.modelAPIKeyLocked(); key != "" {
		t.Fatalf("clearKey 后 key 应为空: %q", key)
	}
}

// 3) 旧明文迁移：检出明文→加密入库→落盘擦除，且幂等。
func TestLegacyPlaintextMigration(t *testing.T) {
	a := testApp(t)
	// 模拟旧版 settings.json 带明文
	a.settings.APIKey = "sk-legacy-plain-003"
	a.stageLegacyAPIKeyMigration()
	if a.settings.APIKey != "" {
		t.Fatal("staging 后内存 settings.APIKey 应清空")
	}
	if a.pendingLegacyAPIKey != "sk-legacy-plain-003" {
		t.Fatalf("pending 暂存不符: %q", a.pendingLegacyAPIKey)
	}
	// 无密码：vault 已自动解锁，立即迁移
	a.migratePendingLegacyAPIKeyLocked()
	if a.pendingLegacyAPIKey != "" {
		t.Fatal("迁移后 pending 应清空")
	}
	if !a.hasModelAPIKey() {
		t.Fatal("迁移后 vault 应有 key")
	}
	if key, _ := a.modelAPIKeyLocked(); key != "sk-legacy-plain-003" {
		t.Fatalf("迁移后解出 key=%q", key)
	}
	// 落盘 settings.json 无明文
	if s := settingsJSONOnDisk(t, a); strings.Contains(s, "sk-legacy-plain-003") {
		t.Fatalf("迁移后 settings.json 仍有明文: %s", s)
	}
	// 幂等：再次 staging+migrate 不报错、不重复
	a.stageLegacyAPIKeyMigration()
	a.migratePendingLegacyAPIKeyLocked()
	if key, _ := a.modelAPIKeyLocked(); key != "sk-legacy-plain-003" {
		t.Fatalf("幂等后 key 应不变: %q", key)
	}
}

// 4) 设置账户密码后，模拟重启锁定 vault；本机 access-token 应自动恢复凭证访问。
func TestPasswordDoesNotRelockVault(t *testing.T) {
	a := testApp(t)
	// 机器密钥下存 key
	a.storeModelAPIKeyPlaintextLocked("sk-withpw-004")
	// 设置账户密码不会改变 vault 的 access-token 密钥。
	w := request(a, "PUT", "/api/settings", map[string]any{
		"baseURL": "https://api.deepseek.com", "model": "deepseek-chat", "newPassword": "pw-004",
	})
	requireStatus(t, w, 200)
	// 模拟重启：锁定 vault
	a.vault.Lock()
	if a.vaultIsUnlocked() {
		t.Fatal("锁定后 vault 应处于未解锁态")
	}
	if err := a.unlockVaultAtStartup(); err != nil {
		t.Fatalf("access-token 自动解锁失败: %v", err)
	}
	key, err := a.modelAPIKeyLocked()
	if err != nil || key != "sk-withpw-004" {
		t.Fatalf("自动解锁后应解出 key, got %q err=%v", key, err)
	}
}

// 5) 旧版账户密码密钥会在一次正确账户密码验证后迁移到访问令牌密钥。
func TestLegacyPasswordVaultMigratesToAccessToken(t *testing.T) {
	a := testApp(t)
	legacyKey := deriveKey("legacy-pw-005")
	a.vault.Unlock(legacyKey)
	if err := a.vault.Put(VaultIDModelAPIKey, VaultTypeModelAPIKey, "模型 API Key", []byte("sk-legacy-password-005"), ""); err != nil {
		t.Fatal(err)
	}
	if err := a.vault.Save(); err != nil {
		t.Fatal(err)
	}
	a.settings.UserPasswordHash = mustHashPassword("legacy-pw-005")
	a.vault.Lock()
	if err := a.unlockVaultAtStartup(); err != nil {
		t.Fatal(err)
	}
	if a.vaultIsUnlocked() {
		t.Fatal("旧密码密钥不应在未验证密码时自动解锁")
	}
	a.unlockVault("legacy-pw-005")
	if !a.vaultIsUnlocked() {
		t.Fatal("正确密码后应迁移并解锁")
	}
	a.vault.Lock()
	if err := a.unlockVaultAtStartup(); err != nil || !a.vaultIsUnlocked() {
		t.Fatalf("迁移后应由访问令牌自动解锁: %v", err)
	}
	key, err := a.modelAPIKeyLocked()
	if err != nil || key != "sk-legacy-password-005" {
		t.Fatalf("迁移后 key=%q err=%v", key, err)
	}
}

// 5) 导出配置备份不含明文 API Key。
func TestExportNoPlaintextKey(t *testing.T) {
	a := testApp(t)
	_ = request(a, "PUT", "/api/settings", map[string]any{
		"baseURL": "https://api.deepseek.com", "model": "deepseek-chat", "apiKey": "sk-export-005",
	})
	w := request(a, "POST", "/api/config/export", map[string]any{"includeSecrets": true})
	requireStatus(t, w, 200)
	if strings.Contains(w.Body.String(), "sk-export-005") {
		t.Fatalf("导出备份含明文 key: %s", w.Body.String())
	}
}

// 6) 导入旧版（含明文）备份时自动把明文迁入 vault，落盘 settings 无明文。
func TestImportLegacyPlaintextKey(t *testing.T) {
	a := testApp(t)
	// 构造一个旧版备份：settings 内含明文 apiKey
	backup := map[string]any{
		"format":          configBackupFormat,
		"formatVersion":   configBackupVersion,
		"settingsVersion": configSettingsVersion,
		"includeSecrets":  true,
		"settings":        json.RawMessage(`{"baseURL":"https://api.deepseek.com","model":"deepseek-chat","apiKey":"sk-import-006"}`),
	}
	w := request(a, "POST", "/api/config/import", map[string]any{"backup": backup, "importSecrets": true})
	requireStatus(t, w, 200)
	if !a.hasModelAPIKey() {
		t.Fatal("导入含明文备份后 vault 应有 key")
	}
	if key, _ := a.modelAPIKeyLocked(); key != "sk-import-006" {
		t.Fatalf("导入后解出 key=%q", key)
	}
	if s := settingsJSONOnDisk(t, a); strings.Contains(s, "sk-import-006") {
		t.Fatalf("导入后 settings.json 仍有明文: %s", s)
	}
}
