package server

// ── 模型 API Key 加密保险库（从 settings.json 明文迁出）──────────────────────
// 威胁模型与目标：
//   - 模型 API Key 不再明文落 /data/config/settings.json；改为 AES-256-GCM 加密进
//     统一凭证保险库 /data/secrets/vault.enc（复用 #38 的 SecretVault 信封）。
//   - settings.json 只保留"是否已配置"的语义（由 vault 条目存在性推导），不含任何密钥材料。
//
// 主密钥分层（关键）：
//   - 用户已设账户密码（hasPassword=true）：主密钥 = Argon2id(账户密码, kdf-salt)，
//     与现有 SSH vault 一致；重启后保持锁定，须经 /api/unlock 或登录解锁才能取 key 调模型。
//   - 用户未设密码（hasPassword=false）：生成机器绑定随机主密钥（32B，0600 落盘
//     /data/secrets/master-key.bin），启动时自动加载并解锁 vault。明文不落 settings.json；
//     即便 settings.json 被拷走，无本机 master-key.bin 也无法解出 key。
//   - 后续用户首次设置密码时，把 vault 全部条目从机器密钥 re-wrap 到密码派生密钥。
//
// 旧明文迁移：启动时检测 settings.json / 环境变量里的明文 API Key，
// 能解锁则立即加密入 vault 并安全擦除落盘明文；不能解锁（有密码但尚未解锁）则暂存内存，
// 待下次解锁时再迁移。多次启动幂等：vault 已有条目即跳过。

