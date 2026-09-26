package server

// ── 统一数据目录布局（data layering）──────────────────────────────────────────
// 权威方案：proposals/data-integrity/data-layering-and-self-healing.html
//
// 本模块是 /data 命名卷内所有路径的唯一权威来源。所有函数均为纯函数：
// 只基于传入的 dataPath 计算绝对路径，不触碰文件系统、无副作用，便于单测。
//
// 目标布局（/data 为状态卷根，aide 私有，0700）：
//
//   auth/                     认证凭据（机密）
//     access-token            本机访问令牌（0600）
//     kdf-salt.bin            Argon2id 固定派生盐（0600）
//     password.phc            Argon2id 密码哈希 PHC（#29）
//     webauthn-credentials.json  Touch ID / WebAuthn 凭证（0600）
//   sessions/
//     active/session-*.json   活动会话
//     archived/session-*.json 归档会话
//     assistant/session-*.json 小蜜系统会话（#30）
//   assistant/                小蜜私有区（全局、加密）
//     voice-history.json      小蜜对话历史信封
//     voice-memory.json       小蜜长期记忆
//   memory/
//     core/                   核心记忆
//     cache/                  嵌入/向量缓存（可再生）
//     feedback/               好/坏回答反馈
//   config/
//     settings.json           全局设置（原子写）
//     profiles.json           模型自定义配置
//     backups/                配置导入导出与历史副本
//   stats/                    统计（可再生）
//     token-stats.json
//     token-pricing.json
//   audit/                    审计日志（追加写）
//     debug-audit.jsonl
//     security-audit.jsonl
//   secrets/                  第三方来源/工作区密钥（加密）
//     vault.enc
//     sources-secrets.json
//     workspace-secrets.json
//   certs/                    TLS 证书与私钥（#29，0600）
//     cert.pem / key.pem
//   .integrity/               完整性清单 / 基线 / 恢复日志
//   .quarantine/              损坏用户数据隔离区（不自动删）
//
// 迁移完成前，旧平铺文件仍在 /data 根；migration.go 负责把它们归位。

import (
	"os"
	"path/filepath"
)

// ── 目录相对名常量（与方案树一一对应）─────────────────────────────────────────

const (
	authDirName      = "auth"
	sessionsDirName  = "sessions"
	activeDirName    = "active"
	archivedDirName  = "archived"
	assistantSessDir = "assistant" // sessions/assistant（小蜜系统会话）

	assistantDirName = "assistant"
	memoryDirName    = "memory"
	coreDirName      = "core"
	cacheDirName     = "cache"
	feedbackDirName  = "feedback"

	configDirName     = "config"
	backupsDirName    = "backups"
	statsDirName      = "stats"
	auditDirName      = "audit"
	secretsDirName    = "secrets"
	certsDirName      = "certs"
	integrityDirName  = ".integrity"
	quarantineDirName = ".quarantine"
	commentsDirName   = "comments" // #63 侧车批注（跨格式：docx/xlsx/pptx/pdf 通用）
)

// 文件名常量（平铺期与分层期共用同一文件名，仅所在目录变化）。
const (
	AccessTokenFileName      = "access-token"
	KdfSaltFileName          = "kdf-salt.bin"
	PasswordPHCFileName      = "password.phc"
	SettingsFileName         = "settings.json"
	PersonalityStateFileName = "personality-state.json" // 性格演化触发计数/回滚状态（#34，明文，config 目录）
	VoiceHistoryFileName     = "voice-history.json"
	VoiceMemoryFileName      = "voice-memory.json"
	DebugAuditFileName       = "debug-audit.jsonl"
	SecurityAuditFileName    = "security-audit.jsonl"
	TokenStatsFileName       = "token-stats.json"
	TokenPricingFileName     = "token-pricing.json"
	VaultFileName            = "vault.enc"
	SourcesSecretsFile       = "sources-secrets.json"
	WorkspaceSecretsFile     = "workspace-secrets.json"
	WebAuthnCredsFile        = "webauthn-credentials.json"
	CertFileName             = "cert.pem"
	KeyFileName              = "key.pem"
)

