package server

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// 口语化预处理：小蜜对话回复（speakReply 路径）直接朗读模型原文会很生硬——
// 原文带 markdown、长句、数字符号、书面语。这里在朗读前用 LLM 把它改写成"说人话"的口语稿。
// 导览讲解路径（speakAwait）已由 voice-narrate 做过口语化，不重复处理。
//
// 缓存：hash(原文+模型) → 口语稿，内存 LRU，避免同一回复重复花 token。

const colloquialCacheMax = 256

type colloquialEntry struct {
	key       string
	text      string
	updatedAt time.Time
}

type colloquialCache struct {
	mu    sync.Mutex
	items map[string]*colloquialEntry
	order []string // 简单 LRU：尾部最新，头部最旧
}

func newColloquialCache() *colloquialCache {
	return &colloquialCache{items: map[string]*colloquialEntry{}}
}

func (c *colloquialCache) get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[key]
	if !ok {
		return "", false
	}
	// 命中即提到最新
	c.touchLocked(key)
	return e.text, true
}

func (c *colloquialCache) put(key, text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.items[key]; ok {
		c.touchLocked(key)
		c.items[key].text = text
		return
	}
	c.items[key] = &colloquialEntry{key: key, text: text, updatedAt: time.Now()}
	c.order = append(c.order, key)
	for len(c.order) > colloquialCacheMax {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.items, oldest)
	}
}

func (c *colloquialCache) touchLocked(key string) {
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			c.order = append(c.order, key)
			return
		}
	}
}

// colloquialize 把一段书面回复改写成适合朗读的口语稿。失败时返回原文与 error，
// 调用方应降级朗读原文（不阻断语音）。
// mode: "brief"=简要概括（抓结论/数字/决策，强保真）；"full"=完整朗读（只口语化不删减）。
func (a *App) colloquialize(ctx context.Context, text string, mode string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", errors.New("空文本")
	}
	// 已经很短的文本没必要改写
	if len([]rune(text)) <= 24 {
		return text, nil
	}
	a.mu.Lock()
	cfg := a.settings
	cache := a.colloqCache
	a.mu.Unlock()
	if cfg.BaseURL == "" || cfg.Model == "" || cache == nil {
		return text, nil // 未配置模型：直接读原文
	}
	full := mode == "full"

	key := colloquialKey(cfg.Model+"|"+mode, text)
	if cached, ok := cache.get(key); ok {
		return cached, nil
	}

	name := cfg.VoiceAssistantName
	if name == "" {
		name = "小秘"
	}
	systemBrief := fmt.Sprintf(`你是「%s」，用户的语音秘书。下面是 AI 工作台刚给用户的一段书面回复。请做【简要语音概括】，像真人秘书当面口头汇报一样：
- 先抓住【核心结论 + 关键数字/决策/下一步】，再用自然口语说出来；
- 强保真：只基于原文，不添加任何原文没有的事实，不改变结论，不道歉，不复述指令；
- 关键数字、百分比、金额、专有名词、决策结论必须保留，不得丢失或臆测；
- 拆成短句，去掉所有 markdown，数字按口语念（100%% 念"百分之百"）；
- 简短但要点齐全——宁可稍长也不要漏掉结论；
- 只输出概括稿本身，不要任何解释或前后缀。`, name)
	systemFull := fmt.Sprintf(`你是「%s」，用户的语音秘书。下面是 AI 工作台刚给用户的一段书面回复。请【完整口语化朗读稿】，不要删减任何内容：
- 把书面语转成自然口语，但保留原文全部要点，不概括、不省略；
- 拆成短句，去掉 markdown，数字按口语念；
- 长文请完整输出，不要中途停下；
- 只输出口语稿本身，不要解释或前后缀。`, name)
	system := systemBrief
	maxTok := 600
	if full {
		system = systemFull
		maxTok = 2000 // 完整朗读避免截断
	}

	params := ProfileParams{MaxTokens: maxTok, Temperature: fp(0.4)}
	out, _, _, err := complete(ctx, cfg, []Message{
		{Role: "system", Content: system},
		{Role: "user", Content: text},
	}, params, nil, nil)
	if err != nil {
		return text, err
	}
	out = strings.TrimSpace(out)
	out = strings.TrimPrefix(out, "```")
	out = strings.TrimSuffix(out, "```")
	out = strings.TrimSpace(out)
	if out == "" {
		return text, nil
	}
	cache.put(key, out)
	return out, nil
}

func colloquialKey(model, text string) string {
	h := sha1.Sum([]byte(model + "\x00" + text))
	return hex.EncodeToString(h[:])
}
