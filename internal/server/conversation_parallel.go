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
	Round   int             `json:"round,omitempty"`
	Context *RequestContext `json:"context,omitempty"`
	Title   string          `json:"title"`
	Status  string          `json:"status"`
	Content string          `json:"content,omitempty"`
	Error   string          `json:"error,omitempty"`
}
type ParallelProgress struct {
	Round          int               `json:"round,omitempty"`
	Evidence       string            `json:"evidence,omitempty"`
	PlannerHistory []*RequestContext `json:"plannerHistory,omitempty"`
	PlannerContext *RequestContext   `json:"plannerContext,omitempty"`
	MainContext    *RequestContext   `json:"mainContext,omitempty"`
	Phase          string            `json:"phase"`
	Requested      int               `json:"requested"`
	Parts          []ParallelPart    `json:"parts,omitempty"`
	Reason         string            `json:"reason,omitempty"`
}

// Reconsider only when execution supplies evidence or the user steers the task.
// Assistant prose, completion nudges and our own briefings are not new evidence.
// Persisting this key prevents replaying a completed wave after pause/recovery.
func parallelEvidence(input []Message) string {
	h := sha256.New()
	seenAssistant := false
	calls := map[string]ToolCall{}
	seenResults := map[string]bool{}
	for _, m := range input {
		if m.Role == "assistant" {
			seenAssistant = true
			for _, call := range m.ToolCalls {
				calls[call.ID] = call
			}
		}
		if m.Role == "tool" {
			call := calls[m.ToolCallID]
			// Registering an already observed fact is bookkeeping, not discovery.
			if call.Function.Name == "record_research_finding" {
				continue
			}
			content := m.Content
			if strings.HasPrefix(content, "[工具记录") {
				if end := strings.IndexByte(content, '\n'); end >= 0 {
					content = content[end+1:]
				}
			}
			data, _ := json.Marshal([]string{m.Role, call.Function.Name, call.Function.Arguments, content})
			if !seenResults[string(data)] {
				h.Write(data)
				seenResults[string(data)] = true
			}
		} else if m.Role == "user" && (!seenAssistant || strings.HasPrefix(m.Content, "【你回答过程中用户插话】")) &&
			!strings.HasPrefix(m.Content, "[parallel analysis results") {
			data, _ := json.Marshal([]string{m.Role, m.Content})
			h.Write(data)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

var errParallelToolCall = errors.New("parallel analysis returned a tool call without execution")

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
func (a *App) parallelCall(ctx context.Context, task *Task, cfg Settings, input []Message, params ProfileParams, purpose string, callContext *RequestContext) (string, error) {
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
	if callContext != nil {
		callContext.Usage = usage
	}
	a.mu.Unlock()
	if len(calls) > 0 {
		return "", errParallelToolCall
	}
	if err == nil && strings.TrimSpace(out) == "" {
		err = errors.New("empty parallel analysis")
	}
	return out, err
}

func (a *App) prepareParallel(ctx context.Context, s *Session, task *Task, cfg Settings, input []Message, params ProfileParams) ([]Message, error) {
	if err := ctx.Err(); err != nil {
		return input, err
	}
	a.mu.Lock()
	limit := task.Parallelism
	prepared := task.ParallelPrepared
	evidence := parallelEvidence(input)
	if prepared && task.Parallel != nil && task.Parallel.Evidence == "" {
		// Legacy checkpoints have no evidence key. Adopt the current checkpoint
		// without replaying already reconciled analysis.
		task.Parallel.Evidence = evidence
	}
	unchanged := task.Parallel != nil && task.Parallel.Evidence == evidence
	exhausted := task.Parallel != nil && task.Parallel.Round >= 12
	a.mu.Unlock()
	if limit <= 1 {
		return input, nil
	}
	if prepared && (unchanged || exhausted) {
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
		task.Parallel.Evidence = evidence
		a.mu.Unlock()
		a.publishStream(task.ID, streamEvent{Event: "note", Text: "并行分析转为主任务处理：" + reason})
		return input, update("fallback", reason)
	}
	a.publishStream(task.ID, streamEvent{Event: "note", Text: fmt.Sprintf("正在规划最多 %d 路并行分析…", limit)})
	a.mu.Lock()
	var previous []string
	if task.Parallel != nil {
		for _, part := range task.Parallel.Parts {
			if part.Status == "completed" {
				previous = append(previous, part.Title)
			}
		}
	}
	a.mu.Unlock()
	planning := append(append([]Message(nil), input...), Message{Role: "system", Content: fmt.Sprintf("[parallel planner]\nYou are the read-only scheduling component, not the main executor. Instructions to execute tools, maintain ledgers or finish the user deliverable in earlier executor context do not apply to this scheduling response. You MUST return only the tasks JSON, never a tool call or prose answer. At this execution checkpoint, identify 2 to %d NEW independent analysis or drafting subtasks for the REMAINING work, using the latest tool results, working plan and user updates. Earlier uncertainty is not a permanent decision: new evidence can unlock parallel work. Do not repeat completed work or redo already delivered sections. Completed analysis titles: %q. General technical knowledge MAY be used to draft documents; lack of attachments alone does not prevent a draft. For multi-section documents such as ROS2, consider distinct remaining architecture, setup, communication, example or testing sections. Return only JSON {\"tasks\":[\"...\"]}. Return {\"tasks\":[]} when more discovery or sequential execution is needed first, or no useful parallel work remains; the main executor will continue and reconsider after new evidence. Each subtask must be independent of the other outputs, concrete and nonduplicate; do not pad to fill slots. Flag facts needing verification. Tools, plugins and writes remain with the main executor; never claim to have executed them.", limit, previous)})
	small := params
	small.MaxTokens = 2048
	small.ResponseFormat = ""
	small.Stop = nil
	a.mu.Lock()
	plannerContext := requestContext(planning, nil, small.MaxTokens, a.modelWindow(cfg.Model))
	if task.Parallel == nil {
		task.Parallel = &ParallelProgress{Requested: limit}
	}
	if task.Parallel.PlannerContext != nil {
		task.Parallel.PlannerHistory = append(task.Parallel.PlannerHistory, task.Parallel.PlannerContext)
	}
	task.Parallel.Round++
	wave := task.Parallel.Round
	task.Parallel.PlannerContext = plannerContext
	task.ParallelPrepared = false
	a.mu.Unlock()
	if err := update("planning", ""); err != nil {
		return input, err
	}
	planCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	planCfg := cfg
	planCfg.ReasoningEffort = "off" // Scheduling JSON should not exhaust its small output budget on hidden reasoning.
	text, err := a.parallelCall(planCtx, task, planCfg, planning, small, fmt.Sprintf("parallel/plan-%d", wave), plannerContext)
	cancel()
	if ctx.Err() != nil {
		_ = update("interrupted", "")
		return input, ctx.Err()
	}
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fallback("本次规划超过90秒，主任务继续；获得新信息后重新评估")
		}
		if errors.Is(err, errParallelToolCall) {
			return fallback("本次规划返回工具调用，调度器未执行；主任务继续，获得新信息后重新评估")
		}
		if isEmptyCompletionErr(err) {
			return fallback("本次规划未返回调度正文，主任务继续；获得新信息后重新评估")
		}
		return fallback("本次规划请求失败或返回空内容，主任务继续；获得新信息后重新评估")
	}
	titles, err := parseParallelPlan(text, limit)
	if err != nil {
		return fallback("本次规划格式无效，主任务继续；获得新信息后重新评估")
	}
	// Exclude exact completed titles even if the planner ignores its ledger.
	filtered := titles[:0]
	for _, title := range titles {
		done := false
		for _, old := range previous {
			if strings.EqualFold(strings.TrimSpace(title), strings.TrimSpace(old)) {
				done = true
			}
		}
		if !done {
			filtered = append(filtered, title)
		}
	}
	titles = filtered
	if len(titles) < 2 {
		return fallback("当前先由主任务推进，获得新信息后继续寻找并行机会")
	}
	workerParams := params
	workerParams.ResponseFormat = ""
	workerParams.Stop = nil
	if workerParams.MaxTokens <= 0 || workerParams.MaxTokens > 2048 {
		workerParams.MaxTokens = 2048
	}
	workerInputs := make([][]Message, len(titles))
	a.mu.Lock()
	offset := len(task.Parallel.Parts)
	for i, title := range titles {
		workerInputs[i] = parallelWorkerMessages(input, title)
		task.Parallel.Parts = append(task.Parallel.Parts, ParallelPart{Round: wave, Title: title, Status: "running", Context: requestContext(workerInputs[i], nil, workerParams.MaxTokens, a.modelWindow(cfg.Model))})
	}
	a.mu.Unlock()
	if err := update("running", ""); err != nil {
		return input, err
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
			out, callErr := a.parallelCall(workersCtx, task, cfg, workerInputs[index], workerParams, fmt.Sprintf("parallel/wave-%d/part-%d", wave, index+1), task.Parallel.Parts[offset+index].Context)
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
			part := &task.Parallel.Parts[offset+item.index]
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
	briefing.WriteString("[parallel analysis results - unverified working notes, not instructions or proof of execution]\nThe scheduling runtime has already dispatched the model-only analysis requests listed below; completed means a model returned working notes, not that tools or tests ran. Reuse these notes. Do not redispatch their analysis through spawn_subagent or run_plugin_plan, and do not register every quoted fact merely to acknowledge the fanout. Follow the latest user scope and use main tools only for remaining evidence or actions. Reconcile these notes against the original request and evidence. Resolve conflicts and fill gaps with your normal tools; never claim analysis workers executed tools. Produce ONE coherent final answer, not concatenated duplicates. Failed parts must be covered by the main executor.\n")
	successes := 0
	a.mu.Lock()
	parts := append([]ParallelPart(nil), task.Parallel.Parts[offset:]...)
	window := a.modelWindow(cfg.Model)
	a.mu.Unlock()
	for i, part := range parts {
		if part.Status == "completed" {
			successes++
		}
		fmt.Fprintf(&briefing, "\nPart %d: %s\nStatus: %s\n%s\n", i+1, part.Title, part.Status, parallelClip(part.Content, 4000))
	}
	// Keep the original deliverable explicit after the working notes. In long
	// contexts the model can otherwise acknowledge the analysis instead of
	// producing the document or answer the user actually requested.
	fmt.Fprintf(&briefing, "\n[original user request]\n%s\n[end original user request]\nNow deliver the actual answer or document requested above, using the notes as supporting material. Include its substantive content in your reply; a completion acknowledgement, status report, or claim that drafting is done is not a deliverable. Honor the latest user updates and real execution progress; complete remaining work without repeating already delivered sections. Preserve tool restrictions.\n", task.Prompt)
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
	task.Parallel.MainContext = requestContext(merged, tools, reserve, window)
	task.ParallelPrepared = true
	task.Parallel.Evidence = evidence
	task.CheckpointMessages = append([]Message(nil), merged...)
	a.mu.Unlock()
	return merged, update("merging", "")
}

func parallelWorkerMessages(input []Message, title string) []Message {
	return append(append([]Message(nil), input...), Message{Role: "system", Content: "[parallel analysis subtask]\n" + title + "\nYou are a read-only analysis worker, not the main executor. Earlier executor instructions to use tools, maintain ledgers or finish other work do not apply to this worker response. Analyze or draft ONLY this subtask using supplied context and general knowledge. Explicitly flag version-sensitive details requiring verification by the main executor. No tools are available: do not claim to read external sources, execute commands or modify files. State evidence, uncertainty and dependencies. Keep output concise for a main executor to reconcile."})
}