// ── 目录解析（绝对路径）───────────────────────────────────────────────────────

func AuthDir(data string) string     { return filepath.Join(data, authDirName) }
func SessionsDir(data string) string { return filepath.Join(data, sessionsDirName) }
func SessionsActiveDir(data string) string {
	return filepath.Join(data, sessionsDirName, activeDirName)
}
func SessionsArchivedDir(data string) string {
	return filepath.Join(data, sessionsDirName, archivedDirName)
}
func SessionsAssistantDir(data string) string {
	return filepath.Join(data, sessionsDirName, assistantSessDir)
}
func AssistantDir(data string) string   { return filepath.Join(data, assistantDirName) }
func MemoryDir(data string) string      { return filepath.Join(data, memoryDirName) }
func MemoryCoreDir(data string) string  { return filepath.Join(data, memoryDirName, coreDirName) }
func MemoryCacheDir(data string) string { return filepath.Join(data, memoryDirName, cacheDirName) }
func MemoryFeedbackDir(data string) string {
	return filepath.Join(data, memoryDirName, feedbackDirName)
}
func ConfigDir(data string) string        { return filepath.Join(data, configDirName) }
func ConfigBackupsDir(data string) string { return filepath.Join(data, configDirName, backupsDirName) }
func StatsDir(data string) string         { return filepath.Join(data, statsDirName) }
func AuditDir(data string) string         { return filepath.Join(data, auditDirName) }
func CommentsDir(data string) string      { return filepath.Join(data, commentsDirName) } // #63 侧车批注根目录
func SecretsDir(data string) string       { return filepath.Join(data, secretsDirName) }
func CertsDir(data string) string         { return filepath.Join(data, certsDirName) }
func IntegrityDir(data string) string     { return filepath.Join(data, integrityDirName) }
func QuarantineDir(data string) string    { return filepath.Join(data, quarantineDirName) }

// ── 文件解析（绝对路径，纯函数）───────────────────────────────────────────────

// AccessTokenPath 本机访问令牌：auth/access-token。
func AccessTokenPath(data string) string { return filepath.Join(AuthDir(data), AccessTokenFileName) }

// KdfSaltPath Argon2id 固定派生盐：auth/kdf-salt.bin。
func KdfSaltPath(data string) string { return filepath.Join(AuthDir(data), KdfSaltFileName) }

// PasswordPHCPath 账户密码 Argon2id PHC 哈希：auth/password.phc（#29 写入）。
func PasswordPHCPath(data string) string { return filepath.Join(AuthDir(data), PasswordPHCFileName) }

// WebAuthnCredsPath Touch ID / WebAuthn 凭证：auth/webauthn-credentials.json。
func WebAuthnCredsPath(data string) string { return filepath.Join(AuthDir(data), WebAuthnCredsFile) }

// SettingsPath 全局设置：config/settings.json。
func SettingsPath(data string) string { return filepath.Join(ConfigDir(data), SettingsFileName) }

// PersonalityStatePath 性格演化触发计数与回滚历史：config/personality-state.json（#34，明文）。
// 与 settings.json 同属 config 目录（aide 性格明文层）；小秘语音历史仍加密存 assistant/。
func PersonalityStatePath(data string) string {
	return filepath.Join(ConfigDir(data), PersonalityStateFileName)
}

// PersonalityAuditPath 性格演化审计日志：audit/personality-audit.jsonl（追加写）。
func PersonalityAuditPath(data string) string {
	return filepath.Join(AuditDir(data), "personality-audit.jsonl")
}

// VoiceHistoryPath 小蜜对话历史信封：assistant/voice-history.json。
func VoiceHistoryPath(data string) string {
	return filepath.Join(AssistantDir(data), VoiceHistoryFileName)
}

// VoiceMemoryPath 小蜜长期记忆：assistant/voice-memory.json。
func VoiceMemoryPath(data string) string {
	return filepath.Join(AssistantDir(data), VoiceMemoryFileName)
}

// DebugAuditPath 外部接入审计：audit/debug-audit.jsonl。
func DebugAuditPath(data string) string { return filepath.Join(AuditDir(data), DebugAuditFileName) }

