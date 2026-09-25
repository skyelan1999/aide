package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// VoiceHistoryEntry 小秘 agent 的一条决策记录：听到了什么、怎么分析、做了什么。
type VoiceHistoryEntry struct {
	Time       string `json:"time"`
	Heard      string `json:"heard"`      // 原始听到的口语
	Summarized string `json:"summarized"` // 总结后的清晰意图
	Action     string `json:"action"`     // send | ignore | standby | ask
	Text       string `json:"text"`       // action=send 时=总结后的意图（前端据此发送）
	Ask        string `json:"ask"`        // action=ask 时的单个追问
	Reason     string `json:"reason"`     // 小秘的分析理由
	Mode       string `json:"mode,omitempty"` // #41：action=send 时的发送调度 queue(默认,排队) | insert(插队,打断当前 run)
	Stop       bool   `json:"stop,omitempty"`  // #41：本条是否为中止/止损类指令（始终插队、不受冷却限制）
}

// VoiceMemory 小秘的长期记忆：与主记忆区分，记录用户习惯/偏好。
type VoiceMemory struct {
	Habits []string `json:"habits,omitempty"`
	Notes  string   `json:"notes,omitempty"`
}

const voiceHistoryMax = 200

// insertCooldownDur 按「插队敏感度」返回非中止类插队的冷却窗口：
// 冷却窗口内再次出现的非紧急 insert 会被降级为 queue，避免模型误判频繁打断当前 run。
// 明确中止/止损类（stop=true）不受此冷却限制，始终插队。
func insertCooldownDur(sensitivity string) time.Duration {
	switch sensitivity {
	case "aggressive": // 激进：几乎不冷却，鼓励即时插话
		return 1 * time.Second
	case "normal":
		return 3 * time.Second
	default: // conservative（默认，保守）
		return 5 * time.Second
	}
}

// VoiceAgent 小秘本身：一个有独立 system prompt、记忆与上下文的语音秘书 agent。
// 它不直接持有工具，而是判断"该不该把这句话转达给当前会话"——
// 判定为 send 的句子由前端发到当前会话，那里的主 agent 拥有全部工具与委派能力。
// voiceHistoryFile 历史落盘信封：未加密时直接存 History；加密时存 Cipher（AES-256-GCM，base64）。
type voiceHistoryFile struct {
	Encrypted bool                `json:"encrypted"`
	Cipher    string              `json:"cipher,omitempty"`
	History   []VoiceHistoryEntry `json:"history,omitempty"`
}

type VoiceAgent struct {
	mu                 sync.Mutex
	history            []VoiceHistoryEntry // 未加密恒在内存；加密后仅解锁时持有
	memory             VoiceMemory
	encrypted          bool   // 历史是否启用加密
	cachedCipher       string // 加密落盘密文，锁定时保留供解锁
	key                []byte // 内存密钥，锁定为 nil
	dataPath           string
	broker             *StreamBroker // #35：可选注入；小秘据此拉取 aide 主会话实时流式输出
	lastUrgentInsertAt time.Time     // #41：上一次【非中止类】插队时间戳，用于插队冷却
	now                func() time.Time // #41：可注入时钟，测试用；运行态=time.Now
}

func newVoiceAgent(dataPath string) *VoiceAgent {
	va := &VoiceAgent{dataPath: dataPath, now: time.Now}
	if b, err := os.ReadFile(VoiceHistoryPath(dataPath)); err == nil {
		var f voiceHistoryFile
		if json.Unmarshal(b, &f) == nil && (f.Encrypted || f.History != nil) {
			va.encrypted = f.Encrypted
			if f.Encrypted {
				va.cachedCipher = f.Cipher // 锁定态：不持有明文
			} else {
				va.history = f.History
			}
		} else {
			// 兼容裸数组格式
			var hist []VoiceHistoryEntry
			if json.Unmarshal(b, &hist) == nil {
				va.history = hist
			}
		}
	}
	if b, err := os.ReadFile(VoiceMemoryPath(dataPath)); err == nil {
		_ = json.Unmarshal(b, &va.memory)
	}
	return va
}

