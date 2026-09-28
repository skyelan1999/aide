package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

// ════════════════════════════════════════════════════════════════════════
// 可演化性格：触发持久化 + 演化/压缩解耦 + 可观测审计 + 统一取样 + 回滚上限（#34）
//
// 根因修复：
//   1. 旧触发太窄且不持久——aide 只在压缩后演化、小秘计数只在内存（重启清零）。
//      现在"自上次演化以来的交互计数/时间"落盘 config/personality-state.json，重启不清零。
//   2. 旧演化被等同于"必须严格更短"，默认提示词已精炼后微调几乎必然被拒。
//      现在区分两种模式：常规演化（精炼 + 偏好微调，允许小幅变长 ≤1.1×）与
//      压缩事件（只减不增）。
//   3. 旧 autoEvolve 失败/被拒完全静默。现在每次尝试都写审计 + log.Printf。
//   4. 小秘触发曾传空 sample。现在统一取样：aide 取近期真实消息、小秘取语音历史+会话。
// ════════════════════════════════════════════════════════════════════════

// ── 阈值常量 ──────────────────────────────────────────────────────────────
const (
	personalityAideMsgThreshold   = 20   // aide：每 N 条用户消息触发一次常规演化
	personalityXiaomiIntThreshold = 15   // 小秘：每 N 次有效交互（send/analyze 成功）触发
	personalityStableMinInteract  = 5    // 时间维度触发时要求的最低累计交互数
	personalityEvolveAfterHours   = 72   // 距上次演化超过 N 小时且交互足够 → 兜底触发
	personalityMaxPromptChars     = 4000 // 提示词硬上限，防长期膨胀
	personalityGrowRatio          = 1.1  // 常规演化允许的最大变长倍率
	personalitySimilarityCutoff   = 0.9  // 与旧版相似度 ≥ 此值视为"无实质变化"，不采纳也不计数
	personalityHistoryLimit       = 5    // 回滚保留的最近版本数
	personalityMinSampleMsgs      = 5    // 演化最小样本条数，不足则安全跳过
)

// personalityMode 演化模式：常规精炼+偏好微调 vs 压缩事件只减不增。
type personalityMode string

const (
	modeRefine   personalityMode = "refine"   // 常规：精炼 + 依稳定偏好微调（允许 ≤1.1× 变长）
	modeCompress personalityMode = "compress" // 压缩/归档事件：只减不增
)

// ── 持久化状态（config/personality-state.json，明文）──────────────────────
// 注意：这里只存"触发计数 / 上次尝试结果 / 回滚历史"，不存性格提示词本体本身
// （提示词仍在 settings.json 的 Personalities，遵循既有目录分层）。
type personalityStateEntry struct {
	CountSinceEvolve  int      `json:"countSinceEvolve"`            // 自上次演化以来的交互计数（持久，重启不清零）
	LastEvolvedAt     string   `json:"lastEvolvedAt,omitempty"`     // 上次成功演化时间（RFC3339Nano）
	LastAttemptAt     string   `json:"lastAttemptAt,omitempty"`     // 上次尝试时间
	LastAttemptResult string   `json:"lastAttemptResult,omitempty"` // success | rejected | failed | unchanged | insufficient_sample
	LastAttemptNote   string   `json:"lastAttemptNote,omitempty"`   // 上次尝试备注（拒绝/失败原因）
	History           []string `json:"history,omitempty"`           // 采纳前保留的旧提示词（最近 N 份，回滚用）
}

// personalityStateFile 落盘根结构。
type personalityStateFile struct {
	Entries map[string]personalityStateEntry `json:"entries"`
}

// personalityAuditEntry 一条演化审计（追加写 audit/personality-audit.jsonl）。
type personalityAuditEntry struct {
	At         string `json:"at"`
	ID         string `json:"id"`
	Trigger    string `json:"trigger"`    // message_count | time_elapsed | compaction | manual
	Result     string `json:"result"`     // success | rejected | failed | unchanged | insufficient_sample
	OldLen     int    `json:"oldLen"`     // 演化前提示词长度
	NewLen     int    `json:"newLen"`     // 演化后长度（未采纳时=oldLen）
	Evolutions int    `json:"evolutions"` // 采纳后的累计演化次数
	Note       string `json:"note,omitempty"`
}

// evolveResult 一次演化尝试的结构化结果（用于审计与上层分支）。
type evolveResult struct {
	Personality Personality
	Status      string // adopted | rejected | failed | unchanged
	Note        string
}

// ── 状态加载 / 持久化 ─────────────────────────────────────────────────────