// SecurityAuditPath 安全/恢复审计：audit/security-audit.jsonl。
func SecurityAuditPath(data string) string {
	return filepath.Join(AuditDir(data), SecurityAuditFileName)
}

// TokenStatsPath Token 消耗明细：stats/token-stats.json。
func TokenStatsPath(data string) string { return filepath.Join(StatsDir(data), TokenStatsFileName) }

// TokenPricingPath 模型计价表：stats/token-pricing.json。
func TokenPricingPath(data string) string { return filepath.Join(StatsDir(data), TokenPricingFileName) }

// SourcesSecretsPath 来源密钥：secrets/sources-secrets.json。
func SourcesSecretsPath(data string) string {
	return filepath.Join(SecretsDir(data), SourcesSecretsFile)
}

// WorkspaceSecretsPath 工作区旧明文密钥：secrets/workspace-secrets.json。
func WorkspaceSecretsPath(data string) string {
	return filepath.Join(SecretsDir(data), WorkspaceSecretsFile)
}

// VaultPath 统一加密凭证保险库：secrets/vault.enc。
func VaultPath(data string) string { return filepath.Join(SecretsDir(data), VaultFileName) }

// TLSCertPath TLS 证书：certs/cert.pem。
// 注意：当前运行期仍由 #29/B 任务写入 data/tls/；此处给出目标位置，待 B 完成后对齐。
func TLSCertPath(data string) string { return filepath.Join(CertsDir(data), CertFileName) }

// TLSKeyPath TLS 私钥：certs/key.pem。
func TLSKeyPath(data string) string { return filepath.Join(CertsDir(data), KeyFileName) }

// SessionPath 会话文件：sessions/<bucket>/session-<id>.json。
// bucket ∈ "active" | "archived" | "assistant"；非法 bucket 回退 active。
func SessionPath(data, id, bucket string) string {
	var dir string
	switch bucket {
	case "archived":
		dir = SessionsArchivedDir(data)
	case "assistant":
		dir = SessionsAssistantDir(data)
	default:
		dir = SessionsActiveDir(data)
	}
	return filepath.Join(dir, "session-"+id+".json")
}

// sessionBucketDirs 所有存放会话文件的目录（active/archived/assistant），用于启动时 Glob。
func sessionBucketDirs(data string) []string {
	return []string{SessionsActiveDir(data), SessionsArchivedDir(data), SessionsAssistantDir(data)}
}

// baselinePath 完整性基线清单：.integrity/baseline.json。
func baselinePath(data string) string { return filepath.Join(IntegrityDir(data), "baseline.json") }

// migrationStatePath 迁移标记：.integrity/migration-state.json。
func migrationStatePath(data string) string {
	return filepath.Join(IntegrityDir(data), "migration-state.json")
}

// recoveryLogPath 完整性恢复日志：.integrity/recovery.log。
func recoveryLogPath(data string) string { return filepath.Join(IntegrityDir(data), "recovery.log") }

// ── 目录创建辅助 ─────────────────────────────────────────────────────────────

// layeredDirs 所有需要保证存在的分层目录（0700）。顺序无关，MkdirAll 幂等。
func layeredDirs(data string) []string {
	return []string{
		AuthDir(data),
		SessionsDir(data), SessionsActiveDir(data), SessionsArchivedDir(data), SessionsAssistantDir(data),
		AssistantDir(data),
		MemoryDir(data), MemoryCoreDir(data), MemoryCacheDir(data), MemoryFeedbackDir(data),
		ConfigDir(data), ConfigBackupsDir(data),
		StatsDir(data),
		AuditDir(data),
		CommentsDir(data), // #63 侧车批注（0700）
		SecretsDir(data),
		CertsDir(data),
		IntegrityDir(data),
		QuarantineDir(data),
	}
}

// EnsureDirs 创建全部分层目录（0700）。幂等：已存在不报错。
// 必须在任何按分层路径写盘之前调用，否则 atomicJSON 的临时文件落不进目录。
func EnsureDirs(data string) error {
	for _, d := range layeredDirs(data) {
		if err := os.MkdirAll(d, 0700); err != nil {
			return err
		}
	}
	return nil
}
