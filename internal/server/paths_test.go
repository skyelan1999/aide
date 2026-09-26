package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPathsLayout 所有路径函数返回正确的分层路径（纯函数，基于 dataPath 拼接）。
func TestPathsLayout(t *testing.T) {
	data := t.TempDir()
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"AccessToken", AccessTokenPath(data), "auth/access-token"},
		{"KdfSalt", KdfSaltPath(data), "auth/kdf-salt.bin"},
		{"PasswordPHC", PasswordPHCPath(data), "auth/password.phc"},
		{"WebAuthn", WebAuthnCredsPath(data), "auth/webauthn-credentials.json"},
		{"Settings", SettingsPath(data), "config/settings.json"},
		{"VoiceHistory", VoiceHistoryPath(data), "assistant/voice-history.json"},
		{"VoiceMemory", VoiceMemoryPath(data), "assistant/voice-memory.json"},
		{"DebugAudit", DebugAuditPath(data), "audit/debug-audit.jsonl"},
		{"SecurityAudit", SecurityAuditPath(data), "audit/security-audit.jsonl"},
		{"TokenStats", TokenStatsPath(data), "stats/token-stats.json"},
		{"TokenPricing", TokenPricingPath(data), "stats/token-pricing.json"},
		{"SourcesSecrets", SourcesSecretsPath(data), "secrets/sources-secrets.json"},
		{"WorkspaceSecrets", WorkspaceSecretsPath(data), "secrets/workspace-secrets.json"},
		{"Vault", VaultPath(data), "secrets/vault.enc"},
		{"TLSCert", TLSCertPath(data), "certs/cert.pem"},
		{"TLSKey", TLSKeyPath(data), "certs/key.pem"},
		{"SessionActive", SessionPath(data, "abc123", "active"), "sessions/active/session-abc123.json"},
		{"SessionArchived", SessionPath(data, "abc123", "archived"), "sessions/archived/session-abc123.json"},
		{"SessionAssistant", SessionPath(data, "abc123", "assistant"), "sessions/assistant/session-abc123.json"},
		{"SessionDefaultBucket", SessionPath(data, "abc123", "weird"), "sessions/active/session-abc123.json"},
	}
	for _, c := range cases {
		want := filepath.Join(data, filepath.FromSlash(c.want))
		if c.got != want {
			t.Errorf("%s: got %q want %q", c.name, c.got, want)
		}
	}
}

// TestEnsureDirsCreatesAllLayeredDirs EnsureDirs 创建全部分层目录且权限 0700。
func TestEnsureDirsCreatesAllLayeredDirs(t *testing.T) {
	data := t.TempDir()
	if err := EnsureDirs(data); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{
		AuthDir(data), SessionsActiveDir(data), SessionsArchivedDir(data), SessionsAssistantDir(data),
		AssistantDir(data), MemoryCoreDir(data), MemoryCacheDir(data), MemoryFeedbackDir(data),
		ConfigDir(data), ConfigBackupsDir(data), StatsDir(data), AuditDir(data),
		SecretsDir(data), CertsDir(data), IntegrityDir(data), QuarantineDir(data),
	} {
		st, err := os.Stat(d)
		if err != nil {
			t.Errorf("目录未创建: %s: %v", d, err)
			continue
		}
		if !st.IsDir() {
			t.Errorf("不是目录: %s", d)
		}
		if perm := st.Mode().Perm(); perm != 0700 {
			t.Errorf("目录权限 %o  want 0700: %s", perm, d)
		}
	}
	// 幂等：再调一次不报错
	if err := EnsureDirs(data); err != nil {
		t.Fatalf("重复 EnsureDirs: %v", err)
	}
}

// TestPathsPureFunctionsNoSideEffect 路径函数不创建/触碰文件。
func TestPathsPureFunctionsNoSideEffect(t *testing.T) {
	data := filepath.Join(t.TempDir(), "notcreated")
	_ = SettingsPath(data)
	_ = AccessTokenPath(data)
	_ = SessionPath(data, "x", "active")
	if _, err := os.Stat(data); !os.IsNotExist(err) {
		t.Fatal("路径函数不应创建目录")
	}
}

// 确保 strings 被引用（joinMissing 等在 integrity.go 使用）。
var _ = strings.TrimSpace
