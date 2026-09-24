package server

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
)

// deriveKey 从用户密码派生 32 字节 AES-256 密钥
func deriveKey(password string) []byte {
	sum := sha256.Sum256([]byte(password))
	return sum[:]
}

// encryptPersona AES-256-GCM 加密明文，返回 base64(nonce+ciphertext)
func encryptPersona(plaintext, password string) (string, error) {
	if password == "" {
		return "", errors.New("密码不能为空")
	}
	key := deriveKey(password)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ct), nil
}

// decryptPersona 解密
func decryptPersona(b64, password string) (string, error) {
	if b64 == "" {
		return "", nil
	}
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", err
	}
	key := deriveKey(password)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	ns := gcm.NonceSize()
	if len(data) < ns {
		return "", errors.New("数据损坏")
	}
	nonce, ct := data[:ns], data[ns:]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", errors.New("密码错误或数据损坏")
	}
	return string(pt), nil
}

// defaultPersona 默认性格
const defaultPersona = "你是 aide，一个专业、简洁、直接的 AI 助手。回答优先给结论，再给理由。不要废话。"

// personaGet 返回性格状态（不返回明文）
func (a *App) personaGet(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"enabled":   a.settings.PersonaEnabled,
		"hasCipher": a.settings.PersonaCipher != "",
		"unlocked":  a.personaKey != "",
	})
}

// personaUnlock 用密码解锁性格
func (a *App) personaUnlock(w http.ResponseWriter, r *http.Request) {
	var in struct{ Password string `json:"password"` }
	json.NewDecoder(r.Body).Decode(&in)
	if a.settings.PersonaCipher == "" {
		// 首次设置密码
		if in.Password == "" {
			http.Error(w, "请设置密码", 400)
			return
		}
		a.personaKey = in.Password
		ct, _ := encryptPersona(defaultPersona, in.Password)
		a.settings.PersonaCipher = ct
		a.mu.Lock()
		atomicJSON(filepath.Join(a.dataPath, "settings.json"), a.settings)
		a.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "firstSetup": true})
		return
	}
	// 验证密码
	if _, err := decryptPersona(a.settings.PersonaCipher, in.Password); err != nil {
		http.Error(w, "密码错误", 401)
		return
	}
	a.personaKey = in.Password
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

// personaSave 保存性格内容（用当前内存密钥加密）
func (a *App) personaSave(w http.ResponseWriter, r *http.Request) {
	if a.personaKey == "" {
		http.Error(w, "请先解锁性格", 401)
		return
	}
	var in struct{ Content string `json:"content"` }
	json.NewDecoder(r.Body).Decode(&in)
	ct, err := encryptPersona(in.Content, a.personaKey)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	a.settings.PersonaCipher = ct
	a.mu.Lock()
	atomicJSON(filepath.Join(a.dataPath, "settings.json"), a.settings)
	a.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

// personaReset 重置为默认性格（需要密码）
func (a *App) personaReset(w http.ResponseWriter, r *http.Request) {
	var in struct{ Password string `json:"password"` }
	json.NewDecoder(r.Body).Decode(&in)
	if _, err := decryptPersona(a.settings.PersonaCipher, in.Password); err != nil {
		http.Error(w, "密码错误，无法重置", 401)
		return
	}
	ct, _ := encryptPersona(defaultPersona, in.Password)
	a.settings.PersonaCipher = ct
	a.personaKey = in.Password
	a.mu.Lock()
	atomicJSON(filepath.Join(a.dataPath, "settings.json"), a.settings)
	a.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": true})
}
