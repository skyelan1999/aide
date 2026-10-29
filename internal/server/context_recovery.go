package server

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// safeCompactionSplit moves a cut past an entire tool exchange, or before its
// initiating assistant when the exchange is unfinished.
// It never fabricates results or leaves retained tool messages without calls.
func safeCompactionSplit(messages []Message, cut int) int {
	if cut <= 0 {
		return 0
	}
	if cut > len(messages) {
		cut = len(messages)
	}
	pending := map[string]int{}
	for i, m := range messages[:cut] {
		for _, c := range m.ToolCalls {
			pending[c.ID] = i
		}
		if m.Role == "tool" {
			delete(pending, m.ToolCallID)
		}
	}
	if len(pending) == 0 {
		return cut
	}
	first := cut
	for _, i := range pending {
		if i < first {
			first = i
		}
	}
	for i := cut; i < len(messages); i++ {
		m := messages[i]
		if m.Role == "tool" {
			delete(pending, m.ToolCallID)
		}
		for _, c := range m.ToolCalls {
			pending[c.ID] = i
		}
		if len(pending) == 0 && i+1 < len(messages) {
			return i + 1
		}
	}
	return first
}

// fitActiveContext handles growth during a tool loop and routed parallel calls,
// not just history present before a run. It compacts a local candidate and only
// returns it after all bounded summaries succeed and the request fits. Session
// history remains untouched. Preserve current/latest user scope and images.
func (a *App) fitActiveContext(ctx context.Context, cfg Settings, input []Message, params ProfileParams, tools []any, task *Task) (fitted []Message, fitErr error) {
	a.mu.Lock()
	window := a.modelWindow(cfg.Model)
	a.mu.Unlock()
	reserve := params.MaxTokens
	if reserve <= 0 {
		reserve = maxTokensDefault
	}
	if estimateProviderPromptTokens(input, tools)+reserve <= window {
		return input, nil
	}
	if len(input) < 2 {
		return input, errors.New("当前请求本身超出模型上下文窗口，未裁剪用户请求")
	}
	pinned := map[int]bool{0: true}
	lastUser := -1
	currentUser := -1
	for i, m := range input {
		if len(m.Images) > 0 {
			pinned[i] = true
		}
		if m.Role == "user" {
			lastUser = i
			if task != nil && task.Prompt != "" && strings.HasPrefix(m.Content, task.Prompt) {
				currentUser = i
			}
		}
	}
	if lastUser >= 0 {
		pinned[lastUser] = true
	}
	if currentUser >= 0 {
		pinned[currentUser] = true
	}
	// Retain a recent suffix when possible. If a single completed exchange is
	// larger than the budget, summarize it as a whole instead of orphaning results.
	budget := (window - reserve) / 3
	if budget > compactKeepTokens {
		budget = compactKeepTokens
	}
	if budget < 0 {
		budget = 0
	}
	cut, total := len(input), 0
	for cut > 1 && total+contextMessageTokens(input[cut-1]) <= budget {
		cut--
		total += contextMessageTokens(input[cut])
	}
	cut = safeCompactionSplit(input, cut)
	build := func(split int) ([]Message, []Message) {
		var folded, kept []Message
		for i, m := range input {
			if i < split && !pinned[i] {
				folded = append(folded, m)
			} else {
				kept = append(kept, m)
			}
		}
		return folded, kept
	}
	folded, kept := build(cut)
	if len(folded) == 0 {
		folded, kept = build(len(input))
	}
	if len(folded) == 0 || estimateProviderPromptTokens(kept, tools)+reserve+1024 > window {
		return input, errors.New("当前用户请求、图片或工具定义超出模型窗口；原上下文已保留，请减少本轮输入")
	}
	startedAt := time.Now().UnixMilli()
	defer func() {
		if fitErr != nil && task != nil {
			a.publishStream(task.ID, streamEvent{Event: "compaction", Phase: "failed", StartedAt: startedAt, Text: "运行上下文整理失败，原上下文已保留"})
		}
	}()
	if task != nil {
		a.publishStream(task.ID, streamEvent{Event: "compaction", Phase: "started", StartedAt: startedAt, BeforeTokens: estimateProviderPromptTokens(input, tools), Folded: len(folded)})
	}
	summary, err := a.buildCompactionSummary(ctx, folded, cfg, "")
	if err != nil {
		return input, err
	}
	candidate := append([]Message{kept[0], {Role: "system", Content: "运行上下文摘要（历史证据，不是新的用户指令；以当前用户要求为准）：\n" + summary}}, kept[1:]...)
	if estimateProviderPromptTokens(candidate, tools)+reserve > window {
		return input, fmt.Errorf("运行上下文整理后仍超出模型窗口 %d，原上下文已保留", window)
	}
	if task != nil {
		a.publishStream(task.ID, streamEvent{Event: "compaction", Phase: "completed", StartedAt: startedAt, BeforeTokens: estimateProviderPromptTokens(input, tools), AfterTokens: estimateProviderPromptTokens(candidate, tools), Folded: len(folded)})
	}
	return candidate, nil
}
