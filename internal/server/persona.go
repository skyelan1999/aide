package server

import (
	"context"
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
	"time"
)

// deriveKey 从用户密码派生 32 字节 AES-256 密钥。
// 生产环境（New 已加载 kdf-salt.bin）使用 Argon2id(固定salt)；
// 未加载 salt 的测试环境退化为 SHA-256，保持既有测试确定性。
// 旧版密文（SHA-256 密钥）在登录迁移时通过 re-wrap 统一转为新密钥；
// decryptPersona / voice_agent.unlock 仍会在新密钥失败时回退旧密钥，兼容迁移中途状态。
func deriveKey(password string) []byte {
	if len(kdfSalt) > 0 {
		return DeriveAESKey(password, kdfSalt)
	}
	return DeriveAESKeyLegacy(password)
}

// sha256Hex 返回输入的 SHA-256 十六进制摘要。
// 用于：① 旧版账户密码哈希（64hex，迁移用）；② 高熵随机 token（access-token / debug-token）的存储哈希。
// 高熵 token 本身不可暴力枚举，不需要慢 KDF，故保持裸 SHA-256。
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
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

// decryptPersona 解密。先用新派生密钥（Argon2id），失败回退旧 SHA-256 密钥，
// 兼容迁移中途（密文尚未 re-wrap）状态。
func decryptPersona(b64, password string) (string, error) {
	if b64 == "" {
		return "", nil
	}
	if pt, err := decryptWithKey(deriveKey(password), b64); err == nil {
		return string(pt), nil
	}
	if pt, err := decryptWithKey(DeriveAESKeyLegacy(password), b64); err == nil {
		return string(pt), nil
	}
	return "", errors.New("密码错误或数据损坏")
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

// ════════════════════════════════════════════════════════════════════════
// 可演化性格系统（明文，无需密码）
// 把旧的"人格切换"升级为两个独立、可开关、可编辑、会自我精简的性格：
//   aide  → 作用于基本聊天（性格设置页）
//   小秘  → 作用于语音小秘（语音小秘设置页）
// 性格提示词只影响对话风格；随使用（压缩 / 语音累计）自动重写为更短、信息更密的版本以节约 token。
// ════════════════════════════════════════════════════════════════════════

// Personality 一个可演化性格的运行态与持久态（明文存 settings.json）。
type Personality struct {
	Enabled    bool   `json:"enabled"`
	Prompt     string `json:"prompt"`
	Evolutions int    `json:"evolutions"`
	UpdatedAt  string `json:"updatedAt,omitempty"`
}

const defaultAidePersonality = `你是 aide，一名资深 AI 软件架构师兼开发搭档，服务于专注专业工作的用户。
- 专业务实：先给结论与可执行方案，再补必要细节，不写空话套话。
- 严谨：不确定的事实标注为推测；数字、命令、改动先核对再输出。
- 主动澄清：需求有歧义或缺关键信息时，一次只问最关键的问题，确认后再继续。
- 工程审美：代码安全、可读、可验证；UI/交互改动必须实际验证，不凭想象宣称完成。
- 默认中文、术语保留英文，回答紧凑，用结构化排版让重点一目了然。`

const defaultXiaomiPersonality = `你是用户的私人生活秘书「小秘」，也是熟悉他日常习惯的老朋友。
- 温暖自然、口语：先共情，再给一两条实在建议，不堆术语、不写成文档。
- 体贴：主动记事、提醒、安排日程，留意用户说过的偏好与待办。
- 简洁：日常闲聊轻松有趣、回答短；遇到工作/代码温和提醒切回 aide。
- 分寸：尊重隐私，背景声与旁人闲聊自动忽略，不打断、不乱传话。`

func defaultPersonalityPrompt(id string) string {
	if id == personaXiaomi {
		return defaultXiaomiPersonality
	}
	return defaultAidePersonality
}

func validPersonalityID(id string) string {
	if strings.TrimSpace(id) == personaXiaomi {
		return personaXiaomi
	}
	return personaAide
}

// personalityLocked 返回性格当前态（无记录则给默认，不落盘）。调用方需持 a.mu。
func (a *App) personalityLocked(id string) Personality {
	if a.settings.Personalities != nil {
		if p, ok := a.settings.Personalities[id]; ok {
			if strings.TrimSpace(p.Prompt) == "" {
				p.Prompt = defaultPersonalityPrompt(id)
			}
			return p
		}
	}
	return Personality{Prompt: defaultPersonalityPrompt(id)}
}

// persistPersonalitiesLocked 写回 settings.json。调用方需持 a.mu。
func (a *App) persistPersonalitiesLocked() {
	atomicJSON(filepath.Join(a.dataPath, "settings.json"), a.settings)
}

// ── HTTP ──────────────────────────────────────────────────────────────────
func (a *App) personalityGet(w http.ResponseWriter, r *http.Request) {
	id := validPersonalityID(r.URL.Query().Get("id"))
	a.mu.Lock()
	p := a.personalityLocked(id)
	a.mu.Unlock()
	jsonOut(w, 200, map[string]any{
		"id": id, "enabled": p.Enabled, "prompt": p.Prompt,
		"defaultPrompt": defaultPersonalityPrompt(id),
		"evolutions": p.Evolutions, "updatedAt": p.UpdatedAt,
		"estTokens": (len(p.Prompt) + 3) / 4,
	})
}

func (a *App) personalitySave(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID      string `json:"id"`
		Enabled bool   `json:"enabled"`
		Prompt  string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		fail(w, 400, errors.New("请求格式错误"))
		return
	}
	id := validPersonalityID(in.ID)
	prompt := strings.TrimSpace(in.Prompt)
	if prompt == "" {
		prompt = defaultPersonalityPrompt(id)
	}
	a.mu.Lock()
	p := a.personalityLocked(id)
	p.Enabled = in.Enabled
	p.Prompt = prompt
	p.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if a.settings.Personalities == nil {
		a.settings.Personalities = map[string]Personality{}
	}
	a.settings.Personalities[id] = p
	a.persistPersonalitiesLocked()
	a.mu.Unlock()
	jsonOut(w, 200, map[string]any{"ok": true, "enabled": p.Enabled, "prompt": p.Prompt})
}

