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
	Summarized string `json:"summarized"`  // 总结后的清晰意图
	Action     string `json:"action"`     // send | ignore | standby | ask
	Text       string `json:"text"`        // action=send 时=总结后的意图（前端据此发送）
	Ask        string `json:"ask"`         // action=ask 时的单个追问
	Reason     string `json:"reason"`      // 小秘的分析理由
}

// VoiceMemory 小秘的长期记忆：与主记忆区分，记录用户习惯/偏好。
type VoiceMemory struct {
	Habits []string `json:"habits,omitempty"`
	Notes  string   `json:"notes,omitempty"`
}

const voiceHistoryMax = 200

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
	mu           sync.Mutex
	history      []VoiceHistoryEntry // 未加密恒在内存；加密后仅解锁时持有
	memory       VoiceMemory
	encrypted    bool   // 历史是否启用加密
	cachedCipher string // 加密落盘密文，锁定时保留供解锁
	key          []byte // 内存密钥，锁定为 nil
	dataPath     string
}

func newVoiceAgent(dataPath string) *VoiceAgent {
	va := &VoiceAgent{dataPath: dataPath}
	if b, err := os.ReadFile(filepath.Join(dataPath, "voice-history.json")); err == nil {
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
	if b, err := os.ReadFile(filepath.Join(dataPath, "voice-memory.json")); err == nil {
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
	_ = os.WriteFile(filepath.Join(va.dataPath, "voice-history.json"), b, 0o600)
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

// analyze 让小秘结合记忆与近期历史，像真人秘书一样推理判断这句听到的话如何处理。
// 决策结果会追加到历史并落盘。
func (va *VoiceAgent) analyze(ctx context.Context, cfg Settings, heard, aideCtx string) (VoiceHistoryEntry, error) {
	name := cfg.VoiceAssistantName
	if name == "" {
		name = "小秘"
	}

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
	system := fmt.Sprintf(`你是「%s」，用户的私人语音秘书。用户用很口语、啰嗦、重复、带口头禅和停顿的方式说话，周围还常有背景声和旁人插话。你要像一个聪明的真人秘书那样听懂他真正想什么，而不是机械转述。

请先在心里分析（不要输出分析过程）：
1. 这段声音里：哪些是用户本人对 AI 工作台(aide)说的？哪些是电视/视频/广播的背景声？哪些是用户在和身边真人打电话/闲聊？
2. 如果是背景声或旁人闲聊：action 用 ignore 或 standby（深入交谈/打电话时 standby 退下）。
3. 如果是用户对 aide 说话：把啰嗦、重复、口头禅、语气词全部去掉，总结成一句清晰、结构化、可直接执行的"真实意图"放进 summarized。总结要保留关键对象、动作和约束，不要编造用户没说的信息。
4. 如果意图还不清楚（缺对象、缺要做什么、含糊）：不要乱猜，action 用 ask，用一句话向用户追问（只问最关键的一个问题）。

%s你的长期记忆：%s
你最近处理过的上下文：
%s

只输出一个 JSON 对象（不要 markdown 围栏、不要任何多余文字）：
{"action":"send|ignore|standby|ask","summarized":"总结后的清晰意图（仅 send 时填写，其余为空）","ask":"单个简短追问（仅 action=ask 时填写）","reason":"一句话说明你的判断，写给用户看"}`, name, aideSection, voiceMemorySummary(mem), voiceRecentSummary(recent))

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
		Action    string `json:"action"`
		Summarized string `json:"summarized"`
		Ask       string `json:"ask"`
		Reason    string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		return VoiceHistoryEntry{}, errors.New("模型返回无法解析")
	}
	switch parsed.Action {
	case "send", "ignore", "standby", "ask":
	default:
		parsed.Action = "ignore"
	}
	entry := VoiceHistoryEntry{
		Time:       time.Now().Format("2006-01-02 15:04:05"),
		Heard:      heard,
		Summarized: strings.TrimSpace(parsed.Summarized),
		Action:     parsed.Action,
		Text:       strings.TrimSpace(parsed.Summarized), // 前端发送的是总结后的意图
		Ask:        strings.TrimSpace(parsed.Ask),
		Reason:     strings.TrimSpace(parsed.Reason),
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
func (va *VoiceAgent) unlock(password string) error {
	if password == "" {
		return errors.New("请输入密钥")
	}
	va.mu.Lock()
	defer va.mu.Unlock()
	if !va.encrypted {
		return errors.New("未启用加密")
	}
	key := deriveKey(password)
	if va.cachedCipher == "" {
		va.key = key
		return nil
	}
	b, err := decryptWithKey(key, va.cachedCipher)
	if err != nil {
		return errors.New("密钥错误或数据损坏")
	}
	var hist []VoiceHistoryEntry
	if err := json.Unmarshal(b, &hist); err != nil {
		return errors.New("数据损坏")
	}
	va.key = key
	va.history = hist
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
		b, err := decryptWithKey(deriveKey(password), va.cachedCipher)
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
	name := cfg.VoiceAssistantName
	if name == "" {
		name = "小秘"
	}
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
	system := fmt.Sprintf(`你是「%s」，用户的私人秘书。aide 刚完成了一些工作，用户希望你边演示边为他口头讲解。
下面按顺序列出了 %d 个演示环节（有的是打开某个文件，有的是讲解一段内容）。请你为【每个环节】写一句口语化、自然、亲切、简短的讲解词，就像坐在用户旁边讲解一样：可以适当概括和上下串联，不要照念原文，不要 markdown，不要输出编号或任何额外内容。
严格只输出一个 JSON 字符串数组，条数必须恰好等于环节数 %d，顺序与环节一一对应：
["环节1讲解","环节2讲解"]

环节清单：
%s`, name, len(steps), len(steps), sb.String())
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
