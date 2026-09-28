package server

// ── 每来源凭据并入加密 vault（finding backend-integrations-003 / P2 #1）─────────
// 背景：SFTP/SMB/FTPS 等外部来源的密码/私钥原先以明文 JSON 落在
// /data/secrets/sources-secrets.json（App.sourceSecrets）。本模块把每来源凭据收敛进
// 统一的 AES-256-GCM SecretVault，同时保留「旧明文」作为 vault 不可用时的安全回退：
//
//   - 统一读取：sourceCredentialLocked 优先解 vault；vault 未解锁或无条目时回退内存明文。
//     因此迁移期间、vault 锁定、密封失败都不会让来源配置失效或丢凭据。
//   - 自动迁移：migrateLegacySourceSecretsLocked 在 vault 解锁后被调用，把内存明文逐条
//     密封进 vault；全部成功且 Save 落盘后，才清掉内存明文并重写无明文的 sources-secrets.json。
//   - 幂等：vault 已有对应条目即跳过；密封/Save 失败时保留内存明文，下次解锁重试，
//     错误经 log 记录（不吞掉）。
//   - 新增/更新/删除：storeSourceCredentialLocked 同步写 vault 与回退明文；删除来源时
//     由 updateSources 调用方 sweep 掉不再存活条目的密文与明文。

import (
	"encoding/json"
	"log"
	"strings"
)

// VaultTypeSourceSecret 每来源凭据在统一 vault 中的类型标签。
const VaultTypeSourceSecret = "source-secret"

// sourceVaultID 由来源 id 派生 vault 条目 ID。一个来源的 password+key 合并为一条 JSON 密文。
func sourceVaultID(sourceID string) string { return "source:" + sourceID }

// sourceCredPayload 单来源凭据的明文载荷（密封进 vault 前的 JSON 结构）。
// 与 sourcesSecrets.Secrets[id] 同构，便于迁移时直接序列化。
type sourceCredPayload struct {
	Password string `json:"password,omitempty"`
	Key      string `json:"key,omitempty"`
}

// sourceCredentialLocked 解析某来源的 password+key。优先取加密 vault（已解锁且有条目）；
// vault 未解锁、无条目或载荷损坏时，回退到内存中的旧明文 map，保证来源在迁移/锁定期间可用。
// 调用方持有 a.mu。
func (a *App) sourceCredentialLocked(id string) (password, key string) {
	if a.vault != nil && a.vault.Unlocked() && a.vault.Has(sourceVaultID(id)) {
		if pt, err := a.vault.Get(sourceVaultID(id)); err == nil {
			var p sourceCredPayload
			if json.Unmarshal(pt, &p) == nil {
				return p.Password, p.Key
			}
			// 载荷损坏：不吞错，落日志后回退明文，避免单个坏条目永久阻断来源。
			log.Printf("来源 %s 的 vault 凭据载荷损坏，回退旧明文", id)
		}
	}
	sec := a.sourceSecrets.Secrets[id]
	return sec.Password, sec.Key
}

// sourceHasSecretLocked 报告某来源是否持有任一凭据（password 或 key）。调用方持有 a.mu。
func (a *App) sourceHasSecretLocked(id string) bool {
	pw, key := a.sourceCredentialLocked(id)
	return pw != "" || key != ""
}

