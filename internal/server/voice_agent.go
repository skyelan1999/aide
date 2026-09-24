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
	Time   string `json:"time"`
	Heard  string `json:"heard"`
	Action string `json:"action"` // send | ignore | standby
	Text   string `json:"text"`   // action=send 时清洗后发送给 aide 的指令
	Reason string `json:"reason"` // 小秘的分析理由
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
type VoiceAgent struct {
	mu       sync.Mutex
	history  []VoiceHistoryEntry
	memory   VoiceMemory
	dataPath string
}

func newVoiceAgent(dataPath string) *VoiceAgent {
	va := &VoiceAgent{dataPath: dataPath}
	if b, err := os.ReadFile(filepath.Join(dataPath, "voice-history.json")); err == nil {
		_ = json.Unmarshal(b, &va.history)
	}
	if b, err := os.ReadFile(filepath.Join(dataPath, "voice-memory.json")); err == nil {
		_ = json.Unmarshal(b, &va.memory)
	}
	return va
}

func (va *VoiceAgent) persistLocked() {
	b, _ := json.Marshal(va.history)
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
func (va *VoiceAgent) analyze(ctx context.Context, cfg Settings, heard string) (VoiceHistoryEntry, error) {
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

	system := fmt.Sprintf(`你是「%s」，用户的私人语音秘书，不是关键词过滤器。你像一个戴着耳机、懂分寸的真人秘书那样听用户周围的声音，需要自己分析判断。

请先在心里分析（不要输出分析过程）：
1. 说话人是谁——用户本人对 AI 工作台(aide)下指令/提问？用户在和身边真人或打电话？还是电视/视频/广播的声音？
2. 这句话的意图——任务安排、提问、随口评论、背景台词、还是与工作无关的寒暄？
3. 该不该转达给 aide——只有"用户对 aide 下的指令/提问/安排任务"才值得发送；拿不准时宁可忽略，绝不要误发。
4. 该不该退下——用户正在和身边真人深入交谈或打电话时，你应安静退下不录入，直到用户重新对 aide 说话。

你的长期记忆：%s
你最近处理过的上下文：
%s

只输出一个 JSON 对象（不要 markdown 围栏、不要任何多余文字）：
{"action":"send|ignore|standby","text":"清洗后要发给 aide 的指令原文（ignore/standby 时为空字符串）","reason":"一句话说明你为什么这样判断，写给用户看"}`, name, voiceMemorySummary(mem), voiceRecentSummary(recent))

	params := ProfileParams{MaxTokens: 320, Temperature: fp(0.2)}
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
		Action string `json:"action"`
		Text   string `json:"text"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		return VoiceHistoryEntry{}, errors.New("模型返回无法解析")
	}
	switch parsed.Action {
	case "send", "ignore", "standby":
	default:
		parsed.Action = "ignore"
	}
	entry := VoiceHistoryEntry{
		Time:   time.Now().Format("2006-01-02 15:04:05"),
		Heard:  heard,
		Action: parsed.Action,
		Text:   strings.TrimSpace(parsed.Text),
		Reason: strings.TrimSpace(parsed.Reason),
	}
	va.mu.Lock()
	va.history = append(va.history, entry)
	if len(va.history) > voiceHistoryMax {
		va.history = va.history[len(va.history)-voiceHistoryMax:]
	}
	va.persistLocked()
	va.mu.Unlock()
	return entry, nil
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
