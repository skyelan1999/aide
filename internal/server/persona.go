package server

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
)

// deriveKey 从用户密码派生 32 字节 AES-256 密钥
func deriveKey(password string) []byte {
	sum := sha256.Sum256([]byte(password))
	return sum[:]
}

// sha256Hex 返回密码的 SHA-256 十六进制摘要，用于账户密码校验（不存明文）。
// 与 deriveKey 同源：账户密码即小秘历史加密密钥。
func sha256Hex(password string) string {
	return hex.EncodeToString(deriveKey(password))
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

// encryptWithKey 用已派生的 32 字节 AES-256 密钥加密，返回 base64(nonce+ciphertext)。
// voice_agent 等模块复用，密钥只在内存持有，不落盘。
func encryptWithKey(key, plaintext []byte) (string, error) {
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
	ct := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(ct), nil
}

// decryptWithKey 用内存密钥解密 base64(nonce+ciphertext)。
func decryptWithKey(key []byte, b64 string) ([]byte, error) {
	if b64 == "" {
		return nil, errors.New("无密文")
	}
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	ns := gcm.NonceSize()
	if len(data) < ns {
		return nil, errors.New("数据损坏")
	}
	return gcm.Open(nil, data[:ns], data[ns:], nil)
}

// ── 多人格系统 ─────────────────────────────────────────────────────────────
// 一套系统同时承载"工作(aide)"与"生活(小秘)"两个独立人格：
//   - aide  ：工作向，专业简洁的 AI 开发助手（主会话默认人格）
//   - 小秘  ：生活向，用户的私人语音/生活秘书（语音听写、解锁欢迎、日常对话）
//
// 每个人格有独立的 system 设定与记忆边界；通过 activePersona 切换语气与能力侧重。
// 框架可扩展：在 builtinPersonas 追加条目即可新增更多人格。

const (
	personaAide   = "aide"  // 工作人格
	personaXiaomi = "xiaomi" // 生活人格
)

// Persona 一个独立人格的定义与运行态。
type Persona struct {
	ID       string `json:"id"`
	Name     string `json:"name"`      // 显示名（小秘可被用户改名）
	Role     string `json:"role"`      // work | life
	Builtin  bool   `json:"builtin"`   // 是否内置人格
	Editable bool   `json:"editable"`   // 自定义性格是否可编辑/重置
	// Cipher 为持久化密文（settings.json，AES-256-GCM，base64），保存该人格的自定义性格补充。
	Cipher string `json:"cipher,omitempty"`
	// custom 为运行态明文（不落盘），仅解锁后持有。
	custom string `json:"-"`
}

// builtinPersonas 内置人格注册表。新增人格在此追加即可，其余流程自动复用。
var builtinPersonas = []Persona{
	{ID: personaAide, Name: "aide", Role: "work", Builtin: true, Editable: true},
	{ID: personaXiaomi, Name: "小秘", Role: "life", Builtin: true, Editable: true},
}

// xiaomiMainPrompt 小秘人格在主会话中的生活向 system 设定（区别于 aide 的工作设定）。
// %s = 小秘的显示名。语音听写时 voice_agent 使用更专门的甄别提示词。
const xiaomiMainPrompt = `你是「%s」，用户的私人生活秘书兼贴心伙伴。语气亲切、自然、温暖，像一个熟悉用户习惯的老朋友。用户用中文和你闲聊、安排生活、记事、问日常问题、倾诉心事。
回答要简短、口语、有温度：先共情，再给一两条实在的建议；不要堆砌术语，不要写成技术文档，不要主动罗列文件/命令。
你可以帮用户记事、提醒、规划日程、解释生活常识、出主意、陪聊。当用户提出写代码、工程、文件操作等工作任务时，温和地提示他切回 aide 工作人格，并简短说明 aide 更擅长这类事。`

// personaDisplayName 返回人格显示名：小秘名字跟随设置 VoiceAssistantName。
func (a *App) personaDisplayName(id string) string {
	if id == personaXiaomi {
		if n := strings.TrimSpace(a.settings.VoiceAssistantName); n != "" {
			return n
		}
	}
	for _, p := range builtinPersonas {
		if p.ID == id {
			return p.Name
		}
	}
	return id
}

// activePersonaID 返回当前活动人格 id，非法值回落 aide。
func (a *App) activePersonaID() string {
	id := strings.TrimSpace(a.settings.ActivePersona)
	for _, p := range builtinPersonas {
		if p.ID == id {
			return id
		}
	}
	return personaAide
}

// baseSystemPrompt 返回当前活动人格的基础 system 提示词。
func (a *App) baseSystemPrompt() string {
	if a.activePersonaID() == personaXiaomi {
		return fmt.Sprintf(xiaomiMainPrompt, a.personaDisplayName(personaXiaomi))
	}
	return systemPrompt // aide 工作人格的开发助手设定（workflow.go）
}

// personaListOut 返回人格列表（不含明文），供前端切换器渲染。调用方需持有 a.mu。
func (a *App) personaListOut() []map[string]any {
	out := make([]map[string]any, 0, len(builtinPersonas))
	for _, p := range builtinPersonas {
		_, hasCustom := a.settings.PersonaCiphers[p.ID]
		out = append(out, map[string]any{
			"id":       p.ID,
			"name":     a.personaDisplayName(p.ID),
			"role":     p.Role,
			"builtin":  p.Builtin,
			"editable": p.Editable,
			"active":   p.ID == a.activePersonaID(),
			"hasCustom": hasCustom,
			"unlocked": a.personaKey != "",
		})
	}
	return out
}

// unlockPersonas 用密码解锁：持有密钥并解密所有人格的自定义性格明文。
func (a *App) unlockPersonas(password string) {
	a.personaKey = password
	a.personaCustom = map[string]string{}
	for id, cipher := range a.settings.PersonaCiphers {
		if plain, err := decryptPersona(cipher, password); err == nil && strings.TrimSpace(plain) != "" {
			a.personaCustom[id] = plain
		}
	}
}

// personaGet 返回性格/人格状态（不返回明文）
func (a *App) personaGet(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"enabled":  a.settings.PersonaEnabled,
		"unlocked": a.personaKey != "",
		"active":   a.activePersonaID(),
		"personas": a.personaListOut(),
	})
}

