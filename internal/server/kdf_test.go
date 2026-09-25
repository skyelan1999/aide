package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestArgon2idHashVerify：哈希后正确密码通过、错误密码拒绝。
func TestArgon2idHashVerify(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	valid, needsUpgrade := VerifyPassword("correct horse battery staple", hash)
	if !valid || needsUpgrade {
		t.Fatalf("correct password: valid=%v needsUpgrade=%v, want true/false", valid, needsUpgrade)
	}
	valid, _ = VerifyPassword("wrong password", hash)
	if valid {
		t.Fatal("wrong password must be rejected")
	}
}

// TestPHCFormat：输出符合 $argon2id$v=19$m=65536,t=3,p=4$... 格式。
func TestPHCFormat(t *testing.T) {
	hash, err := HashPassword("pw-for-format")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	parts := strings.Split(hash, "$")
	// ["", "argon2id", "v=19", "m=...,t=...,p=...", salt, hash]
	if len(parts) != 6 {
		t.Fatalf("PHC parts = %d, want 6: %q", len(parts), hash)
	}
	if parts[1] != "argon2id" {
		t.Fatalf("algo = %q, want argon2id", parts[1])
	}
	if parts[2] != "v=19" {
		t.Fatalf("version = %q, want v=19", parts[2])
	}
	if parts[3] != "m=65536,t=3,p=4" {
		t.Fatalf("params = %q, want m=65536,t=3,p=4", parts[3])
	}
	if parts[4] == "" || parts[5] == "" {
		t.Fatal("salt/hash must not be empty")
	}
}

// TestLegacyDetection：64hex 识别为旧格式，PHC 与空串不识别。
func TestLegacyDetection(t *testing.T) {
	legacy := strings.Repeat("a", 64) // 64 位 hex
	if !IsLegacyHash(legacy) {
		t.Fatal("64hex should be detected as legacy")
	}
	phc, _ := HashPassword("x")
	if IsLegacyHash(phc) {
		t.Fatal("PHC must not be detected as legacy")
	}
	if IsLegacyHash("") {
		t.Fatal("empty must not be legacy")
	}
	if IsLegacyHash(strings.Repeat("z", 64)) {
		t.Fatal("non-hex 64 chars must not be legacy")
	}
	if IsLegacyHash(strings.Repeat("a", 63)) {
		t.Fatal("63 chars must not be legacy")
	}
}

// TestMigrationFlow：旧 SHA-256 哈希 → VerifyPassword 返回 needsUpgrade → 升级后新格式验证通过。
func TestMigrationFlow(t *testing.T) {
	pw := "skla123" // 仅迁移测试用
	legacy := sha256Hex(pw)
	if !IsLegacyHash(legacy) {
		t.Fatalf("precondition: %q must be legacy", legacy)
	}
	valid, needsUpgrade := VerifyPassword(pw, legacy)
	if !valid || !needsUpgrade {
		t.Fatalf("legacy verify: valid=%v needsUpgrade=%v, want true/true", valid, needsUpgrade)
	}
	// 错误旧密码
	if bad, _ := VerifyPassword("nope", legacy); bad {
		t.Fatal("bad legacy password must be rejected")
	}
	// 升级为 Argon2id PHC
	newHash, err := HashPassword(pw)
	if err != nil {
		t.Fatalf("rehash: %v", err)
	}
	if IsLegacyHash(newHash) {
		t.Fatal("upgraded hash must not be legacy")
	}
	valid2, needsUpgrade2 := VerifyPassword(pw, newHash)
	if !valid2 || needsUpgrade2 {
		t.Fatalf("upgraded verify: valid=%v needsUpgrade=%v, want true/false", valid2, needsUpgrade2)
	}
	if bad, _ := VerifyPassword("nope", newHash); bad {
		t.Fatal("bad password after upgrade must be rejected")
	}
}