func (a *App) personalityReset(w http.ResponseWriter, r *http.Request) {
	var in struct{ ID string `json:"id"` }
	json.NewDecoder(r.Body).Decode(&in)
	id := validPersonalityID(in.ID)
	a.mu.Lock()
	delete(a.settings.Personalities, id)
	a.persistPersonalitiesLocked()
	p := a.personalityLocked(id)
	a.mu.Unlock()
	jsonOut(w, 200, map[string]any{"ok": true, "enabled": p.Enabled, "prompt": p.Prompt})
}

func (a *App) personalityEvolve(w http.ResponseWriter, r *http.Request) {
	var in struct{ ID string `json:"id"` }
	json.NewDecoder(r.Body).Decode(&in)
	id := validPersonalityID(in.ID)
	a.mu.Lock()
	cur := a.personalityLocked(id)
	cfg := a.settings
	sample := a.personalitySampleLocked(id)
	a.mu.Unlock()
	np, err := a.evolvePersonality(id, cur, cfg, sample)
	if err != nil {
		fail(w, 400, err)
		return
	}
	a.mu.Lock()
	if a.settings.Personalities == nil {
		a.settings.Personalities = map[string]Personality{}
	}
	a.settings.Personalities[id] = np
	a.persistPersonalitiesLocked()
	a.mu.Unlock()
	jsonOut(w, 200, map[string]any{
		"ok": true, "enabled": np.Enabled, "prompt": np.Prompt,
		"evolutions": np.Evolutions, "updatedAt": np.UpdatedAt,
	})
}

