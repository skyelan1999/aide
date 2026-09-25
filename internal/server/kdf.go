package server

// ── 密码哈希与密钥派生（KDF）─────────────────────────────────────────────────
// 账户密码不再用裸 SHA-256 存储/派生，改为 Argon2id（PHC 格式）：
//   - 存储：HashPassword 输出 $argon2id$v=19$m=65536,t=3,p=4$<salt>$<hash>
//   - 数据密钥：DeriveAESKey(password, 本机固定 kdf-salt.bin) → 32B AES-256 密钥
// 旧版（v0.1.10 及以前）使用裸 SHA-256：密码哈希为 64 位 hex，AES 密钥即 SHA-256(password)。
// 本模块同时保留旧派生（DeriveAESKeyLegacy）与旧哈希识别（IsLegacyHash），
// 用于登录时平滑迁移：识别旧 64hex → 旧算法校验通过 → 立即升级为 Argon2id 并重加密已有密文。
//
// 注意：高熵随机 token（access-token / debug-token）仍用裸 SHA-256 哈希存储，
// 它们本身已不可暴力枚举，不需要慢 KDF；见 sha256Hex。

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id 参数（OWASP 推荐基线：64MB 内存、3 次迭代、4 线程）。
const (
	argon2Time    = 3
	argon2Memory  = 64 * 1024 // 64MB（KiB）
	argon2Threads = 4
	argon2KeyLen  = 32
	argon2SaltLen = 16
)

// kdfSalt 为本机固定的 KDF salt（data/kdf-salt.bin，权限 0600），New() 启动时加载。
// 它与密码无关、只做域分离，使派生密钥不被跨站彩虹表命中；一旦生成必须永久保留。
// 为空时（如未调用 ensureKdfSalt 的单元测试）派生退化为 SHA-256，保证既有测试确定性。
var kdfSalt []byte

// ensureKdfSalt 确保 data/kdf-salt.bin 存在并加载到包级 kdfSalt。
// 首次生成 16B crypto/rand，权限 0600；已存在则原样读取（禁止重新生成，否则既有密文全部失效）。
func ensureKdfSalt(dataDir string) error {
	p := filepath.Join(dataDir, "kdf-salt.bin")
	if b, err := os.ReadFile(p); err == nil {
		if len(b) >= argon2SaltLen {
			kdfSalt = b[:argon2SaltLen]
			return nil
		}
		// 文件存在但过短：视为损坏，下面重新生成。
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	b := make([]byte, argon2SaltLen)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	if err := os.WriteFile(p, b, 0600); err != nil {
		return err
	}
	kdfSalt = b
	return nil
}

// DeriveAESKey 用 Argon2id + 固定 KDF salt 派生 32B AES-256 密钥。
// fixedSalt 通常来自 data/kdf-salt.bin（由 ensureKdfSalt 加载）。
func DeriveAESKey(password string, fixedSalt []byte) []byte {
	return argon2.IDKey([]byte(password), fixedSalt, argon2Time, argon2Memory, argon2Threads, argon2KeyLen)
}

// DeriveAESKeyLegacy 旧 SHA-256 派生（v0.1.10 及以前），仅用于迁移期解密旧密文。
func DeriveAESKeyLegacy(password string) []byte {
	sum := sha256.Sum256([]byte(password))
	return sum[:]
}

// HashPassword 返回 PHC 格式字符串：$argon2id$v=19$m=65536,t=3,p=4$<b64salt>$<b64hash>
func HashPassword(password string) (string, error) {
	salt := make([]byte, argon2SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argon2Time, argon2Memory, argon2Threads, argon2KeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argon2Memory, argon2Time, argon2Threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// IsLegacyHash 判断是否为旧 64 位裸 hex SHA-256（无 PHC 前缀）。
func IsLegacyHash(hash string) bool {
	h := strings.TrimSpace(hash)
	if h == "" || strings.HasPrefix(h, "$argon2") {
		return false
	}
	if len(h) != 64 {
		return false
	}
	for i := 0; i < len(h); i++ {
		c := h[i]
		ok := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
		if !ok {
			return false
		}
	}
	return true
}

// VerifyPassword 校验密码，支持 PHC(Argon2id) 与旧 64hex(SHA-256) 两种格式。
// 返回 (valid, needsUpgrade)；needsUpgrade=true 表示命中旧 SHA-256 格式，调用方应升级哈希并重加密密文。
func VerifyPassword(password, hash string) (bool, bool) {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return false, false
	}
	if strings.HasPrefix(hash, "$argon2id$") {
		return verifyArgon2id(password, hash), false
	}
	if IsLegacyHash(hash) {
		computed := sha256Hex(password)
		return subtle.ConstantTimeCompare([]byte(computed), []byte(hash)) == 1, true
	}
	return false, false
}

// verifyArgon2id 解析 PHC 串并用相同参数重算 Argon2id，常量时间比较。
func verifyArgon2id(password, phc string) bool {
	parts := strings.Split(phc, "$")
	// 期望：["", "argon2id", "v=19", "m=...,t=...,p=...", salt, hash]
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var (
		time uint32
		mem  uint32
		p    uint8
		ver  int
	)
	if _, err := fmt.Sscanf(parts[2], "v=%d", &ver); err != nil {
		return false
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &time, &p); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, time, mem, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// mustHashPassword 生成 Argon2id PHC 哈希，空密码返回空串（由调用方校验非空）。
func mustHashPassword(password string) string {
	if password == "" {
		return ""
	}
	h, err := HashPassword(password)
	if err != nil {
		return ""
	}
	return h
}