// loadPersonalityState 启动时读取 config/personality-state.json；文件缺失/损坏则空态，不致命。
func (a *App) loadPersonalityState() {
	a.personalityState.Entries = map[string]personalityStateEntry{}
	if b, err := os.ReadFile(PersonalityStatePath(a.dataPath)); err == nil {
		_ = json.Unmarshal(b, &a.personalityState)
	}
	if a.personalityState.Entries == nil {
		a.personalityState.Entries = map[string]personalityStateEntry{}
	}
}

// persistPersonalityStateLocked 原子写回状态文件。调用方需持 a.mu。
func (a *App) persistPersonalityStateLocked() {
	_ = atomicJSON(PersonalityStatePath(a.dataPath), &a.personalityState)
}

// personalityStateLocked 读取某人格状态（无记录返回零值）。调用方需持 a.mu。
func (a *App) personalityStateLocked(id string) personalityStateEntry {
	if e, ok := a.personalityState.Entries[id]; ok {
		return e
	}
	return personalityStateEntry{}
}

// ── 触发计数（持久化）───────────────────────────────────────────────────────

// onPersonalityInteractLocked 记录一次有效交互：递增计数并判断是否达到触发阈值。
// 命中任一维度即重置计数、返回触发原因；否则返回 false。调用方需持 a.mu。
//
// 触发维度：
//   - message_count：aide≥20 条用户消息 / 小秘≥15 次有效交互
//   - time_elapsed：距上次演化 >72h 且累计交互 ≥5（兜底，防长期不演化）
func (a *App) onPersonalityInteractLocked(id string) (fire bool, trigger string) {
	st := a.personalityStateLocked(id)
	st.CountSinceEvolve++
	threshold := personalityAideMsgThreshold
	if id == personaXiaomi {
		threshold = personalityXiaomiIntThreshold
	}
	switch {
	case st.CountSinceEvolve >= threshold:
		st.CountSinceEvolve = 0
		trigger = "message_count"
		fire = true
	case hoursSince(st.LastEvolvedAt) >= personalityEvolveAfterHours && st.CountSinceEvolve >= personalityStableMinInteract:
		st.CountSinceEvolve = 0
		trigger = "time_elapsed"
		fire = true
	}
	a.personalityState.Entries[id] = st
	a.persistPersonalityStateLocked()
	return fire, trigger
}

// ── 核心人格一致性校验（包含性 checklist）───────────────────────────────────

// personalityCoreAnchors 某人格的身份锚点集合：演化后至少命中一个，否则判定核心丢失。
// 小秘锚点含默认"小秘"与用户自定义名；aide 锚点含自身身份词。
func (a *App) personalityCoreAnchors(id string) []string {
	if id == personaXiaomi {
		anchors := []string{"小秘"}
		if n := strings.TrimSpace(a.settings.VoiceAssistantName); n != "" && n != "小秘" {
			anchors = append(anchors, n)
		}
		return anchors
	}
	return []string{"aide"}
}

// personalityCoreOK 校验新提示词仍保留身份锚点（大小写不敏感包含性校验）。
func (a *App) personalityCoreOK(id, got string) bool {
	low := strings.ToLower(got)
	for _, anchor := range a.personalityCoreAnchors(id) {
		if strings.Contains(low, strings.ToLower(anchor)) {
			return true
		}
	}
	return false
}

// ── 相似度（字符二元组 Jaccard）────────────────────────────────────────────

func charBigrams(s string) map[string]struct{} {
	r := []rune(s)
	m := make(map[string]struct{}, len(r))
	for i := 0; i+1 < len(r); i++ {
		m[string(r[i:i+2])] = struct{}{}
	}
	return m
}

