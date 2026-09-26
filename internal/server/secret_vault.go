package server

// ── 统一加密凭证保险库（secret vault）─────────────────────────────────────────
// 工作空间 SSH（#38）与未来 comm-ssh（#37）共用的唯一凭证存储：
//   - 静态加密：AES-256-GCM，每条目独立 12B nonce（crypto/rand）。
//   - 主密钥：Argon2id 从账户密码派生（复用 kdf.go 的 DeriveAESKey + 本机固定 kdf-salt），
//     运行时仅驻留内存，不落盘；改密码时 ReWrap(oldKey, newKey) 重加密全部条目。
//   - 文件：/data/secrets/vault.enc（目录 0700、文件 0600），JSON 信封，仅含密文/nonce/元数据。
//   - 内存中明文最小化：Get 返回明文后调用方应尽快使用并清零；List 永不返回密文或明文。
//
// 威胁模型：/data 卷被只读取证时，凭据不可读（无账户密码即无主密钥）；
// API 仍由 access-token 鉴权。解锁（持有主密钥）仅发生在用户会话内存中。

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const (
	vaultDirName    = "secrets"
	vaultFileName    = "vault.enc"
	vaultEnvelopeV1 = 1
	vaultDirPerm    = 0o700
	vaultFilePerm   = 0o600
)

// 凭证条目类型。统一枚举，#37 comm-ssh 复用同一 vault 时按类型区分。
const (
	VaultTypeSSHPassword   = "ssh-password"   // SSH 登录密码
	VaultTypeSSHKey        = "ssh-key"        // SSH 私钥（导入的加密副本）
	VaultTypeSSHPassphrase = "ssh-passphrase" // 私钥口令（保护私钥本身）
)

// 工作空间 SSH 凭据的固定条目 ID（单实例，与现有 workspace 语义一致）。
const (
	VaultIDWSPassword   = "ws:ssh-password"
	VaultIDWSKey        = "ws:ssh-key"
	VaultIDWSPassphrase = "ws:ssh-passphrase"
)

// VaultEntry 一条加密凭证。Ciphertext/Nonce 以 base64 落 JSON；内存中保留密文形式，
// 明文只在 Get 时短暂存在。
type VaultEntry struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Name        string `json:"name,omitempty"`
	Ciphertext  []byte `json:"ciphertext_b64"`
	Nonce       []byte `json:"nonce_b64"`
	Fingerprint string `json:"fingerprint,omitempty"` // 公钥 SHA256 指纹（元数据，可展示）
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

