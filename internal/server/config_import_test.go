package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"
)

// importBackupRaw 构造一个指定 settings 内容/版本标签的备份并发起导入。
// formatVersion 控制信封格式版本（用于高版本拒绝测试）；settingsVersion 空串表示旧版（无标签）备份。
func importBackupRaw(t *testing.T, a *App, settingsRaw json.RawMessage, formatVersion int, settingsVersion string, importSecrets bool) *httptest.ResponseRecorder {
	t.Helper()
	bk := configBackup{
		Format:          configBackupFormat,
		FormatVersion:   formatVersion,
		SettingsVersion: settingsVersion,
		IncludeSecrets:  importSecrets,
		Settings:        settingsRaw,
	}
	bkb, err := json.Marshal(bk)
	if err != nil {
		t.Fatal(err)
	}
	return request(a, "POST", "/api/config/import", map[string]any{
		"backup":        json.RawMessage(bkb),
		"importSecrets": importSecrets,
	})
}

func curSettings(a *App) Settings {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.settings
}

// 旧备份缺少多个新字段 → 导入后缺失字段应等于当前版本默认值。
func TestImportMissingFieldsDefaults(t *testing.T) {
	a := testApp(t)
	// 模拟旧版备份：只有 baseURL + 单 model，缺少所有新字段
	raw := json.RawMessage(`{"baseURL":"https://api.old.com","model":"old-model"}`)
	requireStatus(t, importBackupRaw(t, a, raw, configBackupVersion, "", false), 200)

	s := curSettings(a)
	if s.VoiceAssistantName != "小秘" {
		t.Fatalf("VoiceAssistantName 未补默认: %q", s.VoiceAssistantName)
	}
	if s.VoiceReplyGender != "female" {
		t.Fatalf("VoiceReplyGender 未补默认: %q", s.VoiceReplyGender)
	}
	if s.SandboxMode != "workspace-write" {
		t.Fatalf("SandboxMode 未补默认: %q", s.SandboxMode)
	}
	if s.ToolMaxRounds != 60 {
		t.Fatalf("ToolMaxRounds 未补默认 60: %d", s.ToolMaxRounds)
	}
	if s.ShellTimeout != 60 {
		t.Fatalf("ShellTimeout 未补默认 60: %d", s.ShellTimeout)
	}
	if s.LockTimeoutSec != 0 {
		t.Fatalf("LockTimeoutSec 默认应为 0: %d", s.LockTimeoutSec)
	}
	if s.ReasoningEffort != "auto" {
		t.Fatalf("ReasoningEffort 未补默认 auto: %q", s.ReasoningEffort)
	}
	if s.TTSProvider != "auto" {
		t.Fatalf("TTSProvider 未补默认 auto: %q", s.TTSProvider)
	}
	if s.TTSRate != 1.0 {
		t.Fatalf("TTSRate 未补默认 1.0: %v", s.TTSRate)
	}
	if s.DebugAccessEnabled != false {
		t.Fatal("DebugAccessEnabled 默认应为 false")
	}
	if s.ActivePersona != personaAide {
		t.Fatalf("ActivePersona 未补默认 aide: %q", s.ActivePersona)
	}
	// 旧单 model 应迁移为模型列表
	if len(s.Models) != 1 || s.Models[0].ID != "old-model" {
		t.Fatalf("旧 model 未迁移为模型列表: %+v", s.Models)
	}
	if s.ActiveModel != "old-model" {
		t.Fatalf("ActiveModel 未补齐: %q", s.ActiveModel)
	}
}