import (
	"crypto/rand"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// 模型 API Key 在统一 vault 中的固定条目 ID / 类型（单实例：一个 OpenAI 兼容 provider 一把 key）。
const (
	VaultTypeModelAPIKey = "model-api-key"
	VaultIDModelAPIKey   = "model:api-key"
)

// errVaultLocked 未解锁且确有 key 时，模型调用应返回的明确错误（不静默失败）。
var errVaultLocked = errors.New("请先解锁以使用模型")

// machineMasterKeyFileName 无密码兜底的机器绑定随机主密钥（0600，与 vault.enc 同目录）。
const machineMasterKeyFileName = "master-key.bin"

// machineMasterKeyPath 返回 /data/secrets/master-key.bin。
func machineMasterKeyPath(data string) string {
	return filepath.Join(SecretsDir(data), machineMasterKeyFileName)
}

// ensureMachineMasterKey 确保存在一把 32B 机器绑定随机主密钥并返回。
// 已存在则原样读取（0600）；不存在则 crypto/rand 新生成并以 0600 落盘。
// 文件损坏（长度不对）时重新生成——无密码场景下 vault 要么为空、要么仅由本密钥保护，
// 重生成后旧密文不可解，属可接受的自愈（用户重新录入 key）。
func ensureMachineMasterKey(data string) ([]byte, error) {
	p := machineMasterKeyPath(data)
	if b, err := os.ReadFile(p); err == nil {
		if len(b) == 32 {
			return b, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	if err := os.WriteFile(p, b, 0600); err != nil {
		return nil, err
	}
	return b, nil
}

// unlockVaultAtStartup 启动时按密码分层决定 vault 解锁态：
//   - 有密码：保持锁定（等用户 /api/unlock 或登录），不读主密钥。
//   - 无密码：加载/生成机器绑定随机主密钥，自动解锁 vault。
// 必须在 vault.Load() 之后、处理请求之前调用。
func (a *App) unlockVaultAtStartup() error {
	if a.vault == nil {
		return nil
	}
	if a.settings.UserPasswordHash != "" {
		return nil // 有密码：保持锁定
	}
	key, err := ensureMachineMasterKey(a.dataPath)
	if err != nil {
		return err
	}
	a.vault.Unlock(key)
	return nil
}

// vaultIsUnlocked 返回 vault 主密钥是否已驻留内存（即"vaultUnlocked"状态）。
// 统一由 SecretVault 内部的主密钥句柄判定，不另设第二份布尔以免状态漂移。
func (a *App) vaultIsUnlocked() bool { return a.vault != nil && a.vault.Unlocked() }

// hasModelAPIKey 报告是否已配置模型 API Key（vault 中是否存在该条目；不解密）。
// 调用方持有 a.mu。
func (a *App) hasModelAPIKey() bool { return a.vault != nil && a.vault.Has(VaultIDModelAPIKey) }

// modelAPIKeyLocked 从 vault 取出解密后的模型 API Key。
//   - 未配置 key（本地模型）：返回 ("", nil)，按原逻辑不带 Authorization。
//   - 已配置但 vault 未解锁：返回 errVaultLocked，供模型调用给出明确提示。
// 调用方持有 a.mu。用完不应长期驻留；这里返回字符串（value copy）由调用方随 cfg 传用。
func (a *App) modelAPIKeyLocked() (string, error) {
	if !a.hasModelAPIKey() {
		return "", nil
	}
	if !a.vaultIsUnlocked() {
		return "", errVaultLocked
	}
	pt, err := a.vault.Get(VaultIDModelAPIKey)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}

// stageLegacyAPIKeyMigration 启动时检测旧明文 API Key（settings.json / AI_API_KEY 环境变量）。
// 有明文则暂存内存（pendingLegacyAPIKey），并立即把内存中的 settings.APIKey 清空，
// 防止后续任何 atomicJSON 把明文重新写回磁盘。是否真正加密入 vault 由解锁后的迁移决定。
// 幂等：明文为空则什么都不做。调用方持有 a.mu（New 启动阶段单 goroutine）。
func (a *App) stageLegacyAPIKeyMigration() {
	if a.settings.APIKey == "" {
		return
	}
	a.pendingLegacyAPIKey = a.settings.APIKey
	a.settings.APIKey = ""
}

// migratePendingLegacyAPIKeyLocked 把暂存的旧明文 API Key 加密入 vault，并安全擦除 settings.json 明文。
// vault 未解锁时跳过（保留暂存，等下次解锁）。调用方已持有 a.mu（或处于单 goroutine 启动阶段）。
func (a *App) migratePendingLegacyAPIKeyLocked() {
	if a.vault == nil || !a.vault.Unlocked() {
		return
	}
	if a.pendingLegacyAPIKey == "" {
		return
	}
	plain := strings.TrimSpace(a.pendingLegacyAPIKey)
	if plain == "" {
		a.pendingLegacyAPIKey = ""
		return
	}
	if err := a.vault.Put(VaultIDModelAPIKey, VaultTypeModelAPIKey, "模型 API Key", []byte(plain), ""); err != nil {
		return // 保留暂存，下次解锁重试
	}
	if err := a.vault.Save(); err != nil {
		return
	}
	a.pendingLegacyAPIKey = ""
	// 安全擦除落盘明文：先把现有 settings.json 原地覆写为随机字节，再写回干净（无明文）的设置。
	shredFile(SettingsPath(a.dataPath))
	_ = atomicJSON(SettingsPath(a.dataPath), a.settings)
}

// storeModelAPIKeyPlaintextLocked 把一个用户新录入的明文 key 收敛进 vault。
// vault 已解锁：直接加密入库；未解锁：暂存待解锁后迁移。无论哪条路径，settings.json 都不留明文。
// 调用方持有 a.mu。
func (a *App) storeModelAPIKeyPlaintextLocked(plain string) {
	plain = strings.TrimSpace(plain)
	if plain == "" {
		return
	}
	if a.vault != nil && a.vault.Unlocked() {
		if err := a.vault.Put(VaultIDModelAPIKey, VaultTypeModelAPIKey, "模型 API Key", []byte(plain), ""); err == nil {
			_ = a.vault.Save()
			return
		}
	}
	a.pendingLegacyAPIKey = plain
}

// clearModelAPIKeyLocked 删除 vault 中的模型 key（用户显式 clearKey）。调用方持有 a.mu。
func (a *App) clearModelAPIKeyLocked() {
	a.pendingLegacyAPIKey = ""
	if a.vault == nil {
		return
	}
	a.vault.Delete(VaultIDModelAPIKey)
	_ = a.vault.Save()
}

// shredFile 尽力原地覆写文件为等长随机字节（best-effort 擦除）。
// SSD 磨损均衡下覆写不保证物理销毁，但可阻止普通取证/备份读到上一版明文。
func shredFile(path string) {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return
	}
	pad := make([]byte, st.Size())
	if _, err := rand.Read(pad); err != nil {
		return
	}
	_ = os.WriteFile(path, pad, 0600)
}

// modelConfigItem 是 /api/config 返回的单个模型条目：在 ModelRef 基础上追加 hasApiKey 标记，
// 绝不回显明文 key。单 provider 场景下所有模型共享同一把 key，故 hasApiKey 取值一致。
type modelConfigItem struct {
	ID            string `json:"id"`
	Name          string `json:"name,omitempty"`
	ContextWindow int    `json:"contextWindow,omitempty"`
	HasAPIKey     bool   `json:"hasApiKey"`
	Vision        bool   `json:"vision"` // #63 扩展：该模型是否支持图片输入
}

// modelConfigOut 把 a.settings.Models 转成带 hasApiKey 标记的输出列表。调用方持有 a.mu。
func (a *App) modelConfigOut() []modelConfigItem {
	out := make([]modelConfigItem, 0, len(a.settings.Models))
	has := a.hasModelAPIKey()
	for _, m := range a.settings.Models {
		out = append(out, modelConfigItem{
			ID: m.ID, Name: m.Name, ContextWindow: m.ContextWindow, HasAPIKey: has, Vision: m.Vision,
		})
	}
	return out
}

// ── HTTP：/api/unlock ────────────────────────────────────────────────────────

// unlockApp POST /api/unlock：用账户密码解锁 vault（重启后/锁屏后恢复模型取 key 能力）。
// 无密码用户 vault 已由机器密钥自动解锁，直接返回。解锁成功后顺带迁移暂存的旧明文 key。
func (a *App) unlockApp(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password string `json:"password"`
	}
	if err := decode(w, r, &in); err != nil {
		return
	}
	a.mu.Lock()
	hash := a.settings.UserPasswordHash
	already := a.vault != nil && a.vault.Unlocked()
	a.mu.Unlock()

	if hash == "" {
		// 未设密码：vault 本应在启动时由机器密钥自动解锁；这里兜底一次。
		if !already {
			if err := a.unlockVaultAtStartup(); err != nil {
				fail(w, 500, err)
				return
			}
		}
		jsonOut(w, 200, map[string]any{"ok": true, "unlocked": true, "passwordless": true})
		return
	}
	ok, _ := VerifyPassword(in.Password, hash)
	if !ok {
		fail(w, 401, errors.New("账户密码错误"))
		return
	}
	a.mu.Lock()
	a.unlockVault(in.Password) // 派生主密钥解锁 + 迁移旧 SSH 凭据
	a.migratePendingLegacyAPIKeyLocked()
	a.mu.Unlock()
	jsonOut(w, 200, map[string]any{"ok": true, "unlocked": true})
}