// jaccardSim 两个字符串的字符二元组 Jaccard 相似度 [0,1]。
func jaccardSim(a, b string) float64 {
	ba, bb := charBigrams(a), charBigrams(b)
	if len(ba) == 0 && len(bb) == 0 {
		return 1
	}
	inter := 0
	for k := range ba {
		if _, ok := bb[k]; ok {
			inter++
		}
	}
	union := len(ba) + len(bb) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// countSampleLines 统计样本中有效（非空）条数。
func countSampleLines(sample string) int {
	n := 0
	for _, line := range strings.Split(sample, "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

// hoursSince 解析 RFC3339Nano 时间，返回距今天数；空串/解析失败返回 0。
func hoursSince(iso string) float64 {
	if iso == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339Nano, iso)
	if err != nil {
		return 0
	}
	return time.Since(t).Hours()
}

// appendHistory 把旧提示词压入回滚栈，超过上限丢弃最旧。
func appendHistory(hist []string, old string) []string {
	old = strings.TrimSpace(old)
	if old == "" {
		return hist
	}
	hist = append(hist, old)
	if len(hist) > personalityHistoryLimit {
		hist = hist[len(hist)-personalityHistoryLimit:]
	}
	return hist
}

// ── 演化执行（核心决策）─────────────────────────────────────────────────────

// evolvePersonality 让模型重写性格提示词，并按模式做采纳决策。
//   - modeRefine：精炼 + 依稳定偏好微调；要求 ≤1.1× 旧长、≤4000 字、核心保留、相似度不过高。
//   - modeCompress：压缩事件专用；要求严格更短（只减不增）。
func (a *App) evolvePersonality(id string, cur Personality, cfg Settings, sample string, mode personalityMode) evolveResult {
	old := strings.TrimSpace(cur.Prompt)
	if old == "" {
		old = defaultPersonalityPrompt(id)
	}
	sys := `你在帮助一个 AI 助手精炼它自己的"性格提示词"。只输出重写后的提示词正文，不要解释、标题、引号或 markdown 围栏。`

	var req string
	if mode == modeCompress {
		req = fmt.Sprintf(`当前性格提示词（%d 字符）：
"""
%s
"""

近期对话样本（仅参考，不要写入新要求）：
"""
%s
"""

要求（本次为压缩整理，只减不增）：
1. 完整保留核心人格、身份、语气与原则。
2. 删除重复、空话、可由系统其它部分提供的内容；合并近义条目；用更短的句子。
3. 新提示词字符数必须严格少于当前，不要新增任何要求。
4. 直接输出新提示词正文。`, len(old), old, sample)
	} else {
		req = fmt.Sprintf(`当前性格提示词（%d 字符）：
"""
%s
"""

近期真实对话样本（仅用于校准风格、提炼用户稳定且反复出现的偏好；不要写入一次性琐事）：
"""
%s
"""

要求：
1. 完整保留核心人格、身份、语气与原则，以及用户明确且反复出现的偏好。
2. 精炼：删除重复、空话；合并近义条目；用更短的句子。
3. 可据样本中反复出现的稳定偏好微调措辞与侧重；允许少量调整，但新提示词不得超过当前长度的 %.2f 倍，且绝不超过 %d 字符。
4. 直接输出新提示词正文。`, len(old), old, sample, personalityGrowRatio, personalityMaxPromptChars)
	}

	params := ProfileParams{Temperature: fp(0.2), MaxTokens: 2048}
	out, _, _, err := complete(context.Background(), cfg, []Message{
		{Role: "system", Content: sys},
		{Role: "user", Content: req},
	}, params, nil, nil)
	if err != nil {
		return evolveResult{Personality: cur, Status: "failed", Note: "演化调用失败: " + err.Error()}
	}
	got := strings.TrimSpace(out)
	got = strings.TrimPrefix(got, "```")
	got = strings.TrimSuffix(got, "```")
	got = strings.Trim(got, "\""+"\n")
	got = strings.TrimSpace(got)
	if got == "" {
		return evolveResult{Personality: cur, Status: "rejected", Note: "演化结果为空"}
	}
	// 硬上限：防长期膨胀
	if len(got) > personalityMaxPromptChars {
		return evolveResult{Personality: cur, Status: "rejected", Note: fmt.Sprintf("新长度 %d 超过硬上限 %d", len(got), personalityMaxPromptChars)}
	}
	// 核心人格一致性：身份锚点不得丢失
	if !a.personalityCoreOK(id, got) {
		return evolveResult{Personality: cur, Status: "rejected", Note: "核心身份锚点丢失，拒绝采纳"}
	}
	// 相似度太高：无实质变化，不采纳也不计数
	if jaccardSim(old, got) >= personalitySimilarityCutoff {
		return evolveResult{Personality: cur, Status: "unchanged", Note: "与旧版几乎相同，无实质变化"}
	}
	// 长度约束按模式区分
	if mode == modeCompress {
		if len(got) >= len(old) {
			return evolveResult{Personality: cur, Status: "rejected", Note: "压缩模式要求严格更短"}
		}
	} else if float64(len(got)) > float64(len(old))*personalityGrowRatio {
		return evolveResult{Personality: cur, Status: "rejected", Note: fmt.Sprintf("变长 %.2f× 超过 %.2f× 上限", float64(len(got))/float64(len(old)), personalityGrowRatio)}
	}
	// 采纳
	cur.Prompt = got
	cur.Evolutions++
	cur.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	return evolveResult{Personality: cur, Status: "adopted", Note: "采纳"}
}

// ── 后台自动演化编排（取样 → 决策 → 采纳/拒绝 → 审计）───────────────────────

// runAutoEvolve 后台执行一次性格演化，不阻塞响应。失败/拒绝/无变化/样本不足均写审计。
func (a *App) runAutoEvolve(id string, mode personalityMode, trigger string, sample string) {
	defer func() { _ = recover() }()
	a.mu.Lock()
	cur := a.personalityLocked(id)
	if !cur.Enabled {
		a.mu.Unlock()
		return
	}
	if sample == "" {
		sample = a.personalitySampleLocked(id)
	}
	cfg := a.settings
	a.mu.Unlock()

	// 统一取样：样本不足则安全跳过，不强行演化
	if countSampleLines(sample) < personalityMinSampleMsgs {
		a.finishPersonalityAttempt(id, trigger, "insufficient_sample", cur, cur,
			fmt.Sprintf("有效样本 %d 条 < %d，跳过", countSampleLines(sample), personalityMinSampleMsgs))
		return
	}

	res := a.evolvePersonality(id, cur, cfg, sample, mode)
	switch res.Status {
	case "adopted":
		a.mu.Lock()
		if a.settings.Personalities == nil {
			a.settings.Personalities = map[string]Personality{}
		}
		st := a.personalityStateLocked(id)
		st.History = appendHistory(st.History, strings.TrimSpace(cur.Prompt)) // 保留旧版用于回滚
		st.LastEvolvedAt = res.Personality.UpdatedAt
		a.settings.Personalities[id] = res.Personality
		a.personalityState.Entries[id] = st
		a.persistPersonalityStateLocked()
		a.mu.Unlock()
		a.finishPersonalityAttempt(id, trigger, "success", cur, res.Personality, res.Note)
	case "unchanged":
		// 无实质变化：不更新、不计数，仅记录
		a.finishPersonalityAttempt(id, trigger, "unchanged", cur, cur, res.Note)
	default: // rejected | failed
		a.finishPersonalityAttempt(id, trigger, res.Status, cur, cur, res.Note)
	}
}

// finishPersonalityAttempt 记录一次尝试：更新状态文件 + 追加审计 jsonl + log.Printf。
func (a *App) finishPersonalityAttempt(id, trigger, result string, old, new Personality, note string) {
	a.mu.Lock()
	st := a.personalityStateLocked(id)
	st.LastAttemptAt = time.Now().UTC().Format(time.RFC3339Nano)
	st.LastAttemptResult = result
	st.LastAttemptNote = note
	a.personalityState.Entries[id] = st
	a.persistPersonalityStateLocked()
	a.mu.Unlock()

	entry := personalityAuditEntry{
		At:         time.Now().UTC().Format(time.RFC3339Nano),
		ID:         id,
		Trigger:    trigger,
		Result:     result,
		OldLen:     len(strings.TrimSpace(old.Prompt)),
		NewLen:     len(strings.TrimSpace(new.Prompt)),
		Evolutions: new.Evolutions,
		Note:       note,
	}
	if b, err := json.Marshal(entry); err == nil {
		if f, err := os.OpenFile(PersonalityAuditPath(a.dataPath), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600); err == nil {
			_, _ = f.Write(append(b, '\n'))
			_ = f.Close()
		}
	}
	log.Printf("personality-evolve[%s] trigger=%s result=%s oldLen=%d newLen=%d evolutions=%d note=%s",
		id, trigger, result, entry.OldLen, entry.NewLen, entry.Evolutions, note)
}

// ── 回滚 ───────────────────────────────────────────────────────────────────

// personalityRollbackLocked 恢复上一版性格（弹出回滚栈顶）。无历史返回 false。调用方需持 a.mu。
func (a *App) personalityRollbackLocked(id string) (Personality, bool) {
	st := a.personalityStateLocked(id)
	if len(st.History) == 0 {
		return a.personalityLocked(id), false
	}
	prev := st.History[len(st.History)-1]
	st.History = st.History[:len(st.History)-1]
	cur := a.personalityLocked(id)
	cur.Prompt = prev
	cur.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if a.settings.Personalities == nil {
		a.settings.Personalities = map[string]Personality{}
	}
	a.settings.Personalities[id] = cur
	a.personalityState.Entries[id] = st
	a.persistPersonalitiesLocked()
	a.persistPersonalityStateLocked()
	return cur, true
}

// personalityResetLocked 重置为默认并清空回滚历史。调用方需持 a.mu。
func (a *App) personalityResetLocked(id string) {
	if a.settings.Personalities != nil {
		delete(a.settings.Personalities, id)
	}
	st := a.personalityStateLocked(id)
	st.History = nil
	st.CountSinceEvolve = 0
	a.personalityState.Entries[id] = st
	a.persistPersonalitiesLocked()
	a.persistPersonalityStateLocked()
}

// errPersonalityNoHistory 无历史可回滚。
var errPersonalityNoHistory = errors.New("没有可回滚的历史版本")