// 备份显式给出的非默认值 → 导入后应原样保留。
func TestImportExplicitValuesPreserved(t *testing.T) {
	a := testApp(t)
	raw := json.RawMessage(`{
		"baseURL":"https://api.x.com","model":"m1","activeModel":"m1",
		"models":[{"id":"m1","name":"M One","contextWindow":128000}],
		"toolMaxRounds":45,"shellTimeout":90,"lockTimeoutSec":300,
		"sandboxMode":"read-only","reasoningEffort":"high",
		"voiceAssistantName":"小助","voiceReplyGender":"male",
		"ttsProvider":"edge","ttsRate":1.2
	}`)
	requireStatus(t, importBackupRaw(t, a, raw, configBackupVersion, configSettingsVersion, false), 200)

	s := curSettings(a)
	if s.ToolMaxRounds != 45 {
		t.Fatalf("ToolMaxRounds 非默认值应保留, got %d", s.ToolMaxRounds)
	}
	if s.ShellTimeout != 90 {
		t.Fatalf("ShellTimeout 应保留 90, got %d", s.ShellTimeout)
	}
	if s.LockTimeoutSec != 300 {
		t.Fatalf("LockTimeoutSec 应保留 300, got %d", s.LockTimeoutSec)
	}
	if s.SandboxMode != "read-only" {
		t.Fatalf("SandboxMode 应保留 read-only, got %q", s.SandboxMode)
	}
	if s.ReasoningEffort != "high" {
		t.Fatalf("ReasoningEffort 应保留 high, got %q", s.ReasoningEffort)
	}
	if s.VoiceAssistantName != "小助" {
		t.Fatalf("VoiceAssistantName 应保留, got %q", s.VoiceAssistantName)
	}
	if s.VoiceReplyGender != "male" {
		t.Fatalf("VoiceReplyGender 应保留 male, got %q", s.VoiceReplyGender)
	}
	if s.TTSProvider != "edge" {
		t.Fatalf("TTSProvider 应保留 edge, got %q", s.TTSProvider)
	}
	if s.TTSRate != 1.2 {
		t.Fatalf("TTSRate 应保留 1.2, got %v", s.TTSRate)
	}
}

// 备份显式给出零值（toolMaxRounds:0）→ 经合法性校验回退默认 60（而非保留零值）。
func TestImportExplicitZeroValues(t *testing.T) {
	a := testApp(t)
	raw := json.RawMessage(`{"baseURL":"https://api.x.com","model":"m1","activeModel":"m1","toolMaxRounds":0}`)
	requireStatus(t, importBackupRaw(t, a, raw, configBackupVersion, configSettingsVersion, false), 200)

	if s := curSettings(a); s.ToolMaxRounds != 60 {
		t.Fatalf("显式 0 应回退默认 60, got %d", s.ToolMaxRounds)
	}
}

// 模型上下文窗口非法（contextWindow:0）→ 经 normalizeModels 回填默认 65536。
// 说明：本版本 Settings 无顶层 ContextWindow 字段，每模型窗口由 normalizeModels 校验。
func TestImportInvalidContextWindow(t *testing.T) {
	a := testApp(t)
	raw := json.RawMessage(`{
		"baseURL":"https://api.x.com","activeModel":"gpt",
		"models":[{"id":"gpt","name":"GPT","contextWindow":0}]
	}`)
	requireStatus(t, importBackupRaw(t, a, raw, configBackupVersion, configSettingsVersion, false), 200)

	s := curSettings(a)
	if len(s.Models) != 1 || s.Models[0].ContextWindow != defaultContextWindow {
		t.Fatalf("contextWindow:0 应回填默认 %d, got %+v", defaultContextWindow, s.Models)
	}
}

// toolMaxRounds 超过上限 200 → 回退默认 60。
func TestImportInvalidMaxToolRounds(t *testing.T) {
	a := testApp(t)
	raw := json.RawMessage(`{"baseURL":"https://api.x.com","model":"m1","activeModel":"m1","toolMaxRounds":999}`)
	requireStatus(t, importBackupRaw(t, a, raw, configBackupVersion, configSettingsVersion, false), 200)

	if s := curSettings(a); s.ToolMaxRounds != 60 {
		t.Fatalf("toolMaxRounds=999 应回退 60, got %d", s.ToolMaxRounds)
	}
}

// 未勾选导入密钥时，APIKey/TTSAPIKey 等敏感字段保留当前值，不被备份里的空串清空。
func TestImportSensitiveFieldsNotImported(t *testing.T) {
	a := testApp(t)
	a.mu.Lock()
	a.settings.APIKey = "cur-key"
	a.settings.TTSAPIKey = "cur-tts-key"
	a.mu.Unlock()

	// 脱敏备份：不含任何密钥（apiKey/ttsAPIKey 均缺省）
	raw := json.RawMessage(`{"baseURL":"https://api.x.com","model":"m1","activeModel":"m1"}`)
	requireStatus(t, importBackupRaw(t, a, raw, configBackupVersion, configSettingsVersion, false), 200)

	s := curSettings(a)
	if s.APIKey != "cur-key" {
		t.Fatalf("未导入密钥时应保留当前 APIKey, got %q", s.APIKey)
	}
	if s.TTSAPIKey != "cur-tts-key" {
		t.Fatalf("未导入密钥时应保留当前 TTSAPIKey, got %q", s.TTSAPIKey)
	}
}

