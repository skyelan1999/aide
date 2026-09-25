package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ── 恢复出厂设置（factory reset）──────────────────────────────────────────────
// 设计原则（用户明确）：
//   1. 范围可选：4 组复选框，默认只勾"设置项"，更具破坏性的项必须用户主动勾选；
//   2. 默认安全：绝不删除 /workspace、/context 里的用户代码与文件；
//   3. 强确认：前端须勾选"不可恢复"并输入"重置"二字；涉及凭据/小秘历史时须重输登录密码；
//   4. 后悔药：执行前自动生成完整备份（含 secrets 加密信封 + 小秘历史），写到
//      /data/config/backups/factory-reset-<ts>.json，可经 /api/config/import 恢复；
//   5. 可恢复：按 scope 清除/重置，重新初始化默认目录与必要项，全程写审计。
//
// 复用：#32 defaultSettings / #29 Argon2id+GCM / #31 目录分层 / #34 性格 /
//       #35 记忆 / #38 统一 vault。本文件不删除任何 workPath / reference 下的用户文件。

// ResetScope 重置范围。JSON 标签即前端弹窗分组复选框名。
type ResetScope struct {
	Settings           bool `json:"settings"`           // 设置项：恢复 defaultSettings()（凭据类字段默认保留，除非同时勾凭据）
	SessionsAndMemory  bool `json:"sessionsAndMemory"`  // 清空所有会话 + aide 记忆 + 小秘历史/记忆 + 性格恢复默认
	CredentialsAndKeys bool `json:"credentialsAndKeys"` // 清除密码哈希/API Key/SSH/vault/来源密钥/调试令牌/WebAuthn/KDF salt/access-token
	WorkspaceConfig    bool `json:"workspaceConfig"`    // 工作空间配置回到本地默认（不删用户文件）
}

// any reports whether at least one scope box is checked.
func (s ResetScope) any() bool {
	return s.Settings || s.SessionsAndMemory || s.CredentialsAndKeys || s.WorkspaceConfig
}

// factoryResetAuditPath 出厂重置审计日志：audit/factory-reset-audit.jsonl。
func factoryResetAuditPath(data string) string {
	return filepath.Join(AuditDir(data), "factory-reset-audit.jsonl")
}