// personaUnlock 用密码解锁性格系统（同时解锁所有人格的自定义性格）
func (a *App) personaUnlock(w http.ResponseWriter, r *http.Request) {
	var in struct{ Password string `json:"password"` }
	json.NewDecoder(r.Body).Decode(&in)
	if len(a.settings.PersonaCiphers) == 0 {
		// 首次：接受密码作为密钥（自定义性格可后续在设置里填写）
		if in.Password == "" {
			http.Error(w, "请设置密码", 400)
			return
		}
		a.unlockPersonas(in.Password)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "firstSetup": true})
		return
	}
	// 验证：至少能解开一个已存密文
	ok := false
	for _, c := range a.settings.PersonaCiphers {
		if _, err := decryptPersona(c, in.Password); err == nil {
			ok = true
			break
		}
	}
	if !ok {
		http.Error(w, "密码错误", 401)
		return
	}
	a.unlockPersonas(in.Password)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

// personaSave 保存当前活动人格的自定义性格内容（用当前内存密钥加密）
func (a *App) personaSave(w http.ResponseWriter, r *http.Request) {
	if a.personaKey == "" {
		http.Error(w, "请先解锁性格", 401)
		return
	}
	var in struct {
		ID      string `json:"id"`
		Content string `json:"content"`
	}
	json.NewDecoder(r.Body).Decode(&in)
	id := a.activePersonaID()
	if in.ID != "" {
		id = in.ID
	}
	if !a.personaEditable(id) {
		http.Error(w, "该人格不可编辑", 400)
		return
	}
	ct, err := encryptPersona(in.Content, a.personaKey)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if a.settings.PersonaCiphers == nil {
		a.settings.PersonaCiphers = map[string]string{}
	}
	a.settings.PersonaCiphers[id] = ct
	if strings.TrimSpace(in.Content) != "" {
		a.personaCustom[id] = in.Content
	} else {
		delete(a.personaCustom, id)
	}
	a.mu.Lock()
	atomicJSON(filepath.Join(a.dataPath, "settings.json"), a.settings)
	a.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

// personaReset 重置指定人格的自定义性格为默认（需密码验证）
func (a *App) personaReset(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID       string `json:"id"`
		Password string `json:"password"`
	}
	json.NewDecoder(r.Body).Decode(&in)
	id := a.activePersonaID()
	if in.ID != "" {
		id = in.ID
	}
	if !a.personaEditable(id) {
		http.Error(w, "该人格不可重置", 400)
		return
	}
	cipher := a.settings.PersonaCiphers[id]
	if cipher != "" {
		if _, err := decryptPersona(cipher, in.Password); err != nil {
			http.Error(w, "密码错误，无法重置", 401)
			return
		}
	}
	delete(a.settings.PersonaCiphers, id)
	delete(a.personaCustom, id)
	a.personaKey = in.Password
	a.mu.Lock()
	atomicJSON(filepath.Join(a.dataPath, "settings.json"), a.settings)
	a.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

// personasActive 切换当前活动人格
func (a *App) personasActive(w http.ResponseWriter, r *http.Request) {
	var in struct{ ID string `json:"id"` }
	if decode(w, r, &in) != nil {
		return
	}
	id := strings.TrimSpace(in.ID)
	valid := false
	for _, p := range builtinPersonas {
		if p.ID == id {
			valid = true
		}
	}
	if !valid {
		fail(w, 400, errors.New("未知人格"))
		return
	}
	a.settings.ActivePersona = id
	a.mu.Lock()
	atomicJSON(filepath.Join(a.dataPath, "settings.json"), a.settings)
	a.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "active": id, "name": a.personaDisplayName(id)})
}

// personasList 列出现有人格（供前端切换器）
func (a *App) personasList(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"active": a.activePersonaID(), "personas": a.personaListOut()})
}

// personaEditable 判断人格是否可编辑/重置
func (a *App) personaEditable(id string) bool {
	for _, p := range builtinPersonas {
		if p.ID == id {
			return p.Editable
		}
	}
	return false
}
