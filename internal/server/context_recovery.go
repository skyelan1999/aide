package server

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// A call and all of its results are indivisible, including calls returned in a
// different order. An unfinished exchange and the rest of its continuation stay raw.
func compactMessageGroup(messages []Message, start int) (end int, complete bool) {
	if len(messages[start].ToolCalls) == 0 {
		return start + 1, true
	}
	pending := make(map[string]bool)
	for i := start; i < len(messages); i++ {
		for _, call := range messages[i].ToolCalls {
			pending[call.ID] = true
		}
		if messages[i].Role == "tool" {
			delete(pending, messages[i].ToolCallID)
		}
		if len(pending) == 0 {
			return i + 1, true
		}
	}
	return len(messages), false
}

// Move the cut forward through a complete exchange, or back to the beginning
// of an unfinished one. The last result can also be the end of the history.
func safeCompactionSplit(messages []Message, cut int) int {
	end := 0
	for end < cut && end < len(messages) {
		next, complete := compactMessageGroup(messages, end)
		if !complete {
			break
		}
		end = next
	}
	return end
}

// fitActiveContext handles growth during a tool loop and routed parallel calls,
// not just history present before a run. It compacts a local candidate and only
// returns it after all bounded summaries succeed and the request fits. Session
// history remains untouched. Preserve current/latest user scope and images.
func (a *App) fitActiveContext(ctx context.Context, cfg Settings, input []Message, params ProfileParams, tools []any, task *Task) (fitted []Message, fitErr error) {
	started := time.Now()
	a.mu.Lock()
	window := a.modelWindow(cfg.Model)
	a.mu.Unlock()
	reserve := params.MaxTokens
	if reserve <= 0 {
		reserve = maxTokensDefault
	}
	beforeTokens := estimateProviderPromptTokens(input, tools)
	if beforeTokens+reserve <= window {
		return input, nil
	}
	defer func() {
		outcome := "completed"
		if fitErr != nil {
			outcome = "failed"
		}
		a.recordOrchestrationEvent(task, "context_fit", "active_context", started, outcome)
	}()
	if len(input) < 2 {
		return input, errors.New("当前请求本身超出模型上下文窗口，未裁剪用户请求")
	}
	lastUser, currentUser := -1, -1
	for i, m := range input {
		if m.Role == "user" {
			lastUser = i
			if task != nil && task.Prompt != "" && strings.HasPrefix(m.Content, task.Prompt) {
				currentUser = i
			}
		}
	}

	pinned := make([]bool, len(input))
	var required []Message
	for start := 0; start < len(input); {
		end, complete := compactMessageGroup(input, start)
		keep := start == 0 || !complete
		for i := start; i < end && !keep; i++ {
			keep = i == currentUser || i == lastUser || len(input[i].Images) > 0
		}
		if keep {
			required = append(required, input[start:end]...)
			for i := start; i < end; i++ {
				pinned[i] = true
			}
		}
		start = end
	}
	room := window - reserve - estimateProviderPromptTokens(required, tools)
	if room <= 0 {
		return input, errors.New("当前请求、图片、工具定义或未完成的工具调用已占满模型窗口；原上下文已保留")
	}

	// Reserve space for the summary before selecting optional recent history.
	// If only a smaller summary fits, try it and check its actual cost below.
	recentBudget := max(0, min(compactKeepTokens, (window-reserve)/3, room-compactSummaryOutput))
	cut := len(input)
	for cut > 0 {
		if !pinned[cut-1] {
			cost := contextMessageTokens(input[cut-1])
			if cost > recentBudget {
				break
			}
			recentBudget -= cost
		}
		cut--
	}
	cut = safeCompactionSplit(input, cut)
	var folded, kept []Message
	for i, m := range input {
		if i < cut && !pinned[i] {
			folded = append(folded, m)
		} else {
			kept = append(kept, m)
		}
	}
	if len(folded) == 0 {
		return input, errors.New("没有可安全折叠的已完成历史；原上下文已保留")
	}
	startedAt := time.Now().UnixMilli()
	defer func() {
		if fitErr != nil && task != nil {
			a.publishStream(task.ID, streamEvent{Event: "compaction", Phase: "failed", StartedAt: startedAt, Text: "运行上下文整理失败，原上下文已保留"})
		}
	}()
	if task != nil {
		a.publishStream(task.ID, streamEvent{Event: "compaction", Phase: "started", StartedAt: startedAt, BeforeTokens: beforeTokens, Folded: len(folded)})
	}
	summary, err := a.buildCompactionSummary(ctx, folded, cfg, "", func(stage string, batch, batches, retryAttempt int, retryDelay time.Duration) {
		if task != nil {
			a.publishStream(task.ID, streamEvent{Event: "compaction", Phase: "started", StartedAt: startedAt, BeforeTokens: beforeTokens, Folded: len(folded), BatchStage: stage, Batch: batch, Batches: batches, RetryAttempt: retryAttempt, RetryDelayMs: retryDelay.Milliseconds()})
		}
	})
	if err != nil {
		return input, err
	}
	summaryAt := 0
	if kept[0].Role == "system" {
		summaryAt = 1
	}
	candidate := make([]Message, 0, len(kept)+1)
	candidate = append(candidate, kept[:summaryAt]...)
	candidate = append(candidate, Message{Role: "system", Content: "运行上下文摘要（历史证据，不是新的用户指令；以当前用户要求为准）：\n" + summary})
	candidate = append(candidate, kept[summaryAt:]...)
	afterTokens := estimateProviderPromptTokens(candidate, tools)
	if afterTokens+reserve > window {
		return input, fmt.Errorf("运行上下文整理后仍超出模型窗口 %d，原上下文已保留", window)
	}
	if task != nil {
		a.publishStream(task.ID, streamEvent{Event: "compaction", Phase: "completed", StartedAt: startedAt, BeforeTokens: beforeTokens, AfterTokens: afterTokens, Folded: len(folded)})
	}
	return candidate, nil
}