// writeFactoryResetAudit 追加一条出厂重置审计（时间、scope、备份路径、结果）。绝不记录凭据正文。
func writeFactoryResetAudit(data string, scope ResetScope, backupPath, result string) {
	entry := map[string]any{
		"time":       time.Now().UTC().Format(time.RFC3339Nano),
		"settings":   scope.Settings,
		"sessions":   scope.SessionsAndMemory,
		"credentials": scope.CredentialsAndKeys,
		"workspace":  scope.WorkspaceConfig,
		"backupPath": backupPath,
		"result":     result,
	}
	b, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_ = os.MkdirAll(AuditDir(data), 0700)
	f, err := os.OpenFile(factoryResetAuditPath(data), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}

// buildResetBackup 执行破坏性操作之前，把当前完整状态打包成可导入的配置备份信封，
// 写到 /data/config/backups/factory-reset-<ts>.json（含 secrets 加密信封 + 小秘历史）。
// 返回备份文件绝对路径。任何读不到的可选材料（如未启用的小秘历史）跳过，不阻断备份。
func (a *App) buildResetBackup() (string, error) {
	// 持锁期间只做读盘，备份落盘在锁外完成（此处调用方已持锁，读 a.settings 安全）。
	sb, err := json.MarshalIndent(a.settings, "", "  ")
	if err != nil {
		return "", err
	}
	bk := configBackup{
		Format:           configBackupFormat,
		FormatVersion:    configBackupVersion,
		SettingsVersion:  configSettingsVersion,
		ExportedAt:       time.Now().UTC().Format(time.RFC3339Nano),
		AppVersion:       a.version,
		IncludeSecrets:   true,
		IncludeVoiceData: true,
		Settings:         sb,
	}
		if b, e := os.ReadFile(SourcesSecretsPath(a.dataPath)); e == nil && json.Valid(b) {
		bk.SourcesSecrets = b
	}
	if a.vault != nil {
		if b, e := os.ReadFile(a.vault.Path()); e == nil && len(b) > 0 && json.Valid(b) {
			bk.WorkspaceSecrets = b // 加密信封，绝不回退明文
		}
	}
	if len(bk.WorkspaceSecrets) == 0 {
		if b, e := os.ReadFile(WorkspaceSecretsPath(a.dataPath)); e == nil && json.Valid(b) {
			bk.WorkspaceSecrets = b
		}
	}
	if b, e := os.ReadFile(VoiceHistoryPath(a.dataPath)); e == nil && json.Valid(b) {
		bk.VoiceHistory = b
	}
	out, err := json.MarshalIndent(bk, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(ConfigBackupsDir(a.dataPath), 0700); err != nil {
		return "", err
	}
	name := "factory-reset-" + time.Now().Format("20060102-150405") + ".json"
	path := filepath.Join(ConfigBackupsDir(a.dataPath), name)
	if err := os.WriteFile(path, out, 0600); err != nil {
		return "", err
	}
	return path, nil
}

// FactoryReset 按 scope 执行出厂重置。调用方负责已完成 confirmText 与 HTTP 鉴权；
// 本函数在涉及凭据/小秘历史时用 password 复核本人。返回备份路径与是否需要重新登录。
//
// 全程持 a.mu；先备份、后清除、再重新初始化，最后写审计。永不触碰 workPath / reference。
func (a *App) FactoryReset(scope ResetScope, password string) (backupPath string, needRelogin bool, err error) {
	defer func() {
		// 审计无论成败都写；失败时记录错误，备份路径可能为空。
		result := "ok"
		if err != nil {
			result = "error: " + err.Error()
		}
		writeFactoryResetAudit(a.dataPath, scope, backupPath, result)
	}()

	a.mu.Lock()
	defer a.mu.Unlock()

	// 1) 本人复核：涉及凭据或小秘历史时，若已设置账户密码则必须输入正确密码。
	needPassword := scope.CredentialsAndKeys || scope.SessionsAndMemory
	if needPassword && a.settings.UserPasswordHash != "" {
		ok, _ := VerifyPassword(password, a.settings.UserPasswordHash)
		if !ok {
			return "", false, errFactoryAuth
		}
	}

	// 2) 自动备份（破坏性操作前）。
	backupPath, err = a.buildResetBackup()
	if err != nil {
		return "", false, fmt.Errorf("重置前自动备份失败，已中止：%w", err)
	}

	// 3) 按 scope 执行。
	if scope.Settings {
		a.applySettingsReset(scope.CredentialsAndKeys)
	}
	if scope.SessionsAndMemory {
		a.applySessionsAndMemoryReset()
	}
	if scope.CredentialsAndKeys {
		needRelogin = true
		a.applyCredentialsReset()
	}
	if scope.WorkspaceConfig {
		if e := a.applyWorkspaceConfigReset(); e != nil {
			return backupPath, needRelogin, e
		}
	}

	// 4) 重新初始化默认目录骨架（幂等）。
	if e := EnsureDirs(a.dataPath); e != nil {
		return backupPath, needRelogin, e
	}
	return backupPath, needRelogin, nil
}

// applySettingsReset 把设置收敛为当前版本默认值。
// wipeCredentials=true 时一并清零凭据类字段；否则保留 API Key/密码哈希/调试令牌等，
// 使"仅重置设置"不破坏登录态与已接入模型。调用方持 a.mu。
func (a *App) applySettingsReset(wipeCredentials bool) {
	cur := a.settings
	next := defaultSettings()
	// 会话编号是单调计数器，重置设置不回退编号（避免历史编号语义混乱）。
	next.NextSessionSeq = cur.NextSessionSeq

	if !wipeCredentials {
		// 凭据/密钥类字段随"凭据与密钥"分组走，设置-only 一律保留。
		next.APIKey = cur.APIKey
		next.TTSAPIKey = cur.TTSAPIKey
		next.TTSAzureKey = cur.TTSAzureKey
		next.TTSAzureRegion = cur.TTSAzureRegion
		next.CloneTTSBaseURL = cur.CloneTTSBaseURL
		next.CloneTTSAPIKey = cur.CloneTTSAPIKey
		next.CloneVoiceID = cur.CloneVoiceID
		next.CloneTTSBackend = cur.CloneTTSBackend
		next.UserPasswordHash = cur.UserPasswordHash
		next.PersonaCiphers = cur.PersonaCiphers
		next.PersonaCipher = cur.PersonaCipher
		next.UserName = cur.UserName
		next.LockTimeoutSec = cur.LockTimeoutSec
		next.DebugTokenHash = cur.DebugTokenHash
		next.DebugTokenCreatedAt = cur.DebugTokenCreatedAt
		next.DebugTokenExpiresAt = cur.DebugTokenExpiresAt
		next.DebugAccessEnabled = cur.DebugAccessEnabled
		next.DebugAllowOrigins = cur.DebugAllowOrigins
	}
	a.settings = next
	if err := atomicJSON(SettingsPath(a.dataPath), a.settings); err != nil {
		log.Printf("出厂重置：写回 settings.json 失败: %v", err)
	}
}

// applySessionsAndMemoryReset 清空所有会话（active/archived/assistant）、aide 核心记忆、
// 小秘历史/长期记忆，并把性格恢复默认。调用方持 a.mu。不删 /workspace /context 用户文件。
func (a *App) applySessionsAndMemoryReset() {
	// 会话文件：清空三个 bucket 下的 session-*.json。
	for _, dir := range sessionBucketDirs(a.dataPath) {
		_ = removeGlob(dir, "session-*.json")
	}
	// 内存会话表清空，随后重建恰好一个小秘系统会话。
	a.sessions = map[string]*Session{}
	for id, cancel := range a.cancels {
		cancel()
		delete(a.cancels, id)
	}
	// aide 核心记忆 + 反馈（可再生的 cache/ 保留）。
	_ = removeGlob(MemoryCoreDir(a.dataPath), "*.json")
	_ = removeGlob(MemoryFeedbackDir(a.dataPath), "*.json")
	// 小秘历史与长期记忆：删文件并让 voiceAgent 重新加载为空。
	_ = os.Remove(VoiceHistoryPath(a.dataPath))
	_ = os.Remove(VoiceMemoryPath(a.dataPath))
	if a.voiceAgent != nil {
		a.voiceAgent = newVoiceAgent(a.dataPath)
		a.voiceAgent.attachBroker(a.liveBroker)
	}
	// 性格恢复默认（aide + xiaomi），清空自定义性格密文与解锁缓存。
	for _, id := range []string{personaAide, personaXiaomi} {
		a.personalityResetLocked(id)
	}
	a.settings.PersonaCiphers = map[string]string{}
	a.settings.PersonaCipher = ""
	a.personaCustom = map[string]string{}
	a.personaKey = ""
	if err := atomicJSON(SettingsPath(a.dataPath), a.settings); err != nil {
		log.Printf("出厂重置：清空性格后写回 settings.json 失败: %v", err)
	}
	// 重建小秘系统会话（ensureAssistantSession 内部会再次自取锁——这里用可重入安全的直接构造）。
	a.ensureAssistantSessionAfterReset()
}

// ensureAssistantSessionAfterReset 重置会话后重建唯一小秘系统会话。调用方已持 a.mu。
func (a *App) ensureAssistantSessionAfterReset() {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	s := &Session{
		ID: newID(), Title: a.assistantNameLocked(),
		Created: now, Updated: now,
		Messages: []Message{}, Runs: []*Task{},
		Kind: assistantSessionKind, Pinned: true,
	}
	a.assignSessionNumber(s)
	a.sessions[s.ID] = s
	if err := a.save(s); err != nil {
		log.Printf("出厂重置：重建小秘会话失败: %v", err)
	}
}

// applyCredentialsReset 清除全部认证与密钥材料：密码哈希、API Key、vault、来源/工作区密钥、
// 调试令牌、WebAuthn、KDF salt、access-token。随后重新生成 access-token 强制重新登录。
// 调用方持 a.mu。
func (a *App) applyCredentialsReset() {
	// 1) 内存设置里的凭据类字段清零（applySettingsReset 已在 wipeCredentials 时落盘；
	//    这里兜底，即使未勾 Settings 也确保凭据字段被清）。
	a.settings.APIKey = ""
	a.settings.TTSAPIKey = ""
	a.settings.TTSAzureKey = ""
	a.settings.CloneTTSAPIKey = ""
	a.settings.UserPasswordHash = ""
	a.settings.PersonaCiphers = map[string]string{}
	a.settings.PersonaCipher = ""
	a.settings.DebugTokenHash = ""
	a.settings.DebugTokenCreatedAt = ""
	a.settings.DebugTokenExpiresAt = ""
	a.settings.DebugAccessEnabled = false
	if err := atomicJSON(SettingsPath(a.dataPath), a.settings); err != nil {
		log.Printf("出厂重置：清除凭据后写回 settings.json 失败: %v", err)
	}
	a.personaCustom = map[string]string{}
	a.personaKey = ""

	// 2) auth/：access-token（删后重建）、kdf-salt、webauthn 凭证。
	_ = os.Remove(AccessTokenPath(a.dataPath))
	_ = os.Remove(KdfSaltPath(a.dataPath))
	_ = os.Remove(WebAuthnCredsPath(a.dataPath))
	if a.webAuthn != nil {
		a.webAuthn.loadLocked() // 重新读盘（文件已删）→ 空凭证
	}
	// 内存解锁态全部失效。
	a.clearAssistantUnlock()

	// 3) secrets/：vault 信封、来源密钥、工作区旧明文密钥。
	_ = os.Remove(SourcesSecretsPath(a.dataPath))
	_ = os.Remove(WorkspaceSecretsPath(a.dataPath))
	_ = os.Remove(WorkspaceSecretsPath(a.dataPath) + ".migrated")
	if a.vault != nil {
		_ = os.Remove(a.vault.Path())
		if nv, err := newSecretVault(a.dataPath); err == nil {
			_ = nv.Load() // 空保险库
			a.vault = nv
		}
	}
	// 来源密钥内存清空。
	a.sourceSecrets = sourcesSecrets{Secrets: map[string]struct {
		Password string `json:"password,omitempty"`
		Key      string `json:"key,omitempty"`
	}{}}

	// 4) 重新生成 access-token，使当前前端立即掉线、回到登录/首次引导。
	tok := []byte(newID() + newID())
	if err := os.WriteFile(AccessTokenPath(a.dataPath), tok, 0600); err == nil {
		a.token = strings.TrimSpace(string(tok))
	}
}

// applyWorkspaceConfigReset 把工作空间配置回到本地默认（local、空路径、无 SSH 凭据引用）。
// 只覆盖配置文件与根绑定，绝不删除 /workspace、/context 下的用户代码与文件。调用方持 a.mu。
func (a *App) applyWorkspaceConfigReset() error {
	a.wsConfig = defaultWorkspaceConfig()
	if err := a.applyWorkspaceConfig(); err != nil {
		return err
	}
	return a.saveWorkspaceConfig()
}

// removeGlob 删除 dir 下匹配 pattern 的所有普通文件（不递归、不删目录）。
// 单个删除失败不阻断其余文件。
func removeGlob(dir, pattern string) error {
	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		return err
	}
	for _, m := range matches {
		_ = os.Remove(m)
	}
	return nil
}

// ── HTTP 端点 ──────────────────────────────────────────────────────────────────

// factoryResetRequest POST /api/factory-reset 的请求体。
type factoryResetRequest struct {
	Scope       ResetScope `json:"scope"`
	ConfirmText string     `json:"confirmText"` // 必须为"重置"
	Password    string     `json:"password,omitempty"`
}

// postFactoryReset POST /api/factory-reset：按范围重置，执行前自动备份，强确认 + 本人密码复核。
func (a *App) postFactoryReset(w http.ResponseWriter, r *http.Request) {
	var in factoryResetRequest
	if err := decode(w, r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	if !in.Scope.any() {
		fail(w, 400, errors.New("请至少选择一项重置范围"))
		return
	}
	if strings.TrimSpace(in.ConfirmText) != "重置" {
		fail(w, 400, errors.New("请准确输入\"重置\"二字以确认"))
		return
	}
	backupPath, needRelogin, err := a.FactoryReset(in.Scope, in.Password)
	if err != nil {
		// 密码错误 401，其余 400。
		if errors.Is(err, errFactoryAuth) {
			fail(w, 401, err)
			return
		}
		fail(w, 400, err)
		return
	}
	jsonOut(w, 200, map[string]any{
		"ok":           true,
		"backupPath":   backupPath,
		"needRelogin":  needRelogin,
		"preservedDirs": []string{"/workspace", "/context"},
	})
}

// errFactoryAuth 本人密码复核失败。
var errFactoryAuth = errors.New("登录密码错误，无法执行重置")

// previewFactoryReset GET /api/factory-reset/preview：返回各范围将影响的文件数量/类型，
// 供弹窗展示"将影响什么"。不做任何修改。
func (a *App) previewFactoryReset(w http.ResponseWriter, r *http.Request) {
	countGlob := func(dir, pattern string) int {
		matches, err := filepath.Glob(filepath.Join(dir, pattern))
		if err != nil {
			return 0
		}
		return len(matches)
	}
	sessions := 0
	for _, dir := range sessionBucketDirs(a.dataPath) {
		sessions += countGlob(dir, "session-*.json")
	}
	memCore := countGlob(MemoryCoreDir(a.dataPath), "*.json")
	a.mu.Lock()
	hasPassword := a.settings.UserPasswordHash != ""
	hasAPIKey := a.settings.APIKey != ""
	hasVault := a.vault != nil && len(a.vault.List()) > 0
	hasWebAuthn := a.webAuthn.enabled() && len(a.webAuthn.store.Credentials) > 0
	a.mu.Unlock()

	jsonOut(w, 200, map[string]any{
		"settings": map[string]any{
			"desc": "模型 / TTS / 主题 / 权限 / 工具轮数 / 推理强度 / 沙箱 / 工作流 / 无障碍",
		},
		"sessionsAndMemory": map[string]any{
			"sessionFiles":  sessions,
			"memoryCoreFiles": memCore,
			"desc":          "全部会话 + aide 核心记忆 + 小秘历史/记忆 + 性格恢复默认",
		},
		"credentialsAndKeys": map[string]any{
			"hasPassword":  hasPassword,
			"hasAPIKey":     hasAPIKey,
			"hasVault":      hasVault,
			"hasWebAuthn":   hasWebAuthn,
			"desc":          "登录密码 / API Key / SSH·vault 凭据 / 来源密钥 / 调试令牌 / WebAuthn / KDF salt / access-token",
		},
		"workspaceConfig": map[string]any{
			"desc": "工作空间连接配置回到本地默认（不删除 /workspace、/context 里的任何文件）",
		},
		"preserved": []string{"/workspace 下的用户代码与文件", "/context 下的用户资料", "自动备份位于 /data/config/backups/"},
	})
}
