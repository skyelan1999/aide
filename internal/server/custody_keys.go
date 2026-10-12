package server

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"
)

const maxCustodyKeyBytes = 16 * 1024

// listCustodyKeys returns names and timestamps only. Secret values have no read API.
func (a *App) listCustodyKeys(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	entries := make([]VaultMeta, 0)
	if a.vault != nil {
		for _, meta := range a.vault.List() {
			if meta.Type == VaultTypeCustodyKey {
				entries = append(entries, meta)
			}
		}
	}
	jsonOut(w, http.StatusOK, map[string]any{"entries": entries, "unlocked": a.vaultIsUnlocked()})
}

// putCustodyKey creates or replaces a local-only secret. The plaintext is encrypted
// immediately by SecretVault and is never written to settings or returned to clients.
func (a *App) putCustodyKey(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Secret string `json:"secret"`
	}
	if err := decode(w, r, &in); err != nil {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || !utf8.ValidString(in.Name) || utf8.RuneCountInString(in.Name) > 80 {
		fail(w, http.StatusBadRequest, errors.New("名称不能为空，且最多 80 个字符"))
		return
	}
	if len(in.Secret) == 0 || len(in.Secret) > maxCustodyKeyBytes {
		fail(w, http.StatusBadRequest, errors.New("密钥不能为空，且不能超过 16 KiB"))
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.vault == nil {
		fail(w, http.StatusServiceUnavailable, errors.New("本地加密保险库不可用"))
		return
	}
	if !a.vault.Unlocked() {
		fail(w, http.StatusLocked, errVaultLocked)
		return
	}
	id := strings.TrimSpace(in.ID)
	if id == "" {
		id = "custody:" + newID()
		if a.vault.Has(id) {
			fail(w, http.StatusConflict, errors.New("密钥标识冲突，请重试"))
			return
		}
	} else {
		old, ok := a.vault.entries[id]
		if !ok || old.Type != VaultTypeCustodyKey {
			fail(w, http.StatusNotFound, errors.New("代持密钥不存在"))
			return
		}
	}
	for _, meta := range a.vault.List() {
		if meta.Type == VaultTypeCustodyKey && meta.ID != id && strings.EqualFold(meta.Name, in.Name) {
			fail(w, http.StatusConflict, errors.New("已有同名代持密钥"))
			return
		}
	}

	previous, existed := a.vault.entries[id]
	plain := []byte(in.Secret)
	in.Secret = ""
	if err := a.vault.Put(id, VaultTypeCustodyKey, in.Name, plain, ""); err != nil {
		fail(w, http.StatusInternalServerError, errors.New("代持密钥加密失败"))
		return
	}
	if err := a.vault.Save(); err != nil {
		if existed {
			a.vault.entries[id] = previous
		} else {
			a.vault.Delete(id)
		}
		fail(w, http.StatusInternalServerError, errors.New("代持密钥未能安全保存"))
		return
	}
	a.vaultAudit("custody-key-saved:" + id)
	jsonOut(w, http.StatusOK, map[string]any{"ok": true, "id": id, "name": in.Name, "configured": true})
}

// deleteCustodyKey only deletes entries in the custody-key namespace.
func (a *App) deleteCustodyKey(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.vault == nil || !a.vault.Unlocked() {
		fail(w, http.StatusLocked, errVaultLocked)
		return
	}
	entry, ok := a.vault.entries[id]
	if !ok || entry.Type != VaultTypeCustodyKey {
		fail(w, http.StatusNotFound, errors.New("代持密钥不存在"))
		return
	}
	a.vault.Delete(id)
	if err := a.vault.Save(); err != nil {
		a.vault.entries[id] = entry
		fail(w, http.StatusInternalServerError, errors.New("代持密钥未能安全删除"))
		return
	}
	a.vaultAudit("custody-key-deleted:" + id)
	jsonOut(w, http.StatusOK, map[string]bool{"ok": true})
}
