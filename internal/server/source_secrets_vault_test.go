package server

// 每来源凭据并入加密 vault（finding backend-integrations-003 / P2 #1）单元测试：
//   - 明文 → vault 迁移成功后明文被清除且 Get 可取回
//   - vault 不可用（锁定）时回退明文、不丢配置、迁移静默保留待重试
//   - 重复迁移幂等（不产生重复/损坏条目）
//   - store/clear 同步写 vault；删除来源 sweep 掉密文

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeLegacySourceSecrets 把一份旧明文来源凭据写到磁盘回退文件，并同步进内存 map，
// 模拟 loadSources 之后、迁移之前的状态。
func writeLegacySourceSecrets(t *testing.T, a *App, id, password, key string) {
	t.Helper()
	a.sourceSecrets.Secrets[id] = sourceSecretEntry{Password: password, Key: key}
	b, err := json.MarshalIndent(a.sourceSecrets, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(SourcesSecretsPath(a.dataPath), b, 0600); err != nil {
		t.Fatal(err)
	}
}

// 1) 明文 → vault：迁移成功后 vault 可取回、内存明文清除、落盘文件不再含明文。
func TestSourceSecretsMigrationPlaintextToVault(t *testing.T) {
	a := testApp(t)
	if !a.vaultIsUnlocked() {
		t.Skip("无密码场景下 vault 应由 access-token 自动解锁；本测试依赖解锁态")
	}
	writeLegacySourceSecrets(t, a, "ftp1", "pw-legacy-001", "")
	a.migrateLegacySourceSecretsLocked()
	// vault 可取回
	if !a.vault.Has(sourceVaultID("ftp1")) {
		t.Fatal("迁移后 vault 应有 source:ftp1 条目")
	}
	pt, err := a.vault.Get(sourceVaultID("ftp1"))
	if err != nil {
		t.Fatalf("vault.Get 失败: %v", err)
	}
	var payload sourceCredPayload
	if err := json.Unmarshal(pt, &payload); err != nil {
		t.Fatalf("载荷不是有效 JSON: %v", err)
	}
	if payload.Password != "pw-legacy-001" {
		t.Fatalf("解出密码=%q", payload.Password)
	}
	// 内存明文已清除
	if _, ok := a.sourceSecrets.Secrets["ftp1"]; ok {
		t.Fatal("迁移后内存回退明文应已清除")
	}
	// 落盘文件不再含明文
	onDisk, err := os.ReadFile(SourcesSecretsPath(a.dataPath))
	if err != nil {
		t.Fatalf("读 sources-secrets.json 失败: %v", err)
	}
	if strings.Contains(string(onDisk), "pw-legacy-001") {
		t.Fatalf("迁移后 sources-secrets.json 仍含明文: %s", onDisk)
	}
	// 统一读取点能从 vault 取回
	pw, _ := a.sourceCredentialLocked("ftp1")
	if pw != "pw-legacy-001" {
		t.Fatalf("sourceCredentialLocked 取回=%q", pw)
	}
	if !a.sourceHasSecretLocked("ftp1") {
		t.Fatal("hasSecret 应为 true")
	}
}

// 2) vault 锁定（不可用）：回退明文、不丢配置；解锁后再迁移成功。
func TestSourceSecretsFallbackWhenVaultLocked(t *testing.T) {
	a := testApp(t)
	writeLegacySourceSecrets(t, a, "ftp2", "pw-locked-002", "")
	// 锁定 vault，模拟启动时 vault 不可用（有账户密码但尚未解锁）。
	a.vault.Lock()
	if a.vaultIsUnlocked() {
		t.Fatal("前置：vault 应处于锁定态")
	}
	// 迁移应静默跳过：不报错、不动内存明文、不写 vault。
	a.migrateLegacySourceSecretsLocked()
	if a.vault.Has(sourceVaultID("ftp2")) {
		t.Fatal("vault 锁定时迁移不应写入条目")
	}
	if _, ok := a.sourceSecrets.Secrets["ftp2"]; !ok {
		t.Fatal("vault 锁定时必须保留内存明文回退，配置不能失效")
	}
	// 统一读取点回退到明文：来源仍可连接。
	pw, _ := a.sourceCredentialLocked("ftp2")
	if pw != "pw-locked-002" {
		t.Fatalf("锁定态回退明文=%q", pw)
	}
	// 解锁（access-token 自动解锁路径）后重试迁移。
	if err := a.unlockVaultAtStartup(); err != nil {
		t.Fatalf("自动解锁失败: %v", err)
	}
	if !a.vaultIsUnlocked() {
		t.Skip("无法在当前测试环境解锁 vault")
	}
	a.migrateLegacySourceSecretsLocked()
	if !a.vault.Has(sourceVaultID("ftp2")) {
		t.Fatal("解锁后迁移应写入 vault")
	}
	if _, ok := a.sourceSecrets.Secrets["ftp2"]; ok {
		t.Fatal("解锁迁移后内存明文应清除")
	}
}

// 3) 幂等：重复迁移不产生重复条目、不损坏、明文只清一次。
func TestSourceSecretsMigrationIdempotent(t *testing.T) {
	a := testApp(t)
	if !a.vaultIsUnlocked() {
		t.Skip("依赖 vault 解锁态")
	}
	writeLegacySourceSecrets(t, a, "ftp3", "pw-idem-003", "KEY-BODY")
	a.migrateLegacySourceSecretsLocked()
	before := a.vault.List()
	if !a.vault.Has(sourceVaultID("ftp3")) {
		t.Fatal("首次迁移应写入 vault")
	}
	// 再次迁移：vault 已有条目即跳过，不重复、不报错。
	a.migrateLegacySourceSecretsLocked()
	a.migrateLegacySourceSecretsLocked()
	after := a.vault.List()
	if len(before) != len(after) {
		t.Fatalf("重复迁移产生条目差异: before=%d after=%d", len(before), len(after))
	}
	pw, key := a.sourceCredentialLocked("ftp3")
	if pw != "pw-idem-003" || key != "KEY-BODY" {
		t.Fatalf("幂等后取回 pw=%q key=%q", pw, key)
	}
}

// 4) store/clear 同步写 vault；明文不回退到磁盘回退文件。
func TestSourceCredentialStoreAndClear(t *testing.T) {
	a := testApp(t)
	if !a.vaultIsUnlocked() {
		t.Skip("依赖 vault 解锁态")
	}
	a.storeSourceCredentialLocked("sftp1", "pw-store-004", "KEY-BODY-004")
	if !a.vault.Has(sourceVaultID("sftp1")) {
		t.Fatal("store 后 vault 应有条目")
	}
	// 已解锁密封成功 → 内存明文应清除
	if _, ok := a.sourceSecrets.Secrets["sftp1"]; ok {
		t.Fatal("store 成功后内存明文应清除")
	}
	pw, key := a.sourceCredentialLocked("sftp1")
	if pw != "pw-store-004" || key != "KEY-BODY-004" {
		t.Fatalf("store 取回 pw=%q key=%q", pw, key)
	}
	// 清空
	a.storeSourceCredentialLocked("sftp1", "", "")
	if a.vault.Has(sourceVaultID("sftp1")) {
		t.Fatal("clear 后 vault 应无条目")
	}
	if pw, key := a.sourceCredentialLocked("sftp1"); pw != "" || key != "" {
		t.Fatalf("clear 后取回 pw=%q key=%q", pw, key)
	}
}

// 5) 删除来源时 sweep 掉不再存活条目的密文与明文。
func TestSourceCredentialSweepRemoved(t *testing.T) {
	a := testApp(t)
	writeLegacySourceSecrets(t, a, "gone1", "pw-gone-005", "")
	a.migrateLegacySourceSecretsLocked()
	if !a.vault.Has(sourceVaultID("gone1")) {
		t.Fatal("前置：gone1 应已在 vault")
	}
	// 存活集合只包含内置来源（context / system-docs），gone1 被 sweep。
	alive := map[string]bool{contextSource: true, systemDocsSource: true}
	a.sweepSourceCredentialsLocked(alive)
	if a.vault.Has(sourceVaultID("gone1")) {
		t.Fatal("sweep 后 vault 不应再有已删除来源条目")
	}
	if _, ok := a.sourceSecrets.Secrets["gone1"]; ok {
		t.Fatal("sweep 后内存明文不应再有已删除来源")
	}
}

func sourceVaultPatchFixture(t *testing.T) (*App, map[string]any, string) {
	t.Helper()
	a := testApp(t)
	if !a.vaultIsUnlocked() {
		t.Fatal("source credential patch fixture requires an unlocked vault")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "ssh.log")
	a.sshBin = filepath.Join(dir, "ssh")
	writeStub(t, a.sshBin, "#!/bin/sh\nprintf '%s\\n' \"$*\" >> "+shellQuote(log)+"\nexit 0\n")
	src := sourceBody("vault-patch", "sealed source", "sftp", map[string]any{"host": "fixture.invalid", "port": 22, "username": "reader", "auth": "key", "path": "/docs"}, true)
	requireStatus(t, request(a, http.MethodPut, "/api/sources", map[string]any{
		"sources": []any{src}, "secrets": map[string]any{"vault-patch": map[string]any{"password": "fixture-password", "key": "fixture-key"}},
	}), http.StatusOK)
	if _, ok := a.sourceSecrets.Secrets["vault-patch"]; ok {
		t.Fatal("sealed fixture must not retain fallback plaintext")
	}
	return a, src, log
}

func TestUpdateSourcesPartialVaultCredentials(t *testing.T) {
	a, src, log := sourceVaultPatchFixture(t)
	for _, tt := range []struct {
		patch         map[string]any
		password, key string
	}{
		{map[string]any{"password": "fixture-new-password"}, "fixture-new-password", "fixture-key"},
		{map[string]any{"key": "fixture-new-key"}, "fixture-new-password", "fixture-new-key"},
	} {
		generation := a.sshSessionGeneration(sourceSocket("vault-patch"))
		if err := os.WriteFile(log, nil, 0600); err != nil {
			t.Fatal(err)
		}
		requireStatus(t, request(a, http.MethodPut, "/api/sources", map[string]any{"sources": []any{src}, "secrets": map[string]any{"vault-patch": tt.patch}}), http.StatusOK)
		a.mu.Lock()
		password, key := a.sourceCredentialLocked("vault-patch")
		_, plaintext := a.sourceSecrets.Secrets["vault-patch"]
		a.mu.Unlock()
		if password != tt.password || key != tt.key || plaintext {
			t.Fatal("partial update lost its omitted field or retained plaintext")
		}
		if a.sshSessionGeneration(sourceSocket("vault-patch")) != generation+1 {
			t.Fatal("partial credential update did not invalidate its master generation")
		}
		b, err := os.ReadFile(log)
		if err != nil || !strings.Contains(string(b), "-O exit") {
			t.Fatal("partial credential update did not retire its old master")
		}
	}
}

func TestUpdateSourcesEmptyVaultCredentialPatchIsNoop(t *testing.T) {
	a, src, _ := sourceVaultPatchFixture(t)
	before, err := os.ReadFile(a.vault.Path())
	if err != nil {
		t.Fatal(err)
	}
	generation := a.sshSessionGeneration(sourceSocket("vault-patch"))
	requireStatus(t, request(a, http.MethodPut, "/api/sources", map[string]any{"sources": []any{src}, "secrets": map[string]any{"vault-patch": map[string]any{}}}), http.StatusOK)
	after, err := os.ReadFile(a.vault.Path())
	if err != nil || !bytes.Equal(before, after) || a.sshSessionGeneration(sourceSocket("vault-patch")) != generation {
		t.Fatal("empty credential patch changed a sealed entry or retired its master")
	}
	a.mu.Lock()
	password, key := a.sourceCredentialLocked("vault-patch")
	a.mu.Unlock()
	if password != "fixture-password" || key != "fixture-key" {
		t.Fatal("empty credential patch cleared existing credentials")
	}
}

func TestUpdateSourcesUnreadablePartialVaultPatchIsAtomic(t *testing.T) {
	for _, mode := range []string{"locked", "invalid-payload"} {
		t.Run(mode, func(t *testing.T) {
			a, src, log := sourceVaultPatchFixture(t)
			other := sourceBody("vault-patch-other", "other sealed source", "sftp", map[string]any{"host": "other.invalid", "auth": "key", "path": "/docs"}, true)
			requireStatus(t, request(a, http.MethodPut, "/api/sources", map[string]any{"sources": []any{src, other}, "secrets": map[string]any{"vault-patch-other": map[string]any{"password": "fixture-other-password", "key": "fixture-other-key"}}}), http.StatusOK)
			if mode == "locked" {
				a.vault.Lock()
			} else {
				if err := a.vault.Put(sourceVaultID("vault-patch"), VaultTypeSourceSecret, "fixture malformed payload", []byte("not-json"), ""); err != nil {
					t.Fatal(err)
				}
				if err := a.vault.Save(); err != nil {
					t.Fatal(err)
				}
			}
			paths := []string{a.vault.Path(), a.sourcesPath(), a.sourcesSecretsPath()}
			before := make(map[string][]byte, len(paths))
			for _, path := range paths {
				b, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				before[path] = b
			}
			generation := a.sshSessionGeneration(sourceSocket("vault-patch-other"))
			if err := os.WriteFile(log, nil, 0600); err != nil {
				t.Fatal(err)
			}
			src["name"] = "must not be saved"
			response := request(a, http.MethodPut, "/api/sources", map[string]any{
				"sources": []any{src, other},
				"secrets": map[string]any{"vault-patch": map[string]any{"password": "fixture-partial"}, "vault-patch-other": map[string]any{"clear": true}},
			})
			want := http.StatusLocked
			if mode == "invalid-payload" {
				want = http.StatusInternalServerError
			}
			requireStatus(t, response, want)
			for _, path := range paths {
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(before[path], after) {
					t.Fatal("failed preflight partially saved source credentials or registry")
				}
			}
			if !a.vault.Has(sourceVaultID("vault-patch-other")) || a.sshSessionGeneration(sourceSocket("vault-patch-other")) != generation {
				t.Fatal("failed batch cleared another source or retired its master")
			}
			b, err := os.ReadFile(log)
			if err != nil || len(b) != 0 {
				t.Fatal("failed preflight performed SSH teardown")
			}
		})
	}
}

func TestUpdateSourcesClearLockedVaultCredentials(t *testing.T) {
	a, src, _ := sourceVaultPatchFixture(t)
	a.vault.Lock()
	requireStatus(t, request(a, http.MethodPut, "/api/sources", map[string]any{"sources": []any{src}, "secrets": map[string]any{"vault-patch": map[string]any{"clear": true}}}), http.StatusOK)
	if a.vault.Has(sourceVaultID("vault-patch")) {
		t.Fatal("explicit clear must remain available without decrypting the old credentials")
	}
}