// persistLocked 调用方需已持锁。
func (va *VoiceAgent) persistLocked() {
	f := voiceHistoryFile{Encrypted: va.encrypted}
	switch {
	case va.encrypted && len(va.key) > 0:
		if b, err := json.Marshal(va.history); err == nil {
			f.Cipher, _ = encryptWithKey(va.key, b)
		}
	case va.encrypted:
		f.Cipher = va.cachedCipher // 锁定：保留原密文
	default:
		f.History = va.history
	}
	b, _ := json.Marshal(f)
	hp := VoiceHistoryPath(va.dataPath)
	_ = os.MkdirAll(filepath.Dir(hp), 0700)
	_ = os.WriteFile(hp, b, 0o600)
}

func voiceMemorySummary(m VoiceMemory) string {
	if len(m.Habits) == 0 && strings.TrimSpace(m.Notes) == "" {
		return "（暂无长期记忆）"
	}
	parts := []string{}
	if len(m.Habits) > 0 {
		parts = append(parts, "用户习惯："+strings.Join(m.Habits, "、"))
	}
	if strings.TrimSpace(m.Notes) != "" {
		parts = append(parts, "备注："+m.Notes)
	}
	return strings.Join(parts, "；")
}

func voiceRecentSummary(recent []VoiceHistoryEntry) string {
	if len(recent) == 0 {
		return "（刚启动，暂无上文）"
	}
	lines := []string{}
	for _, h := range recent {
		lines = append(lines, fmt.Sprintf("- [%s] 听到「%s」→ %s", h.Time, clip(h.Heard, 40), h.Action))
	}
	return strings.Join(lines, "\n")
}

// readAideMemory 只读 aide 的长期记忆（memory/core/memory.md），返回一段摘要供小秘参考。
//
// 单向只读：本类型【刻意不提供任何 writeAideMemory 方法】——小秘能看到 aide 记下了什么，
// 但绝不能改写/污染 aide 的记忆（编译期保证：无写方法即不可写）。
// 返回内容前先过 canAccessMemory 策略：小秘对 memory/core 仅读放行。
func (va *VoiceAgent) readAideMemory() string {
	p := aideAideMemoryFilePath(va.dataPath)
	if ok, reason := canAccessMemory(va.dataPath, callerAssistant, p, opRead); !ok {
		return "（无权读取 aide 记忆：" + reason + "）"
	}
	b, err := os.ReadFile(p)
	if err != nil || strings.TrimSpace(string(b)) == "" {
		return "（aide 暂无长期记忆）"
	}
	s := strings.TrimSpace(string(b))
	if len(s) > 4000 {
		s = s[:4000] + "…（已截断）"
	}
	return s
}

// aideAideMemoryFilePath aide 长期记忆文件的绝对路径（与 App.memoryPath 同一文件，#31/#35）。
func aideAideMemoryFilePath(dataPath string) string {
	return filepath.Join(MemoryCoreDir(dataPath), "memory.md")
}

// attachBroker 由 App 注入实时流式输出 broker（#35）。构造后调用一次。
func (va *VoiceAgent) attachBroker(b *StreamBroker) { va.broker = b }

// getSessionLiveOutput 小秘按会话 ID 拉取 aide 主会话的实时流式输出。
// 覆盖进行中/未落库内容；被打断(interrupted)时仍返回已产出部分。
// 与 #30 get_session（已落库消息）互补。返回 (output, status, found)。
func (va *VoiceAgent) getSessionLiveOutput(sessionID string) (output, status string, found bool) {
	if va == nil || va.broker == nil {
		return "", "", false
	}
	return va.broker.GetSessionLiveOutput(sessionID)
}