// 备份 FormatVersion 高于当前 → 拒绝导入。
func TestImportHigherVersionRejected(t *testing.T) {
	a := testApp(t)
	raw := json.RawMessage(`{"baseURL":"https://api.x.com","model":"m1"}`)
	w := importBackupRaw(t, a, raw, configBackupVersion+5, configSettingsVersion, false)
	requireStatus(t, w, 400)
}

// 导入后立即生效：内存态与"读盘后经 New() 同款规范化"的结果一致（无需重启）。
func TestImportNoRestartNeeded(t *testing.T) {
	a := testApp(t)
	raw := json.RawMessage(`{"baseURL":"https://api.old.com","model":"old-model","toolMaxRounds":45}`)
	requireStatus(t, importBackupRaw(t, a, raw, configBackupVersion, "", false), 200)

	inMem := curSettings(a)

	// 从磁盘重读 settings.json，走与 New() 启动加载完全相同的收敛流程
	diskB, err := os.ReadFile(SettingsPath(a.dataPath))
	if err != nil {
		t.Fatal(err)
	}
	disk := defaultSettings()
	if err := json.Unmarshal(diskB, &disk); err != nil {
		t.Fatal(err)
	}
	if err := normalizeLoadedSettings(&disk); err != nil {
		t.Fatal(err)
	}

	if inMem.VoiceAssistantName != disk.VoiceAssistantName {
		t.Fatalf("VoiceAssistantName 导入态(%q) ≠ 重启态(%q)", inMem.VoiceAssistantName, disk.VoiceAssistantName)
	}
	if inMem.ToolMaxRounds != disk.ToolMaxRounds {
		t.Fatalf("ToolMaxRounds 导入态(%d) ≠ 重启态(%d)", inMem.ToolMaxRounds, disk.ToolMaxRounds)
	}
	if len(inMem.Models) != len(disk.Models) || inMem.Models[0].ID != disk.Models[0].ID {
		t.Fatalf("Models 导入态 %+v ≠ 重启态 %+v", inMem.Models, disk.Models)
	}
	if inMem.ActiveModel != disk.ActiveModel {
		t.Fatalf("ActiveModel 导入态(%q) ≠ 重启态(%q)", inMem.ActiveModel, disk.ActiveModel)
	}
}

// 迁移钩子 v0→v1：旧单人格 personaCipher 字符串 → personaCiphers 映射。
func TestMigrateHookV0toV1(t *testing.T) {
	s := Settings{PersonaCipher: "legacy-cipher-ct"}
	migrateSettings("", &s) // 空 settingsVersion = v0
	if s.PersonaCiphers == nil || s.PersonaCiphers[personaAide] != "legacy-cipher-ct" {
		t.Fatalf("v0→v1 迁移未把 personaCipher 播种到映射: %+v", s.PersonaCiphers)
	}
}

// 导出脱敏未回退：非敏感导出不得携带任何可解密封面的材料。
func TestDesensitizationPreserved(t *testing.T) {
	a := testApp(t)
	a.mu.Lock()
	a.settings.APIKey = "secret-key"
	a.settings.TTSAPIKey = "secret-tts"
	a.settings.UserPasswordHash = "secret-hash"
	a.settings.DebugTokenHash = "secret-debug"
	a.settings.PersonaCiphers = map[string]string{"aide": "ct"}
	a.mu.Unlock()

	w := request(a, "POST", "/api/config/export", map[string]any{"includeSecrets": false})
	requireStatus(t, w, 200)
	var bk configBackup
	if err := json.Unmarshal(w.Body.Bytes(), &bk); err != nil {
		t.Fatal(err)
	}
	if bk.SettingsVersion != configSettingsVersion {
		t.Fatalf("导出信封应携带 settingsVersion=%q, got %q", configSettingsVersion, bk.SettingsVersion)
	}
	var s Settings
	if err := json.Unmarshal(bk.Settings, &s); err != nil {
		t.Fatal(err)
	}
	if s.APIKey != "" || s.TTSAPIKey != "" || s.UserPasswordHash != "" || s.DebugTokenHash != "" {
		t.Fatalf("非敏感导出仍含敏感材料: apiKey=%q ttsAPIKey=%q pwHash=%q debugHash=%q",
			s.APIKey, s.TTSAPIKey, s.UserPasswordHash, s.DebugTokenHash)
	}
	if s.PersonaCiphers != nil || s.PersonaCipher != "" {
		t.Fatalf("非敏感导出应清空人格密文: %+v", s.PersonaCiphers)
	}
}
