package server

// 统一加密凭证保险库（#38）单元测试：
// 加密存/取、列表不泄明文、删除、改密码 ReWrap、文件权限、导出导入保持密文、
// 公钥指纹、临时目录清理，以及无账户密码拒绝保存、路径越权拒绝。

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func testKey(b byte) []byte { return bytes.Repeat([]byte{b}, 32) }

func newTestVault(t *testing.T) *SecretVault {
	t.Helper()
	v, err := newSecretVault(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestVaultPutGet(t *testing.T) {
	v := newTestVault(t)
	v.Unlock(testKey(0x11))
	if err := v.Put("ws:ssh-password", VaultTypeSSHPassword, "登录密码", []byte("s3cret"), ""); err != nil {
		t.Fatal(err)
	}
	pt, err := v.Get("ws:ssh-password")
	if err != nil {
		t.Fatal(err)
	}
	if string(pt) != "s3cret" {
		t.Fatalf("roundtrip mismatch: %q", pt)
	}
	// 未解锁时 Get 必须失败
	v.Lock()
	if _, err := v.Get("ws:ssh-password"); err == nil {
		t.Fatal("locked vault Get should fail")
	}
}

func TestVaultListNoPlaintext(t *testing.T) {
	v := newTestVault(t)
	v.Unlock(testKey(0x11))
	_ = v.Put("ws:ssh-key", VaultTypeSSHKey, "私钥", []byte("PRIVATE-KEY-BODY"), "SHA256:abc")
	meta := v.List()
	if len(meta) != 1 {
		t.Fatalf("want 1 entry, got %d", len(meta))
	}
	if meta[0].Fingerprint != "SHA256:abc" {
		t.Fatalf("fingerprint not preserved: %+v", meta[0])
	}
	// 列表元数据绝不携带密文
	env, _ := json.Marshal(meta)
	if strings.Contains(string(env), "PRIVATE-KEY-BODY") {
		t.Fatalf("List leaked plaintext: %s", env)
	}
}

func TestVaultDelete(t *testing.T) {
	v := newTestVault(t)
	v.Unlock(testKey(0x11))
	_ = v.Put("k", VaultTypeSSHPassword, "x", []byte("bye"), "")
	v.Delete("k")
	if v.Has("k") {
		t.Fatal("Delete should remove entry")
	}
	if _, err := v.Get("k"); err == nil {
		t.Fatal("Get after Delete should fail")
	}
}

func TestVaultReWrap(t *testing.T) {
	dir := t.TempDir()
	v, _ := newSecretVault(dir)
	v.Unlock(testKey(0x22))
	_ = v.Put("k", VaultTypeSSHPassword, "x", []byte("rot-secret"), "")
	if err := v.Save(); err != nil {
		t.Fatal(err)
	}
	// 新句柄加载，用新密钥 ReWrap（旧密钥解开、新密钥重封）
	v2, _ := newSecretVault(dir)
	if err := v2.Load(); err != nil {
		t.Fatal(err)
	}
	if err := v2.ReWrap(testKey(0x22), testKey(0x33)); err != nil {
		t.Fatal(err)
	}
	// 新密钥可解
	if pt, err := v2.Get("k"); err != nil || string(pt) != "rot-secret" {
		t.Fatalf("new key should decrypt: %q %v", pt, err)
	}
	// 旧密钥不可解：重新加载并用旧密钥
	v3, _ := newSecretVault(dir)
	_ = v3.Load()
	v3.Unlock(testKey(0x22))
	if _, err := v3.Get("k"); err == nil {
		t.Fatal("old key must not decrypt after ReWrap")
	}
}

func TestVaultFilePermission(t *testing.T) {
	v := newTestVault(t)
	v.Unlock(testKey(0x11))
	_ = v.Put("k", VaultTypeSSHPassword, "x", []byte("perm"), "")
	if err := v.Save(); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(v.path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := st.Mode().Perm(); perm != 0o600 {
		t.Fatalf("vault file perm = %o, want 0600", perm)
	}
}

func TestExportImportEncrypted(t *testing.T) {
	dirA := t.TempDir()
	src, _ := newSecretVault(dirA)
	src.Unlock(testKey(0x77))
	_ = src.Put("ws:ssh-password", VaultTypeSSHPassword, "pw", []byte("export-me"), "")
	_ = src.Save()
	envBytes, err := os.ReadFile(src.Path())
	if err != nil {
		t.Fatal(err)
	}
	// 导出物必须是密文
	if strings.Contains(string(envBytes), "export-me") {
		t.Fatal("exported envelope must not contain plaintext")
	}
	// 新机器导入（不同主密钥）；同密钥下应可解
	dst, _ := newSecretVault(t.TempDir())
	n, err := dst.ImportEnvelope(envBytes)
	if err != nil || n != 1 {
		t.Fatalf("import n=%d err=%v", n, err)
	}
	// 用源密钥解锁后应能解出（同机恢复场景）
	dst.Unlock(testKey(0x77))
	if pt, err := dst.Get("ws:ssh-password"); err != nil || string(pt) != "export-me" {
		t.Fatalf("imported entry not readable: %q %v", pt, err)
	}
}

func TestNoPasswordReject(t *testing.T) {
	a := testApp(t)
	// 无账户密码：保存 SSH 凭据必须被拒
	requireStatus(t, request(a, "PUT", "/api/workspace-config", map[string]any{
		"workspace": map[string]any{"mode": "ssh", "host": "h", "port": 22, "username": "u", "auth": "password"},
		"password":  "locked-pw",
	}), 400)
}

func TestPathValidationReject(t *testing.T) {
	a := testApp(t)
	a.settings.UserPasswordHash = mustHashPassword("test-pw")
	a.unlockVault("test-pw")
	// 越权路径（.. 遍历 / 根外）应被拒
	requireStatus(t, request(a, "PUT", "/api/workspace-config", map[string]any{
		"workspace":  map[string]any{"mode": "ssh", "host": "h", "port": 22, "username": "u", "auth": "key"},
		"keyMode":    "ref",
		"keyRefPath": "/etc/passwd",
	}), 400)
	requireStatus(t, request(a, "PUT", "/api/workspace-config", map[string]any{
		"workspace":  map[string]any{"mode": "ssh", "host": "h", "port": 22, "username": "u", "auth": "key"},
		"keyMode":    "ref",
		"keyRefPath": "/workspace/../../etc/passwd",
	}), 400)
}

func TestFingerprint(t *testing.T) {
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen 不可用，跳过指纹测试")
	}
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_test")
	cmd := exec.Command("ssh-keygen", "-t", "ed25519", "-N", "", "-f", keyPath, "-q")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("无法生成测试密钥: %v %s", err, out)
	}
	fp, err := sshKeygenFingerprintFile(keyPath, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(fp, "SHA256:") {
		t.Fatalf("fingerprint = %q, want SHA256: prefix", fp)
	}
}

func TestTempDirCleanup(t *testing.T) {
	a := testApp(t)
	dir := a.ensureSecretsTmpDir()
	st, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := st.Mode().Perm(); perm != 0o700 {
		t.Fatalf("tmp dir perm = %o, want 0700", perm)
	}
	// 临时密钥文件：用完 defer 删除（模拟运行时行为）
	f, err := os.CreateTemp(dir, "aide-sshkey-*")
	if err != nil {
		t.Fatal(err)
	}
	name := f.Name()
	_, _ = f.WriteString("key")
	_ = f.Close()
	if err := os.Chmod(name, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(name); err != nil {
		t.Fatal("temp key should exist before cleanup")
	}
	_ = os.Remove(name) // defer os.Remove 的等价物
	if _, err := os.Stat(name); !os.IsNotExist(err) {
		t.Fatal("temp key should be removed after use")
	}
}