// TestReWrapCiphertext：用旧密钥加密 → re-wrap → 新密钥解密成功。
func TestReWrapCiphertext(t *testing.T) {
	pw := "rewrap-secret"
	oldKey := DeriveAESKeyLegacy(pw)
	salt := []byte("0123456789abcdef")
	newKey := DeriveAESKey(pw, salt)
	if string(oldKey) == string(newKey) {
		t.Fatal("old and new keys must differ")
	}
	plain := []byte("小秘历史：今天买了牛奶")
	ct, err := encryptWithKey(oldKey, plain)
	if err != nil {
		t.Fatalf("encrypt old: %v", err)
	}
	// 旧密钥可解
	if _, err := decryptWithKey(oldKey, ct); err != nil {
		t.Fatalf("old key should decrypt: %v", err)
	}
	// 新密钥此时解不开旧密文
	if _, err := decryptWithKey(newKey, ct); err == nil {
		t.Fatal("new key must not decrypt old ciphertext")
	}
	// re-wrap：旧 key 解 → 新 key 封
	pt, err := decryptWithKey(oldKey, ct)
	if err != nil {
		t.Fatalf("pre-rewrap decrypt: %v", err)
	}
	newCt, err := encryptWithKey(newKey, pt)
	if err != nil {
		t.Fatalf("rewrap encrypt: %v", err)
	}
	// re-wrap 后新密钥可解、旧密钥解不开
	got, err := decryptWithKey(newKey, newCt)
	if err != nil {
		t.Fatalf("new key must decrypt re-wrapped: %v", err)
	}
	if string(got) != string(plain) {
		t.Fatalf("roundtrip mismatch: %q", got)
	}
	if _, err := decryptWithKey(oldKey, newCt); err == nil {
		t.Fatal("old key must not decrypt re-wrapped ciphertext")
	}
}

