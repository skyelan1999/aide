package server

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// ── App 侧 vault 生命周期辅助 ──────────────────────────────────────────────

// unlockVault 用账户密码派生主密钥并解锁保险库，随后迁移旧明文凭证。
// 必须在持有账户密码（已校验）的上下文中调用。
func (a *App) unlockVault(password string) {
	if a.vault == nil || password == "" {
		return
	}
	a.vault.Unlock(deriveKey(password))
	a.migrateLegacyWorkspaceSecrets()
}

// vaultAudit 追加一条凭证审计（data/vault-audit.jsonl，0600）。
// 只记动作/时间/类型，绝不记录凭据正文、口令或私钥。
func (a *App) vaultAudit(action string) {
	entry := map[string]any{
		"time":   time.Now().UTC().Format(time.RFC3339Nano),
		"action": action,
	}
	b, err := json.Marshal(entry)
	if err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(a.dataPath, "vault-audit.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		log.Printf("vault-audit 写入失败: %v", err)
		return
	}
	defer f.Close()
	if _, err := f.Write(append(b, '\n')); err != nil {
		log.Printf("vault-audit 写入失败: %v", err)
	}
}

// ── HTTP：保险库列表 / 删除 / 解锁 ──────────────────────────────────────────

// listWorkspaceSecrets 返回保险库全部条目的元数据（不含密文/明文）。
func (a *App) listWorkspaceSecrets(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.vault == nil {
		jsonOut(w, 200, map[string]any{"entries": []VaultMeta{}, "unlocked": false})
		return
	}
	jsonOut(w, 200, map[string]any{"entries": a.vault.List(), "unlocked": a.vault.Unlocked()})
}

// deleteWorkspaceSecret 删除指定条目（路径参数 id）。不允许删除正在被引用的固定条目之外的越权操作；
// 这里仅按 id 删除，调用方（前端）已限定可选范围。
func (a *App) deleteWorkspaceSecret(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		fail(w, 400, errors.New("缺少条目 id"))
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.vault == nil {
		fail(w, 500, errors.New("保险库未初始化"))
		return
	}
	a.vault.Delete(id)
	if err := a.vault.Save(); err != nil {
		fail(w, 500, err)
		return
	}
	a.vaultAudit("workspace-secret-deleted:" + id)
	jsonOut(w, 200, map[string]bool{"ok": true})
}

// unlockWorkspaceVault 用账户密码解锁保险库（重启后/锁屏后恢复 SSH 运行时解密能力）。
func (a *App) unlockWorkspaceVault(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password string `json:"password"`
	}
	if err := decode(w, r, &in); err != nil {
		return
	}
	a.mu.Lock()
	hash := a.settings.UserPasswordHash
	a.mu.Unlock()
	if hash == "" {
		fail(w, 401, errors.New("未设置登录密码"))
		return
	}
	ok, _ := VerifyPassword(in.Password, hash)
	if !ok {
		fail(w, 401, errors.New("账户密码错误"))
		return
	}
	a.mu.Lock()
	a.unlockVault(in.Password)
	a.mu.Unlock()
	jsonOut(w, 200, map[string]any{"ok": true, "unlocked": true})
}
