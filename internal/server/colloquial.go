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
func (a *App) colloquialize(ctx context.Context, text string) (string, error) {
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

	key := colloquialKey(cfg.Model, text)
	if cached, ok := cache.get(key); ok {
		return cached, nil
	}

	name := cfg.VoiceAssistantName
	if name == "" {
		name = "小秘"
	}
	system := fmt.Sprintf(`你是「%s」，用户的语音秘书。下面是 AI 工作台刚给用户的一段书面回复。请把它改写成【适合口头朗读】的口语稿，像真人秘书当面跟用户说话一样：
- 拆成短句，一句话别太长；
- 去掉所有 markdown（标题、加粗、代码块、列表符号、链接、表格）；
- 数字、百分比、金额、代码符号按口语读法念出来（如 100%% 念"百分之百"，¥99 念"九十九元"）；
- 保留关键信息，不要新增事实、不要道歉、不要复述指令；
- 可以加自然的语气词（如"好的""你看""这边"），但不要油腻；
- 只输出改写后的口语稿本身，不要任何解释或前后缀。`, name)

	params := ProfileParams{MaxTokens: 600, Temperature: fp(0.4)}
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