// TestReWrapAllVoice：VoiceAgent.ReWrapAll 在旧密钥密文上重加密后，新密钥可解锁。
// 手动用旧 SHA-256 密钥构造 voice-history.json，不依赖包级 kdfSalt，保证测试隔离。
func TestReWrapAllVoice(t *testing.T) {
	dir := t.TempDir()
	oldPw := "voice-old-pw"
	oldKey := DeriveAESKeyLegacy(oldPw) // 旧版派生
	salt := []byte("fedcba9876543210")
	newKey := DeriveAESKey(oldPw, salt) // 新版 Argon2id 派生
	if string(newKey) == string(oldKey) {
		t.Fatal("new key must differ from legacy")
	}
	// 用旧密钥加密一条历史，手写加密信封落盘
	hist := []VoiceHistoryEntry{{Heard: "旧密钥时期的一条小秘记录", Action: "send"}}
	b, err := json.Marshal(hist)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	cipher, err := encryptWithKey(oldKey, b)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	env, _ := json.Marshal(voiceHistoryFile{Encrypted: true, Cipher: cipher})
	if err := os.WriteFile(filepath.Join(dir, "voice-history.json"), env, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// 从文件重载为锁定态
	locked := newVoiceAgent(dir)
	if st := locked.encStatus(); st["encrypted"] != true || st["unlocked"] != false {
		t.Fatalf("reload should be encrypted+locked, got %v", st)
	}
	// 重加密：旧 key 解 → 新 key 封
	if err := locked.ReWrapAll(oldKey, newKey); err != nil {
		t.Fatalf("ReWrapAll: %v", err)
	}
	// 旧密钥此时应解不开（已重加密）
	if _, err := decryptWithKey(oldKey, locked.cachedCipher); err == nil {
		t.Fatal("old key must not open after re-wrap")
	}
	// 新密钥可解开且内容完好
	got, err := decryptWithKey(newKey, locked.cachedCipher)
	if err != nil {
		t.Fatalf("new key must open after re-wrap: %v", err)
	}
	if !strings.Contains(string(got), "旧密钥时期") {
		t.Fatalf("re-wrapped content lost plaintext: %s", got)
	}
	// 无密文时 ReWrapAll 必须安全跳过
	va2 := newVoiceAgent(t.TempDir())
	if err := va2.ReWrapAll(oldKey, newKey); err != nil {
		t.Fatalf("ReWrapAll on empty must be no-op, got %v", err)
	}
}

// TestDeriveAESKeyDeterministic：同密码同 salt → 同 key；不同 salt → 不同 key。
func TestDeriveAESKeyDeterministic(t *testing.T) {
	salt := []byte("abcdef0123456789")
	k1 := DeriveAESKey("pw", salt)
	k2 := DeriveAESKey("pw", salt)
	if string(k1) != string(k2) {
		t.Fatal("same password+salt must derive same key")
	}
	if len(k1) != 32 {
		t.Fatalf("key len = %d, want 32", len(k1))
	}
	other := DeriveAESKey("pw", []byte("9876543210fedcba"))
	if string(k1) == string(other) {
		t.Fatal("different salt must derive different key")
	}
}

// TestEmptyPassword：空密码不生成哈希；旧空哈希拒绝。
func TestEmptyPassword(t *testing.T) {
	if h := mustHashPassword(""); h != "" {
		t.Fatalf("empty password hash must be empty, got %q", h)
	}
	if valid, _ := VerifyPassword("whatever", ""); valid {
		t.Fatal("empty stored hash must never validate")
	}
}

// TestMigrationIntegration 端到端迁移：旧 SHA-256 哈希 + 旧密钥加密的 persona 密文，
// 经 /api/account/verify-password 后自动升级为 Argon2id PHC 并重封装密文。
func TestMigrationIntegration(t *testing.T) {
	a := testApp(t) // New() 已加载 kdfSalt，deriveKey 走 Argon2id
	pw := "skla123" // 仅迁移测试用
	oldKey := DeriveAESKeyLegacy(pw)
	newKey := DeriveAESKey(pw, kdfSalt)
	if string(oldKey) == string(newKey) {
		t.Fatal("precondition: old/new keys must differ")
	}

	// 种子旧态：64hex 旧密码哈希 + 用旧密钥加密的 persona 密文
	pt := []byte("小秘自定义性格明文-迁移测试")
	oldCt, err := encryptWithKey(oldKey, pt)
	if err != nil {
		t.Fatalf("encrypt old: %v", err)
	}
	a.mu.Lock()
	a.settings.UserPasswordHash = sha256Hex(pw)
	a.settings.PersonaCiphers = map[string]string{personaAide: oldCt}
	a.mu.Unlock()
	if !IsLegacyHash(a.settings.UserPasswordHash) {
		t.Fatal("precondition: seeded hash must be legacy")
	}

	// 错误密码不得升级
	w := request(a, "POST", "/api/account/verify-password", map[string]any{"password": "wrong-pw"})
	requireStatus(t, w, 401)
	if !IsLegacyHash(a.settings.UserPasswordHash) {
		t.Fatal("failed login must not upgrade hash")
	}

	// 正确旧密码：触发自动迁移
	w = request(a, "POST", "/api/account/verify-password", map[string]any{"password": pw})
	requireStatus(t, w, 200)
	var body map[string]any
	_ = json.Unmarshal([]byte(w.Body.String()), &body)
	if body["ok"] != true || body["upgraded"] != true {
		t.Fatalf("expected ok/upgraded, got %v", body)
	}

	// 哈希已升级为 PHC
	if !strings.HasPrefix(a.settings.UserPasswordHash, "$argon2id$") {
		t.Fatalf("hash not upgraded to PHC: %q", a.settings.UserPasswordHash)
	}
	// 旧密钥解不开、新密钥解得开且内容完好
	if _, err := decryptWithKey(oldKey, a.settings.PersonaCiphers[personaAide]); err == nil {
		t.Fatal("old key must not open re-wrapped cipher")
	}
	got, err := decryptWithKey(newKey, a.settings.PersonaCiphers[personaAide])
	if err != nil {
		t.Fatalf("new key must open re-wrapped: %v", err)
	}
	if string(got) != string(pt) {
		t.Fatalf("content mismatch: %q", got)
	}

	// 落盘 settings.json 里也是 PHC
	onDisk, err := os.ReadFile(filepath.Join(a.dataPath, "settings.json"))
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if !strings.Contains(string(onDisk), `"$argon2id$`) {
		t.Fatalf("on-disk settings.json lacks PHC hash: %s", onDisk)
	}
}