// VaultMeta 列表项元数据：绝不包含密文或明文。
type VaultMeta struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Name        string `json:"name,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

type vaultEnvelope struct {
	Version int          `json:"version"`
	Entries []VaultEntry `json:"entries"`
}

// SecretVault 内存中的保险库句柄。key 为当前主密钥（nil = 已锁定，不可 Put/Get）。
type SecretVault struct {
	path    string
	key     []byte
	entries map[string]VaultEntry
}

// VaultDir 返回保险库目录（/data/secrets），并确保其存在（0700）。
func VaultDir(dataDir string) string { return filepath.Join(dataDir, vaultDirName) }

// newSecretVault 创建句柄（不解锁、不读盘）。Load 之后 entries 可用。
func newSecretVault(dataDir string) (*SecretVault, error) {
	dir := VaultDir(dataDir)
	if err := os.MkdirAll(dir, vaultDirPerm); err != nil {
		return nil, fmt.Errorf("创建保险库目录失败: %w", err)
	}
	return &SecretVault{
		path:    filepath.Join(dir, vaultFileName),
		entries: map[string]VaultEntry{},
	}, nil
}

// Unlocked 报告主密钥是否驻留内存。
func (v *SecretVault) Unlocked() bool { return len(v.key) > 0 }

// Path 返回保险库信封文件绝对路径（导出/审计用）。
func (v *SecretVault) Path() string { return v.path }

// Unlock 用已派生的主密钥解锁（持有）。nil/空密钥视为锁定。
// 调用方负责先用账户密码验证，再经 deriveKey 派生。
func (v *SecretVault) Unlock(key []byte) {
	v.key = append(v.key[:0:0], key...)
}

// Lock 清除内存中的主密钥与所有密文句柄（不删盘上数据）。
func (v *SecretVault) Lock() {
	zeroBytes(v.key)
	v.key = nil
}

// Load 从加密信封读入条目（密文形式）。文件不存在视为空保险库。
func (v *SecretVault) Load() error {
	v.entries = map[string]VaultEntry{}
	b, err := os.ReadFile(v.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var env vaultEnvelope
	if err := json.Unmarshal(b, &env); err != nil {
		return fmt.Errorf("解析保险库信封失败: %w", err)
	}
	for _, e := range env.Entries {
		ee := e
		v.entries[ee.ID] = ee
	}
	return nil
}

// Save 把当前条目（密文形式）原子写回，权限 0600。锁定状态下仍可保存（保留密文不变）。
func (v *SecretVault) Save() error {
	env := vaultEnvelope{Version: vaultEnvelopeV1, Entries: make([]VaultEntry, 0, len(v.entries))}
	for _, e := range v.entries {
		env.Entries = append(env.Entries, e)
	}
	b, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return err
	}
	// 原子写：先写临时文件再 rename，0600。
	dir := filepath.Dir(v.path)
	tmp, err := os.CreateTemp(dir, ".vault-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, vaultFilePerm); err != nil {
		return err
	}
	return os.Rename(tmpName, v.path)
}

// Put 加密并存入/更新一条凭证。要求已解锁。fingerprint 仅作元数据展示。
func (v *SecretVault) Put(id, secretType, name string, plaintext []byte, fingerprint string) error {
	if !v.Unlocked() {
		return errors.New("保险库未解锁")
	}
	ct, nonce, err := v.seal(plaintext)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	existing, ok := v.entries[id]
	e := VaultEntry{
		ID:          id,
		Type:        secretType,
		Name:        name,
		Ciphertext:  ct,
		Nonce:       nonce,
		Fingerprint: fingerprint,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if ok {
		e.CreatedAt = existing.CreatedAt // 保留原始创建时间
	}
	v.entries[id] = e
	zeroBytes(plaintext)
	return nil
}

// Get 解密返回一条凭证明文。调用方用完应 zeroBytes 清零。
func (v *SecretVault) Get(id string) ([]byte, error) {
	if !v.Unlocked() {
		return nil, errors.New("保险库未解锁")
	}
	e, ok := v.entries[id]
	if !ok {
		return nil, os.ErrNotExist
	}
	return v.open(e.Ciphertext, e.Nonce)
}

// Delete 删除一条凭证。不存在视为成功。
func (v *SecretVault) Delete(id string) { delete(v.entries, id) }

// Has 报告是否存在某条目。
func (v *SecretVault) Has(id string) bool { _, ok := v.entries[id]; return ok }

// IsEnvelope 报告字节是否为可识别的加密信封（导出/导入时区分新信封 vs 旧明文格式）。
func IsEnvelope(b []byte) bool {
	var env vaultEnvelope
	if err := json.Unmarshal(b, &env); err != nil {
		return false
	}
	return env.Version > 0 || env.Entries != nil
}

// ImportEnvelope 合并一个导出信封里的条目进当前保险库（不覆盖已存在的同 ID 条目）。
// 条目保持其原有密文：同机恢复（同 kdf-salt、已用同密码解锁）时可被当前主密钥直接解开；
// 跨机恢复因 kdf-salt 不同而不可解，属预期——用户重新录入凭据即可。
// 返回新导入的条目数。
func (v *SecretVault) ImportEnvelope(b []byte) (int, error) {
	var env vaultEnvelope
	if err := json.Unmarshal(b, &env); err != nil {
		return 0, err
	}
	imported := 0
	for _, e := range env.Entries {
		if e.ID == "" {
			continue
		}
		if _, exists := v.entries[e.ID]; exists {
			continue // 不覆盖当前可用凭据
		}
		v.entries[e.ID] = e
		imported++
	}
	return imported, nil
}

// List 返回全部条目的元数据（不含密文/明文）。
func (v *SecretVault) List() []VaultMeta {
	out := make([]VaultMeta, 0, len(v.entries))
	for _, e := range v.entries {
		out = append(out, VaultMeta{
			ID: e.ID, Type: e.Type, Name: e.Name,
			Fingerprint: e.Fingerprint, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
		})
	}
	return out
}

// ReWrap 用 oldKey 解密全部条目、再用 newKey 重加密，并切换到 newKey。
// 用于改密码：旧密钥不可再解，新密钥可用。任一旧条目解密失败则保留原状态返回错误。
func (v *SecretVault) ReWrap(oldKey, newKey []byte) error {
	if len(newKey) == 0 {
		return errors.New("新主密钥为空")
	}
	rebuilt := map[string]VaultEntry{}
	for id, e := range v.entries {
		pt, err := openWithKey(oldKey, e.Ciphertext, e.Nonce)
		if err != nil {
			return fmt.Errorf("条目 %s 用旧密钥解密失败: %w", id, err)
		}
		ct, nonce, err := sealWithKey(newKey, pt)
		zeroBytes(pt)
		if err != nil {
			return err
		}
		e.Ciphertext, e.Nonce, e.UpdatedAt = ct, nonce, time.Now().UTC().Format(time.RFC3339Nano)
		rebuilt[id] = e
	}
	v.entries = rebuilt
	v.key = append(v.key[:0:0], newKey...)
	return v.Save()
}

// seal 用当前主密钥加密：GCM, 12B nonce。
func (v *SecretVault) seal(plaintext []byte) (ciphertext, nonce []byte, err error) {
	return sealWithKey(v.key, plaintext)
}

// open 用当前主密钥解密。
func (v *SecretVault) open(ciphertext, nonce []byte) ([]byte, error) {
	return openWithKey(v.key, ciphertext, nonce)
}

func gcmFor(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func sealWithKey(key, plaintext []byte) (ciphertext, nonce []byte, err error) {
	g, err := gcmFor(key)
	if err != nil {
		return nil, nil, err
	}
	nonce = make([]byte, g.NonceSize()) // 12B
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, err
	}
	ct := g.Seal(nil, nonce, plaintext, nil)
	return ct, nonce, nil
}

func openWithKey(key, ciphertext, nonce []byte) ([]byte, error) {
	g, err := gcmFor(key)
	if err != nil {
		return nil, err
	}
	if len(nonce) != g.NonceSize() {
		return nil, errors.New("nonce 长度非法")
	}
	return g.Open(nil, nonce, ciphertext, nil)
}

// zeroBytes 尽力清零一个 byte 切片（明文密钥/口令用完即清）。
func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