// autoEvolvePersonality 后台自动演化（压缩 / 语音累计触发），失败静默。
func (a *App) autoEvolvePersonality(id, sample string) {
	defer func() { _ = recover() }()
	a.mu.Lock()
	cur := a.personalityLocked(id)
	if !cur.Enabled {
		a.mu.Unlock()
		return
	}
	cfg := a.settings
	if sample == "" {
		sample = a.personalitySampleLocked(id)
	}
	a.mu.Unlock()
	np, err := a.evolvePersonality(id, cur, cfg, sample)
	if err != nil {
		return
	}
	a.mu.Lock()
	if a.settings.Personalities == nil {
		a.settings.Personalities = map[string]Personality{}
	}
	a.settings.Personalities[id] = np
	a.persistPersonalitiesLocked()
	a.mu.Unlock()
}

// personalitySampleLocked 构造演化用的近期真实样本。调用方需持 a.mu。
func (a *App) personalitySampleLocked(id string) string {
	if id == personaXiaomi {
		if va := a.voiceAgent; va != nil {
			hist := va.snapshotHistory()
			start := len(hist) - 12
			if start < 0 {
				start = 0
			}
			var b strings.Builder
			for _, h := range hist[start:] {
				src := h.Summarized
				if src == "" {
					src = h.Heard
				}
				b.WriteString("用户: " + clip(src, 400) + "\n")
			}
			return clip(b.String(), 4000)
		}
		return ""
	}
	var latest *Session
	var latestT time.Time
	for _, s := range a.sessions {
		t, _ := time.Parse(time.RFC3339Nano, s.Updated)
		if latest == nil || t.After(latestT) {
			latest, latestT = s, t
		}
	}
	if latest == nil {
		return ""
	}
	start := len(latest.Messages) - 12
	if start < 0 {
		start = 0
	}
	var b strings.Builder
	for _, m := range latest.Messages[start:] {
		b.WriteString(m.Role + ": " + clip(m.Content, 400) + "\n")
	}
	return clip(b.String(), 4000)
}

// evolvePersonality 让模型把性格提示词重写为更短、信息更密的版本；结果必须更短才采纳。
func (a *App) evolvePersonality(id string, cur Personality, cfg Settings, sample string) (Personality, error) {
	old := strings.TrimSpace(cur.Prompt)
	if old == "" {
		old = defaultPersonalityPrompt(id)
	}
	sys := `你在帮助一个 AI 助手精炼它自己的"性格提示词"。只输出重写后的提示词正文，不要解释、标题、引号或 markdown 围栏。`
	usr := fmt.Sprintf(`当前性格提示词（%d 字符）：
"""
%s
"""

近期真实对话样本（仅用于校准风格、提炼用户稳定且反复出现的偏好；不要写入一次性琐事）：
"""
%s
"""

要求：
1. 完整保留核心人格、语气、原则，以及用户明确且反复出现的偏好。
2. 删除重复、空话、可由系统其它部分提供的内容；合并近义条目；用更短的句子。
3. 新提示词字符数必须严格少于当前（只减不增），信息密度更高，不要新增任何要求。
4. 直接输出新提示词正文。`, len(old), old, sample)
	params := ProfileParams{Temperature: fp(0.2), MaxTokens: 2048}
	out, _, _, err := complete(context.Background(), cfg, []Message{
		{Role: "system", Content: sys},
		{Role: "user", Content: usr},
	}, params, nil, nil)
	if err != nil {
		return cur, fmt.Errorf("演化调用失败: %w", err)
	}
	got := strings.TrimSpace(out)
	got = strings.TrimPrefix(got, "```")
	got = strings.TrimSuffix(got, "```")
	got = strings.Trim(got, "\""+"\n")
	got = strings.TrimSpace(got)
	if got == "" {
		return cur, errors.New("演化结果为空，已保留原版本")
	}
	if len(got) >= len(old) {
		return cur, errors.New("演化后未更精简，已保留原版本")
	}
	cur.Prompt = got
	cur.Evolutions++
	cur.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	return cur, nil
}

// messagesSample 把一批消息拼成演化样本（截断）。
func messagesSample(ms []Message) string {
	var b strings.Builder
	for _, m := range ms {
		b.WriteString(m.Role + ": " + clip(m.Content, 400) + "\n")
	}
	return clip(b.String(), 4000)
}