// storeSourceCredentialLocked 把用户新录入/清空的来源凭据收敛进 vault 与回退明文。
//  - password/key 均为空：删除 vault 条目与内存明文（用户显式 clear）。
//  - vault 已解锁且密封成功：密文入库，内存明文随之清除（避免再被落盘为明文）。
//  - vault 未解锁或密封失败：保留内存明文作为回退，由后续 migrateLegacySourceSecretsLocked 重试。
// 调用方持有 a.mu。
func (a *App) storeSourceCredentialLocked(id, password, key string) {
	vaultID := sourceVaultID(id)
	// 始终维护回退明文 map；vault 路径成功后再清掉对应项。
	if password != "" || key != "" {
		a.sourceSecrets.Secrets[id] = sourceSecretEntry{Password: password, Key: key}
	} else {
		delete(a.sourceSecrets.Secrets, id)
	}

	if a.vault == nil {
		return
	}
	if password == "" && key == "" {
		a.vault.Delete(vaultID)
		_ = a.vault.Save()
		return
	}
	if !a.vault.Unlocked() {
		return // 未解锁：保留内存明文，待解锁后迁移。
	}
	payload, err := json.Marshal(sourceCredPayload{Password: password, Key: key})
	if err != nil {
		log.Printf("来源 %s 凭据序列化失败（保留明文备用）: %v", id, err)
		return
	}
	if err := a.vault.Put(vaultID, VaultTypeSourceSecret, "来源凭据: "+id, payload, ""); err != nil {
		log.Printf("来源 %s 凭据密封入 vault 失败（保留明文备用，稍后重试）: %v", id, err)
		return
	}
	if err := a.vault.Save(); err != nil {
		// 密封已在内存成功、但落盘失败：保留内存明文以便本次会话仍可回退；
		// 下次解锁后 migrateLegacySourceSecretsLocked 会因 vault 尚无该条目而重封。
		log.Printf("来源 %s 凭据写入 vault 落盘失败（保留明文备用）: %v", id, err)
		return
	}
	// 密封+落盘均成功：清除内存明文，避免被 saveSourcesSecrets 再次写盘为明文。
	delete(a.sourceSecrets.Secrets, id)
}

// migrateLegacySourceSecretsLocked 把内存中的旧明文来源凭据逐条密封进 vault。
// 幂等：vault 已有条目即跳过。任何一条密封或最终 Save 失败，都保留内存明文（并落盘回退文件），
// 待下次解锁重试；错误经 log 记录。调用方持有 a.mu（或处于单 goroutine 启动阶段）。
func (a *App) migrateLegacySourceSecretsLocked() {
	if a.vault == nil || !a.vault.Unlocked() {
		return // vault 不可用：保留明文，下次解锁重试。
	}
	migrated := 0
	for id, sec := range a.sourceSecrets.Secrets {
		if sec.Password == "" && sec.Key == "" {
			continue
		}
		vaultID := sourceVaultID(id)
		if a.vault.Has(vaultID) {
			continue // 已在 vault：跳过，幂等。
		}
		payload, err := json.Marshal(sourceCredPayload{Password: sec.Password, Key: sec.Key})
		if err != nil {
			log.Printf("迁移来源 %s 凭据序列化失败（保留明文）: %v", id, err)
			continue
		}
		if err := a.vault.Put(vaultID, VaultTypeSourceSecret, "来源凭据: "+id, payload, ""); err != nil {
			log.Printf("迁移来源 %s 凭据密封入 vault 失败（保留明文，稍后重试）: %v", id, err)
			continue
		}
		migrated++
	}
	if migrated == 0 {
		return
	}
	// 先把 vault 密文落盘；失败则保留内存明文，下次重试。
	if err := a.vault.Save(); err != nil {
		log.Printf("迁移来源凭据后 vault 落盘失败（保留明文，稍后重试）: %v", err)
		return
	}
	// 全部落盘成功：清掉内存明文并重写无明文的回退文件（保留文件本身，结构为空）。
	for id := range a.sourceSecrets.Secrets {
		delete(a.sourceSecrets.Secrets, id)
	}
	if err := a.saveSourcesSecrets(); err != nil {
		log.Printf("迁移来源凭据后清理 sources-secrets.json 失败（不影响 vault 密文）: %v", err)
	}
	a.vaultAudit("source-secrets-migrated")
}

// sweepSourceCredentialsLocked 删除已不在注册表中的来源凭据（密文+明文）。
// 调用方持有 a.mu，且 a.sourceRegistry 已更新为最新存活集合。
func (a *App) sweepSourceCredentialsLocked(alive map[string]bool) {
	for id := range a.sourceSecrets.Secrets {
		if !alive[id] {
			delete(a.sourceSecrets.Secrets, id)
		}
	}
	if a.vault == nil {
		return
	}
	for _, m := range a.vault.List() {
		if strings.HasPrefix(m.ID, "source:") && !alive[strings.TrimPrefix(m.ID, "source:")] {
			a.vault.Delete(m.ID)
		}
	}
	_ = a.vault.Save()
}