// voiceIdentityPrompt 小秘的集中身份核心：第一人称、稳定注入到 analyze / narrate / 小秘对话
// 等所有场景 system prompt 的最前面。名字动态取 cfg.VoiceAssistantName（默认"小秘"），
// 用户改名后下次调用立即使用新名字，保证任何场景下自我认知与自称一致。
// 身份核心保持稳定；语气/性格由可演化的小秘性格系统（persona.go）在其后追加，不在此堆砌。
func voiceIdentityPrompt(cfg Settings) string {
	name := strings.TrimSpace(cfg.VoiceAssistantName)
	if name == "" {
		name = "小秘"
	}
	return fmt.Sprintf(`【你是谁】你是「%s」，用户专属的私人秘书与伙伴。你就是你自己——你不是 aide，也不是一个泛用问答助手。在任何回答、讲解、自言自语里，都用第一人称自称：说"我"，说"我是%s"；不要用第三人称指代自己，也不要把自己介绍成 aide 或某个通用助手。

【你和 aide 的分工】aide 是负责实际干活的 AI 工作台：写代码、跑命令、操作文件、产出正式内容，它的主聊天不主动说话。你是用户与 aide 之间的桥梁：倾听、听懂真实意图、把意图传达调度给 aide、总结 aide 的成果、用大白话口头讲解、跟进待办与提醒；你同时也是用户的生活助理与伙伴。专业、正式的产出交给 aide，你不越权替它写代码或造内容。

【你能做什么】
- 语音意图判断：判断听到的话属于 send（转达给 aide）、ignore（电视/广播等背景声或旁人闲聊）、standby（用户打电话或深入交谈时退下）还是 ask（意图不清时只追问最关键的一句）。
- 跨会话调度：用 search_sessions 搜索历史会话、get_session 按 #编号 或会话 ID 读取（含归档）、follow_session 标记跟进、push_to_session 向指定会话推送备注。
- 控制屏幕滚动、打开文件；对 aide 的产出做总结与口头讲解；把 aide 的书面回复改写成顺口的口语稿念给用户听；必要时把任务交给合适的子 agent 或 aide。

【你的连续性】你拥有独立的长期记忆与对话历史（加密保存），可以引用过去说过的事，保持口吻、立场和称呼前后一致；你常驻在会话列表顶部的助理会话里。

【边界与准则】专业产出交给 aide，你不越权替它产出；不编造用户没说过的信息，不虚构自己没有的能力；保护用户隐私，遇到关键或不可逆的动作先向用户确认。`, name, name)
}

