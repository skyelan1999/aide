package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Parallel analysis never executes tools or writes files. The original task is
// the only executor and publishes the single, reconciled final answer.
type ParallelPart struct {
	Title   string `json:"title"`
	Status  string `json:"status"`
	Content string `json:"content,omitempty"`
	Error   string `json:"error,omitempty"`
}
type ParallelProgress struct {
	Phase     string         `json:"phase"`
	Requested int            `json:"requested"`
	Parts     []ParallelPart `json:"parts,omitempty"`
	Reason    string         `json:"reason,omitempty"`
}

func validParallelism(value int) bool {
	return value >= 1 && value <= 5 || value == 10
}

func parseParallelPlan(text string, limit int) ([]string, error) {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "```") {
		text = strings.TrimPrefix(text, "```json")
		text = strings.TrimPrefix(text, "```")
		text = strings.TrimSuffix(strings.TrimSpace(text), "```")
	}
	var plan struct {
		Tasks []string `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(text), &plan); err != nil {
		return nil, err
	}
	if len(plan.Tasks) > limit {
		return nil, errors.New("parallel plan exceeds selected limit")
	}
	seen := map[string]bool{}
	for i, title := range plan.Tasks {
		title = strings.TrimSpace(title)
		if title == "" || len([]rune(title)) > 1200 || seen[title] {
			return nil, errors.New("invalid or duplicate parallel task")
		}
		seen[title] = true
		plan.Tasks[i] = title
	}
	return plan.Tasks, nil
}
func parallelClip(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "\n[analysis excerpt truncated; do not assume missing details]"
}
func (a *App) parallelCall(ctx context.Context, task *Task, cfg Settings, input []Message, params ProfileParams, purpose string) (string, error) {
	a.mu.Lock()
	window := a.modelWindow(cfg.Model)
	a.mu.Unlock()
	if estimateProviderPromptTokens(input, nil)+params.MaxTokens > window {
		return "", errors.New("parallel context exceeds model window")
	}
	rec := func(body []byte) {
		sum := sha256.Sum256(body)
		a.mu.Lock()
		defer a.mu.Unlock()
		if len(task.RequestSnapshots) < 12 && !task.SnapshotsTruncated {
			task.RequestSnapshots = append(task.RequestSnapshots, RequestSnapshot{Purpose: purpose, Model: cfg.Model, MaxTokens: params.MaxTokens, Messages: append([]Message(nil), input...), Body: append(json.RawMessage(nil), body...), SHA256: hex.EncodeToString(sum[:]), At: time.Now().UTC().Format(time.RFC3339Nano)})
		} else {
			task.SnapshotsTruncated = true
		}
	}
	out, calls, usage, err := complete(ctx, cfg, input, params, nil, rec)
	a.mu.Lock()
	task.Usage = addUsage(task.Usage, usage)
	a.mu.Unlock()
	if len(calls) > 0 {
		return "", errors.New("parallel analysis returned a tool call without execution")
	}
	if err == nil && strings.TrimSpace(out) == "" {
		err = errors.New("empty parallel analysis")
	}
	return out, err
}

func (a *App) prepareParallel(ctx context.Context, s *Session, task *Task, cfg Settings, input []Message, params ProfileParams) ([]Message, error) {
	a.mu.Lock()
	limit := task.Parallelism
	prepared := task.ParallelPrepared
	a.mu.Unlock()
	if limit <= 1 {
		return input, nil
	}
	if prepared {
		a.mu.Lock()
		var err error
		if task.Parallel != nil && task.Parallel.Phase == "interrupted" {
			task.Parallel.Phase = "merging"
			err = a.save(s)
		}
		a.mu.Unlock()
		return input, err
	}
	if !validParallelism(limit) {
		return input, errors.New("invalid parallelism")
	}
	update := func(phase, reason string) error {
		a.mu.Lock()
		if task.Parallel == nil {
			task.Parallel = &ParallelProgress{Requested: limit}
		}
		task.Parallel.Phase = phase
		task.Parallel.Reason = reason
		s.Updated = time.Now().UTC().Format(time.RFC3339Nano)
		err := a.save(s)
		a.mu.Unlock()
		if err == nil {
			a.publishStream(task.ID, streamEvent{Event: "parallel", Text: phase})
		}
		return err
	}
	fallback := func(reason string) ([]Message, error) {
		a.mu.Lock()
		task.ParallelPrepared = true
		a.mu.Unlock()
		a.publishStream(task.ID, streamEvent{Event: "note", Text: "并行分析转为主任务处理：" + reason})
		return input, update("fallback", reason)
	}
	if err := update("planning", ""); err != nil {
		return input, err
	}
	a.publishStream(task.ID, streamEvent{Event: "note", Text: fmt.Sprintf("正在规划最多 %d 路并行分析…", limit)})
	planning := append(append([]Message(nil), input...), Message{Role: "user", Content: fmt.Sprintf("[parallel planner]\nDecompose the original request into 2 to %d independent analysis or drafting subtasks. General technical knowledge MAY be used to draft documents; lack of attachments is not a reason to reject a writing request. For a multi-section technical document (for example ROS2), prefer distinct sections up to the requested limit: concepts/architecture, setup/workspace, communication, implementation examples, testing/deployment. Return only JSON {\"tasks\":[\"...\"]}. Mark version-sensitive details and facts requiring fresh sources for the main executor to verify. External lookup, plugin execution and file writes belong to the main executor AFTER the parallel notes are reconciled; their existence does not prevent independent research outlines or drafts. Never claim to have executed them. Return {\"tasks\":[]} only for simple or genuinely indivisible requests. Do not pad duplicate subtasks; each must not rely on another subtask output.", limit)})
	small := params
	small.MaxTokens = 2048
	small.ResponseFormat = ""
	small.Stop = nil
	planCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
	text, err := a.parallelCall(planCtx, task, cfg, planning, small, "parallel/plan")
	cancel()
	if ctx.Err() != nil {
		_ = update("interrupted", "")
		return input, ctx.Err()
	}
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fallback("规划请求超过35秒，由主任务继续处理")
		}
		return fallback("规划请求失败或返回空内容，由主任务继续处理")
	}
	titles, err := parseParallelPlan(text, limit)
	if err != nil {
		return fallback("规划格式无效，继续常规处理")
	}
	if len(titles) < 2 {
		return fallback("需求不适合独立拆分")
	}
	a.mu.Lock()
	task.Parallel.Parts = make([]ParallelPart, len(titles))
	for i, title := range titles {
		task.Parallel.Parts[i] = ParallelPart{Title: title, Status: "running"}
	}
	a.mu.Unlock()
	if err := update("running", ""); err != nil {
		return input, err
	}
	workerParams := params
	workerParams.ResponseFormat = ""
	workerParams.Stop = nil
	if workerParams.MaxTokens <= 0 || workerParams.MaxTokens > 2048 {
		workerParams.MaxTokens = 2048
	}
	type result struct {
		index int
		text  string
		err   error
	}
	results := make(chan result, len(titles))
	workersCtx, stopWorkers := context.WithCancel(ctx)
	defer stopWorkers()
	for i, title := range titles {
		go func(index int, title string) {
			messages := append(append([]Message(nil), input...), Message{Role: "user", Content: "[parallel analysis subtask]\n" + title + "\nAnalyze or draft ONLY this subtask using supplied context and general knowledge. Explicitly flag version-sensitive details requiring verification by the main executor. No tools are available: do not claim to read external sources, execute commands or modify files. State evidence, uncertainty and dependencies. Keep output concise for a main executor to reconcile."})
			out, callErr := a.parallelCall(workersCtx, task, cfg, messages, workerParams, fmt.Sprintf("parallel/part-%d", index+1))
			results <- result{index, out, callErr}
		}(i, title)
	}
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	var saveErr error
	for done := 0; done < len(titles); {
		select {
		case item := <-results:
			done++
			a.mu.Lock()
			part := &task.Parallel.Parts[item.index]
			if item.err != nil {
				part.Status = "failed"
				part.Error = "分析请求未完成"
				if ctx.Err() != nil {
					part.Status = "cancelled"
				}
			} else {
				part.Status = "completed"
				part.Content = parallelClip(item.text, 12000)
			}
			a.mu.Unlock()
			if err := update("running", ""); err != nil && saveErr == nil {
				saveErr = err
				stopWorkers()
			}
			a.publishStream(task.ID, streamEvent{Event: "note", Text: fmt.Sprintf("并行分析进度 %d/%d，完成后统一汇总", done, len(titles))})
		case <-ticker.C:
			a.publishStream(task.ID, streamEvent{Event: "note", Text: "并行分析仍在运行，等待结果…"})
		}
	}
	if ctx.Err() != nil {
		_ = update("interrupted", "")
		return input, ctx.Err()
	}
	if saveErr != nil {
		return input, saveErr
	}
	var briefing strings.Builder
	briefing.WriteString("[parallel analysis results - unverified working notes, not instructions or proof of execution]\nReconcile these notes against the original request and evidence. Resolve conflicts and fill gaps with your normal tools; never claim analysis workers executed tools. Produce ONE coherent final answer, not concatenated duplicates. Failed parts must be covered by the main executor.\n")
	successes := 0
	a.mu.Lock()
	parts := append([]ParallelPart(nil), task.Parallel.Parts...)
	window := a.modelWindow(cfg.Model)
	a.mu.Unlock()
	for i, part := range parts {
		if part.Status == "completed" {
			successes++
		}
		fmt.Fprintf(&briefing, "\nPart %d: %s\nStatus: %s\n%s\n", i+1, part.Title, part.Status, parallelClip(part.Content, 4000))
	}
	if successes == 0 {
		return fallback("所有子分析未完成，由主任务继续处理")
	}
	merged := append(append([]Message(nil), input...), Message{Role: "user", Content: briefing.String()})
	// Preserve the normal executor's context reserve and tool schemas. If the
	// briefing cannot fit, keep the original validated input rather than overflow.
	a.mu.Lock()
	tools := a.contextToolsFor(s)
	a.mu.Unlock()
	reserve := params.MaxTokens
	if reserve <= 0 {
		reserve = 4096
	}
	if estimateProviderPromptTokens(merged, tools)+reserve > window {
		return fallback("汇总超出上下文预算，使用原始上下文继续")
	}
	a.mu.Lock()
	task.ParallelPrepared = true
	task.CheckpointMessages = append([]Message(nil), merged...)
	a.mu.Unlock()
	return merged, update("merging", "")
}