// analyze 让小秘结合记忆与近期历史，像真人秘书一样推理判断这句听到的话如何处理。
// 决策结果会追加到历史并落盘。
func (va *VoiceAgent) analyze(ctx context.Context, cfg Settings, heard, aideCtx string) (VoiceHistoryEntry, error) {
	va.mu.Lock()
	mem := va.memory
	recent := make([]VoiceHistoryEntry, 0, 10)
	if n := len(va.history); n > 0 {
		start := n - 10
		if start < 0 {
			start = 0
		}
		recent = append(recent, va.history[start:]...)
	}
	va.mu.Unlock()

	aideSection := ""
	if strings.TrimSpace(aideCtx) != "" {
		aideSection = fmt.Sprintf("aide 主工作台最近与用户的对话内容如下（用户可能让你讲解、总结或接着讨论；你要据此回答，不要声称看不到）：\n%s\n\n", aideCtx)
	}
	// aide 长期记忆（只读参考）：与小秘自己的私有记忆分两块明确区分，避免混淆归属。
	aideMemBlock := fmt.Sprintf("【aide 的长期记忆（这是 aide 记下的用户偏好/项目约定，你只能参考、绝不修改；它不是你自己的记忆）】\n%s\n\n", va.readAideMemory())
	// 身份核心前置：任何场景下先确立"我是谁"，再接本环节的具体任务指令。
	system := voiceIdentityPrompt(cfg) + "\n\n" + fmt.Sprintf(`【当前任务：听懂这句口语】用户用很口语、啰嗦、重复、带口头禅和停顿的方式说话，周围还常有背景声和旁人插话。你要像一个聪明的真人秘书那样听懂他真正想什么，而不是机械转述。

请先在心里分析（不要输出分析过程）：
1. 这段声音里：哪些是用户本人对 AI 工作台(aide)说的？哪些是电视/视频/广播的背景声？哪些是用户在和身边真人打电话/闲聊？
2. 如果是背景声或旁人闲聊：action 用 ignore 或 standby（深入交谈/打电话时 standby 退下）。
3. 如果是用户对 aide 说话：把啰嗦、重复、口头禅、语气词全部去掉，总结成一句清晰、结构化、可直接执行的"真实意图"放进 summarized。总结要保留关键对象、动作和约束，不要编造用户没说的信息。
4. 如果意图还不清楚（缺对象、缺要做什么、含糊）：不要乱猜，action 用 ask，用一句话向用户追问（只问最关键的一个问题）。
5. 当 action=send 时，还要判断这条意图该「插队(insert)」还是「排队(queue)」：
   - insert（插队，立即打断/插入当前正在进行的回答）：明确紧急措辞（立刻/马上/赶紧/现在就要）；中止或止损（停/停下来/取消/中止/不对、错了、不是这样、等一下）；强时效、必须现在处理；与当前上下文强相关、需要即时交互的追问。
   - queue（排队，等当前回答完成后再按顺序处理；这是默认）：独立的新任务/新需求；不紧急、可以等当前完成；补充或后续事项（然后/待会/顺便/接下来）。
   - 拿不准一律 queue。
   - 若本条是「中止/止损」类（停、取消、不对、错了、等一下、不是这样），把 stop 设为 true；其余情况 stop 一律 false。

%s
%s你自己的长期记忆（小秘私有）：%s
你最近处理过的上下文：
%s

只输出一个 JSON 对象（不要 markdown 围栏、不要任何多余文字）：
{"action":"send|ignore|standby|ask","summarized":"总结后的清晰意图（仅 send 时填写，其余为空）","ask":"单个简短追问（仅 action=ask 时填写）","mode":"queue|insert（仅 send 时判断，其余填空字符串；拿不准填 queue）","stop":false,"reason":"一句话说明你的判断，写给用户看"}`, aideSection, aideMemBlock, voiceMemorySummary(mem), voiceRecentSummary(recent))

	params := ProfileParams{MaxTokens: 700, Temperature: fp(0.2)} // 320 在 JSON 较长时易被 length 截断导致解析失败
	out, _, _, err := complete(ctx, cfg, []Message{
		{Role: "system", Content: system},
		{Role: "user", Content: "刚听到：" + heard},
	}, params, nil, nil)
	if err != nil {
		return VoiceHistoryEntry{}, err
	}
	out = strings.TrimSpace(out)
	out = strings.TrimPrefix(out, "```json")
	out = strings.TrimPrefix(out, "```")
	out = strings.TrimSuffix(out, "```")
	var parsed struct {
		Action     string `json:"action"`
		Summarized string `json:"summarized"`
		Ask        string `json:"ask"`
		Reason     string `json:"reason"`
		Mode       string `json:"mode"`
		Stop       bool   `json:"stop"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		return VoiceHistoryEntry{}, errors.New("模型返回无法解析")
	}
	switch parsed.Action {
	case "send", "ignore", "standby", "ask":
	default:
		parsed.Action = "ignore"
	}
	// #41：发送调度 mode 归一化——仅 send 有意义；非法/空一律回落配置默认（默认 queue，保守）。
	mode := strings.ToLower(strings.TrimSpace(parsed.Mode))
	if mode != "insert" {
		mode = "queue"
		if cfg.VoiceDefaultSendMode == "insert" {
			mode = "insert"
		}
	}
	entry := VoiceHistoryEntry{
		Time:       time.Now().Format("2006-01-02 15:04:05"),
		Heard:      heard,
		Summarized: strings.TrimSpace(parsed.Summarized),
		Action:     parsed.Action,
		Text:       strings.TrimSpace(parsed.Summarized), // 前端发送的是总结后的意图
		Ask:        strings.TrimSpace(parsed.Ask),
		Reason:     strings.TrimSpace(parsed.Reason),
		Mode:       mode,
		Stop:       parsed.Stop,
	}
	// #41：插队冷却。仅对【send + insert + 非中止类】生效——
	// 冷却窗口内重复的非紧急插队降级为 queue，防止模型误判频繁打断当前 run；
	// 明确中止/止损类（stop=true）始终插队，且不占用冷却额度。
	if entry.Action == "send" && entry.Mode == "insert" && !entry.Stop {
		cooldown := insertCooldownDur(cfg.VoiceInsertSensitivity)
		now := va.now()
		va.mu.Lock()
		downgrade := !va.lastUrgentInsertAt.IsZero() && now.Sub(va.lastUrgentInsertAt) < cooldown
		if downgrade {
			entry.Mode = "queue"
			entry.Reason = strings.TrimSpace(entry.Reason) +
				fmt.Sprintf("（%.0f秒内重复插队，触发冷却，已降级为排队）", cooldown.Seconds())
		} else {
			va.lastUrgentInsertAt = now
		}
		va.mu.Unlock()
	}
	// 加密但锁定时不持有明文、不落盘；其余情况记录并持久化
	va.mu.Lock()
	if !va.encrypted || len(va.key) > 0 {
		va.history = append(va.history, entry)
		if len(va.history) > voiceHistoryMax {
			va.history = va.history[len(va.history)-voiceHistoryMax:]
		}
		va.persistLocked()
	}
	va.mu.Unlock()
	return entry, nil
}

// recordFallback 在 AI 分析未能完成（模型报错/截断/未配置）时也落一条决策，
// 保证"听到了什么、最终怎么处理"在审核时间线中不丢事件；兜底按原文发送。
func (va *VoiceAgent) recordFallback(heard, reason string) VoiceHistoryEntry {
	entry := VoiceHistoryEntry{
		Time:   time.Now().Format("2006-01-02 15:04:05"),
		Heard:  heard,
		Action: "send",
		Text:   heard,
		Reason: reason,
	}
	va.mu.Lock()
	if !va.encrypted || len(va.key) > 0 {
		va.history = append(va.history, entry)
		if len(va.history) > voiceHistoryMax {
			va.history = va.history[len(va.history)-voiceHistoryMax:]
		}
		va.persistLocked()
	}
	va.mu.Unlock()
	return entry
}

// historyDesc 返回倒序（最新在前）的历史副本，供设置页时间线展示。
func (va *VoiceAgent) historyDesc() []VoiceHistoryEntry {
	va.mu.Lock()
	defer va.mu.Unlock()
	out := make([]VoiceHistoryEntry, len(va.history))
	copy(out, va.history)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func (va *VoiceAgent) clearHistory() {
	va.mu.Lock()
	va.history = nil
	va.persistLocked()
	va.mu.Unlock()
}

// encStatus 返回历史加密/解锁状态（不泄露内容）。
func (va *VoiceAgent) encStatus() map[string]any {
	va.mu.Lock()
	defer va.mu.Unlock()
	return map[string]any{
		"encrypted": va.encrypted,
		"unlocked":  !va.encrypted || len(va.key) > 0,
		"count":     len(va.history),
	}
}

// enable 启用加密并迁移已有历史。
func (va *VoiceAgent) enable(password string) error {
	if password == "" {
		return errors.New("请设置密钥")
	}
	va.mu.Lock()
	defer va.mu.Unlock()
	if va.encrypted {
		return errors.New("已启用加密")
	}
	va.key = deriveKey(password)
	va.encrypted = true
	va.persistLocked()
	return nil
}

// unlock 用密钥解锁并解密恢复历史。
// 先用新派生密钥（Argon2id）；失败回退旧 SHA-256 密钥，兼容迁移中途密文。
func (va *VoiceAgent) unlock(password string) error {
	if password == "" {
		return errors.New("请输入密钥")
	}
	va.mu.Lock()
	defer va.mu.Unlock()
	if !va.encrypted {
		return errors.New("未启用加密")
	}
	if va.cachedCipher == "" {
		va.key = deriveKey(password)
		return nil
	}
	key, hist, err := va.decryptCached(password)
	if err != nil {
		return err
	}
	va.key = key
	va.history = hist
	return nil
}

// decryptCached 尝试用新密钥（Argon2id）解密密文；失败回退旧 SHA-256 密钥。
// 返回成功使用的密钥与解析出的历史。调用方需持 va.mu。
func (va *VoiceAgent) decryptCached(password string) ([]byte, []VoiceHistoryEntry, error) {
	candidates := []struct{ key []byte }{
		{deriveKey(password)},
		{DeriveAESKeyLegacy(password)},
	}
	for _, c := range candidates {
		b, err := decryptWithKey(c.key, va.cachedCipher)
		if err != nil {
			continue
		}
		var hist []VoiceHistoryEntry
		if json.Unmarshal(b, &hist) != nil {
			continue
		}
		return c.key, hist, nil
	}
	return nil, nil, errors.New("密钥错误或数据损坏")
}

// ReWrapAll 用 oldKey 解密现有密文、再用 newKey 重加密并落盘。
// 用于密码哈希升级（SHA-256→Argon2id）时平滑迁移小秘历史密文；无密文时跳过。
// 若当前已解锁持有明文，同步把内存密钥切到 newKey。
func (va *VoiceAgent) ReWrapAll(oldKey, newKey []byte) error {
	va.mu.Lock()
	defer va.mu.Unlock()
	if !va.encrypted || va.cachedCipher == "" {
		return nil // 无密文，跳过
	}
	b, err := decryptWithKey(oldKey, va.cachedCipher)
	if err != nil {
		return errors.New("旧密钥解密小秘历史失败")
	}
	newCipher, err := encryptWithKey(newKey, b)
	if err != nil {
		return err
	}
	va.cachedCipher = newCipher
	if len(va.key) > 0 {
		va.key = newKey
	}
	va.persistLocked()
	return nil
}

// lock 锁定：清内存密钥与明文历史。
func (va *VoiceAgent) lock() {
	va.mu.Lock()
	defer va.mu.Unlock()
	va.key = nil
	va.history = nil
}

// changePassword 修改密钥（需先解锁并能解开现有密文）。
func (va *VoiceAgent) changePassword(oldPw, newPw string) error {
	if newPw == "" {
		return errors.New("请设置新密钥")
	}
	va.mu.Lock()
	defer va.mu.Unlock()
	if len(va.key) == 0 {
		return errors.New("请先解锁")
	}
	if va.cachedCipher != "" {
		if _, err := decryptWithKey(va.key, va.cachedCipher); err != nil {
			return errors.New("原密钥错误")
		}
	}
	va.key = deriveKey(newPw)
	va.persistLocked()
	return nil
}

// disable 关闭加密（需验证密钥）：解密回明文落盘。
func (va *VoiceAgent) disable(password string) error {
	va.mu.Lock()
	defer va.mu.Unlock()
	if !va.encrypted {
		return nil
	}
	if va.cachedCipher != "" {
		// 新密钥失败回退旧 SHA-256 密钥，兼容迁移中途密文
		b, err := decryptWithKey(deriveKey(password), va.cachedCipher)
		if err != nil {
			b, err = decryptWithKey(DeriveAESKeyLegacy(password), va.cachedCipher)
		}
		if err != nil {
			return errors.New("密钥错误，无法解密回明文")
		}
		var hist []VoiceHistoryEntry
		if json.Unmarshal(b, &hist) == nil {
			va.history = hist
		}
	}
	va.encrypted = false
	va.key = nil
	va.cachedCipher = ""
	va.persistLocked()
	return nil
}

// snapshotHistory 返回历史副本（加密时仅解锁后有内容），供性格演化取样。
func (va *VoiceAgent) snapshotHistory() []VoiceHistoryEntry {
	va.mu.Lock()
	defer va.mu.Unlock()
	return append([]VoiceHistoryEntry{}, va.history...)
}

// NarrationStepInput 讲解环节（前端确定性骨架）：openFile=打开文件，speak=聚焦讲解。
type NarrationStepInput struct {
	Kind string `json:"kind"`
	Path string `json:"path"`
	Text string `json:"text"`
}

// narrate 让小蜜为每个演示环节生成一句口语化讲解词，返回与 steps 等长、顺序对齐的数组。
func (va *VoiceAgent) narrate(ctx context.Context, cfg Settings, steps []NarrationStepInput) ([]string, error) {
	var sb strings.Builder
	for i, st := range steps {
		if st.Kind == "openFile" {
			sb.WriteString(fmt.Sprintf("环节%d（打开文件）：%s\n", i+1, st.Path))
		} else {
			txt := strings.TrimSpace(st.Text)
			if len(txt) > 1400 {
				txt = txt[:1400]
			}
			sb.WriteString(fmt.Sprintf("环节%d（讲解内容）：%s\n", i+1, txt))
		}
	}
	// 身份核心前置：与 analyze / 对话共用同一"我是谁"，再接本环节讲解任务。
	// 附带 aide 长期记忆（只读参考），让讲解时能联系 aide 此前记下的偏好与约定。
	system := voiceIdentityPrompt(cfg) + "\n\n" + fmt.Sprintf(`【当前任务：边演示边口头讲解】aide 刚完成了一些工作，用户希望你边演示边为他口头讲解。
下面按顺序列出了 %d 个演示环节（有的是打开某个文件，有的是讲解一段内容）。请你为【每个环节】写一句口语化、自然、亲切、简短的讲解词，就像坐在用户旁边讲解一样：可以适当概括和上下串联，不要照念原文，不要 markdown，不要输出编号或任何额外内容。
严格只输出一个 JSON 字符串数组，条数必须恰好等于环节数 %d，顺序与环节一一对应：
["环节1讲解","环节2讲解"]

环节清单：
%s

【aide 的长期记忆（只读参考，不要修改）】
%s`, len(steps), len(steps), sb.String(), va.readAideMemory())
	params := ProfileParams{MaxTokens: 1000, Temperature: fp(0.45)}
	out, _, _, err := complete(ctx, cfg, []Message{{Role: "system", Content: system}}, params, nil, nil)
	if err != nil {
		return nil, err
	}
	out = strings.TrimSpace(out)
	out = strings.TrimPrefix(out, "```json")
	out = strings.TrimPrefix(out, "```")
	out = strings.TrimSuffix(strings.TrimSpace(out), "```")
	var speaks []string
	if err := json.Unmarshal([]byte(out), &speaks); err != nil {
		return nil, errors.New("讲解稿无法解析")
	}
	for i := range steps {
		if i < len(speaks) && strings.TrimSpace(speaks[i]) != "" {
			continue
		}
		fallback := ""
		if steps[i].Kind == "openFile" {
			fallback = "我们先来看这个文件：" + steps[i].Path
		} else {
			fallback = clip(stripMdForNarrate(steps[i].Text), 200)
		}
		if i >= len(speaks) {
			speaks = append(speaks, fallback)
		} else {
			speaks[i] = fallback
		}
	}
	if len(speaks) > len(steps) {
		speaks = speaks[:len(steps)]
	}
	return speaks, nil
}

// stripMdForNarrate 仅用于讲解词兜底：粗略剥除 markdown 符号。
func stripMdForNarrate(s string) string {
	for _, rep := range []string{"**", "##", "`", "#", "[", "]", "*"} {
		s = strings.ReplaceAll(s, rep, "")
	}
	return strings.TrimSpace(s)
}
