package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

const pluginPlanMaxNodes = 20

var pluginPlanTool = map[string]any{"type": "function", "function": map[string]any{
	"name":        "run_plugin_plan",
	"description": "Execute an explicit dependency graph of enabled plugin tools, at the task's selected 1x-5x concurrency. Use distinct node IDs and dependsOn; bindings maps an argument name to a direct dependency ID and supplies that dependency's returned text. Independent nodes may overlap; conflicting resources remain ordered. Control-action confirmations are serialized. Writes returned as proposals are NOT applied: their dependent nodes are skipped until a later verified task. Failures/unknown effects block dependents, not independent work. Results are merged in node order. Never retry unresolved interrupted effects automatically. Built-in tools and nested plans are not accepted as nodes.",
	"parameters": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"nodes"}, "properties": map[string]any{
		"nodes": map[string]any{"type": "array", "minItems": 1, "maxItems": pluginPlanMaxNodes, "items": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"id", "tool", "arguments"}, "properties": map[string]any{
			"id": map[string]any{"type": "string"}, "title": map[string]any{"type": "string"}, "tool": map[string]any{"type": "string"}, "arguments": map[string]any{"type": "object"},
			"dependsOn": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "bindings": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
		}}},
	}},
}}

type pluginPlanNodeSpec struct {
	ID        string            `json:"id"`
	Title     string            `json:"title,omitempty"`
	Tool      string            `json:"tool"`
	Arguments map[string]any    `json:"arguments"`
	DependsOn []string          `json:"dependsOn,omitempty"`
	Bindings  map[string]string `json:"bindings,omitempty"`
}

type PluginPlanNode struct {
	pluginPlanNodeSpec
	State        string              `json:"state"`
	Result       string              `json:"result,omitempty"`
	Error        string              `json:"error,omitempty"`
	Proposals    []map[string]any    `json:"proposals,omitempty"`
	MergeState   string              `json:"mergeState,omitempty"`
	EffectChecks []PluginEffectCheck `json:"effectChecks,omitempty"`
}

type PluginExecutionPlan struct {
	ID        string           `json:"id"`
	Digest    string           `json:"digest"`
	State     string           `json:"state"`
	Limit     int              `json:"limit"`
	Requested int              `json:"requested"`
	Reason    string           `json:"reason,omitempty"`
	Nodes     []PluginPlanNode `json:"nodes"`
}

func pluginPlanModelResult(p *PluginExecutionPlan) string {
	copy := *p
	copy.Nodes = append([]PluginPlanNode(nil), p.Nodes...)
	for i := range copy.Nodes {
		copy.Nodes[i].EffectChecks = append([]PluginEffectCheck(nil), p.Nodes[i].EffectChecks...)
		for j := range copy.Nodes[i].EffectChecks {
			copy.Nodes[i].EffectChecks[j].Evidence = ""
		}
	}
	b, _ := json.Marshal(copy)
	return string(b)
}

func pluginPlanID(id string) bool {
	if len(id) == 0 || len(id) > 64 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func parsePluginPlan(arguments string) ([]pluginPlanNodeSpec, error) {
	if len(arguments) > 512*1024 {
		return nil, errors.New("plugin plan exceeds argument limit")
	}
	var in struct {
		Nodes []pluginPlanNodeSpec `json:"nodes"`
	}
	d := json.NewDecoder(strings.NewReader(arguments))
	d.DisallowUnknownFields()
	if err := d.Decode(&in); err != nil {
		return nil, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return nil, errors.New("plugin plan must be one JSON object")
	}
	if len(in.Nodes) == 0 || len(in.Nodes) > pluginPlanMaxNodes {
		return nil, errors.New("plugin plan requires1-20 nodes")
	}
	index := map[string]int{}
	for i, n := range in.Nodes {
		if !pluginPlanID(n.ID) || n.Tool == "" || len(n.Title) > 1024 || n.Arguments == nil {
			return nil, errors.New("invalid plugin node")
		}
		if _, exists := index[n.ID]; exists {
			return nil, errors.New("duplicate plugin node ID")
		}
		index[n.ID] = i
	}
	for _, n := range in.Nodes {
		deps := map[string]bool{}
		for _, dep := range n.DependsOn {
			if _, exists := index[dep]; !exists || dep == n.ID || deps[dep] {
				return nil, errors.New("invalid plugin node dependency")
			}
			deps[dep] = true
		}
		for key, dep := range n.Bindings {
			if key == "" || len(key) > 128 || !deps[dep] {
				return nil, errors.New("bindings must name direct dependencies")
			}
		}
	}
	colors := make([]int, len(in.Nodes))
	var visit func(int) bool
	visit = func(i int) bool {
		if colors[i] != 0 {
			return colors[i] == 2
		}
		colors[i] = 1
		for _, dep := range in.Nodes[i].DependsOn {
			if !visit(index[dep]) {
				return false
			}
		}
		colors[i] = 2
		return true
	}
	for i := range in.Nodes {
		if !visit(i) {
			return nil, errors.New("plugin plan contains a cycle")
		}
	}
	return in.Nodes, nil
}

// Charge nested calls against the same central tool budget as ordinary calls.
func pluginPlanDispatchCost(call ToolCall) int {
	if call.Function.Name != "run_plugin_plan" {
		return 1
	}
	nodes, err := parsePluginPlan(call.Function.Arguments)
	if err != nil {
		return 1
	} // Invalid plans cannot dispatch any node.
	return 1 + len(nodes)
}

func recoverPluginPlans(task *Task) {
	for _, plan := range task.PluginPlans {
		if plan == nil || plan.State == "completed" {
			continue
		}
		plan.State = "interrupted"
		for i := range plan.Nodes {
			n := &plan.Nodes[i]
			if n.State == "dispatched" {
				n.State, n.Error = "uncertain", "Interrupted without durable result; verify external effects before retry."
			}
			if n.State == "waiting" {
				n.State, n.Error = "skipped", "Not dispatched before interruption."
			}
		}
	}
}

func (a *App) pluginPlanPolicy(task *Task, tool, owner, scope, workspace string, epoch uint64) error {
	if err := a.sandboxToolError(tool); err != nil {
		return err
	}
	a.mu.Lock()
	taskDenied := taskToolDenied(task, tool)
	a.mu.Unlock()
	if a.toolDenied(tool) || taskDenied {
		return errors.New("nested plugin tool is disabled")
	}
	for _, builtin := range builtinToolNames() {
		if tool == builtin {
			return errors.New("built-in tools cannot be plugin plan nodes")
		}
	}
	if owner == "" || a.pluginOwnerOf(tool) != owner {
		return errors.New("plan nodes require enabled plugin tools")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pluginsPath != scope || a.workPath != workspace || a.wsRevision != epoch || task.WorkspaceID != "" && task.WorkspaceID != a.wsID() {
		return errors.New("plugin plan workspace changed")
	}
	for _, p := range a.pluginRegistry.Plugins {
		if p.ID == owner && p.Enabled {
			return nil
		}
	}
	return errors.New("plan plugin is disabled")
}

type pluginPlanAdmissionKey struct{}

var errPluginPlanNotDispatched = errors.New("plugin node was not dispatched")

func pluginPlanRejected(ctx context.Context, err error) error {
	if _, ok := ctx.Value(pluginPlanAdmissionKey{}).(func() error); ok {
		return fmt.Errorf("%w: %w", errPluginPlanNotDispatched, err)
	}
	return err
}

func pluginPlanAdmission(ctx context.Context) error {
	if check, ok := ctx.Value(pluginPlanAdmissionKey{}).(func() error); ok {
		if err := check(); err != nil {
			return pluginPlanRejected(ctx, err)
		}
	}
	return nil
}

func pluginPlanArgumentsAllowed(args map[string]any) bool {
	// Keep the ordinary executeToolCall reference-source restriction. Graphs
	// cannot use a plugin alias to bypass the shared source/retrieval entry.
	source, _ := args["source"].(string)
	return strings.TrimSpace(source) == ""
}

// Only the controller mutates the plan, versions, proposals or approval slot.
// Workers only read guarded policy at admission and always finish before return.
func (a *App) executePluginPlan(parent context.Context, outer ToolCall, task *Task, versions map[string]Change) string {
	nodes, err := parsePluginPlan(outer.Function.Arguments)
	if err != nil {
		return "错误：插件任务图无效: " + err.Error()
	}
	if outer.ID == "" || len(outer.ID) > 256 {
		return "错误：插件任务图缺少有效调用标识"
	}
	sum := sha256.Sum256([]byte(outer.Function.Arguments))
	digest := hex.EncodeToString(sum[:])
	a.mu.Lock()
	encode := pluginPlanModelResult
	for _, prior := range task.PluginPlans {
		if prior == nil {
			continue
		}
		if prior.ID == outer.ID {
			if prior.Digest != digest {
				a.mu.Unlock()
				return "错误：任务图调用标识已用于不同参数"
			}
			out := encode(prior)
			a.mu.Unlock()
			return out // Never replay a persisted plan.
		}
		continued := pluginPlanContinued(task, prior)
		for _, n := range prior.Nodes {
			if (n.State == "dispatched" || n.State == "uncertain") && !continued {
				a.mu.Unlock()
				return "错误：前一插件任务图结果待核实，不自动重放或派发新图"
			}
		}
		if prior.State != "completed" && !continued {
			a.mu.Unlock()
			return "错误：已有未完成插件任务图"
		}
	}
	if len(task.PluginPlans) >= 8 {
		a.mu.Unlock()
		return "错误：本任务插件图记录已达上限，请基于已有结果总结"
	}
	limit := task.Parallelism
	if limit == 0 {
		limit = 1
	}
	requested := limit
	serialHooks := false
	for _, hook := range taskHarnessConfig(task).Hooks {
		if !hook.Enabled || hook.Event != "before_tool" && hook.Event != "after_tool" {
			continue
		}
		for _, n := range nodes {
			if hook.Tool == "*" || hook.Tool == n.Tool {
				serialHooks = true
			}
		}
	}
	scope, workspace, epoch := a.pluginsPath, a.workPath, a.wsRevision
	chain := append([]Message(nil), task.CheckpointMessages...)
	step := task.CheckpointStep
	if step == "" {
		step = "plugin-plan"
	}
	a.mu.Unlock()
	if !validParallelism(limit) {
		return "错误：无效并行倍率"
	}
	reason := ""
	if serialHooks && limit > 1 {
		limit = 1
		reason = "Tool hooks have no complete resource contract; execute hooks and plugin nodes serially."
	}
	owners := make([]string, len(nodes))
	for i, n := range nodes {
		owners[i] = a.pluginOwnerOf(n.Tool)
		if !pluginPlanArgumentsAllowed(n.Arguments) {
			return "错误：插件任务图不能绕过只读来源入口"
		}
		if err := a.pluginPlanPolicy(task, n.Tool, owners[i], scope, workspace, epoch); err != nil {
			return "错误：插件任务图未派发: " + err.Error()
		}
	}
	plan := &PluginExecutionPlan{ID: outer.ID, Digest: digest, State: "running", Limit: limit, Requested: requested, Reason: reason}
	index := map[string]int{}
	for i, n := range nodes {
		plan.Nodes = append(plan.Nodes, PluginPlanNode{pluginPlanNodeSpec: n, State: "waiting"})
		index[n.ID] = i
	}
	a.mu.Lock()
	// Preflight deliberately runs without mu. Reserve the task's graph slot
	// atomically afterwards as well, so concurrent callers cannot both dispatch.
	for _, prior := range task.PluginPlans {
		if prior == nil {
			continue
		}
		if prior.ID == outer.ID {
			out := encode(prior)
			if prior.Digest != digest {
				out = "错误：任务图调用标识已用于不同参数"
			}
			a.mu.Unlock()
			return out
		}
		continued := pluginPlanContinued(task, prior)
		if prior.State != "completed" && !continued {
			a.mu.Unlock()
			return "错误：已有未完成插件任务图"
		}
		for _, n := range prior.Nodes {
			if (n.State == "uncertain" || n.State == "dispatched") && !continued {
				a.mu.Unlock()
				return "错误：前一插件任务图结果待核实"
			}
		}
	}
	if len(task.PluginPlans) >= 8 {
		a.mu.Unlock()
		return "错误：本任务插件图记录已达上限"
	}
	task.PluginPlans = append(task.PluginPlans, plan)
	a.mu.Unlock()
	checkpoint := func(kind string, call *ToolCall, result string) error {
		return a.checkpointExecution(task, step, chain, kind, call, result)
	}
	if err := checkpoint("plugin_plan", nil, ""); err != nil {
		a.mu.Lock()
		plan.State = "interrupted"
		for i := range plan.Nodes {
			plan.Nodes[i].State = "skipped"
			plan.Nodes[i].Error = "Plan was not persisted; not dispatched."
		}
		a.mu.Unlock()
		return "错误：插件任务图未持久化，未派发: " + err.Error()
	}
	lines := []string{}
	for _, n := range nodes {
		line := n.ID + " · " + n.Tool
		if n.Title != "" {
			line += " · " + n.Title
		}
		if len(n.DependsOn) > 0 {
			line += " ← " + strings.Join(n.DependsOn, ", ")
		}
		lines = append(lines, line)
	}
	a.publishStream(task.ID, streamEvent{Event: "note", Text: fmt.Sprintf("插件任务图 · 最多 %dx\n%s", limit, strings.Join(lines, "\n"))})
	if reason != "" {
		a.publishStream(task.ID, streamEvent{Event: "note", Text: reason})
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	if a.bgCtx != nil {
		stop := context.AfterFunc(a.bgCtx, cancel)
		defer stop()
	}
	type response struct {
		index int
		raw   any
		err   error
	}
	results := make(chan response, len(nodes))
	active, terminal := 0, 0
	var fatal error
	set := func(i int, state, reason string) {
		a.mu.Lock()
		plan.Nodes[i].State, plan.Nodes[i].Error = state, reason
		a.mu.Unlock()
	}
	nodeCall := func(i int, args map[string]any) ToolCall {
		c := ToolCall{ID: outer.ID + "/" + nodes[i].ID}
		c.Function.Name = nodes[i].Tool
		b, _ := json.Marshal(args)
		c.Function.Arguments = string(b)
		return c
	}
	finish := func(r response) {
		i := r.index
		n := &plan.Nodes[i]
		state, text := "succeeded", ""
		var proposals []map[string]any
		if r.err != nil {
			state, text = "uncertain", "Plugin returned no reliable success receipt: "+r.err.Error()
			if errors.Is(r.err, errPluginPlanNotDispatched) {
				state, text = "skipped", "Not dispatched: "+r.err.Error()
			}
		} else {
			text, proposals = normalizePluginResult(r.raw)
			if len(proposals) > 0 {
				state = "proposed"
			}
		}
		a.mu.Lock()
		n.State, n.Result, n.Proposals = state, text, proposals
		if len(proposals) > 0 {
			n.MergeState = "pending"
		}
		a.mu.Unlock()
		kind := "tool_result"
		if state == "uncertain" {
			kind = "plugin_node_uncertain"
		}
		call := nodeCall(i, n.Arguments)
		if err := checkpoint(kind, &call, text); err != nil {
			fatal = err
			cancel()
		}
		a.publishStream(task.ID, streamEvent{Event: "tool", Tool: n.Tool, CallID: call.ID, Preview: parallelClip(text, 2000), OK: state == "succeeded" || state == "proposed"})
		if err := a.runHarnessHooks(ctx, task, step, chain, "after_tool", n.Tool); err != nil {
			fatal = err
			cancel()
		}
		active--
		terminal++
	}
	for terminal < len(nodes) {
		for i, spec := range nodes {
			if plan.Nodes[i].State != "waiting" {
				continue
			}
			if ctx.Err() != nil || fatal != nil {
				set(i, "skipped", "Plan interrupted before dispatch.")
				terminal++
				continue
			}
			ready, blocked := true, false
			for _, dep := range spec.DependsOn {
				state := plan.Nodes[index[dep]].State
				if state == "waiting" || state == "dispatched" {
					ready = false
				} else if state != "succeeded" {
					blocked = true
				}
			}
			if blocked {
				set(i, "skipped", "Dependency failed, is uncertain, or only proposed a write; verify/apply it before a later task.")
				terminal++
				continue
			}
			if !ready || active >= limit {
				continue
			}
			args := map[string]any{}
			for k, v := range spec.Arguments {
				args[k] = v
			}
			for k, dep := range spec.Bindings {
				args[k] = plan.Nodes[index[dep]].Result
			}
			if !pluginPlanArgumentsAllowed(args) {
				set(i, "skipped", "Reference sources must use the ordinary read-only entry.")
				terminal++
				continue
			}
			if err := a.recoveryOperationError(task, spec.Tool, args, plan); err != nil {
				set(i, "skipped", err.Error())
				terminal++
				continue
			}
			if err := a.pluginPlanPolicy(task, spec.Tool, owners[i], scope, workspace, epoch); err != nil {
				set(i, "skipped", err.Error())
				terminal++
				continue
			}
			if owners[i] == "browser-control" && browserToolRequiresConfirmation(spec.Tool) || owners[i] == "computer-control" && computerToolRequiresConfirmation(spec.Tool) {
				question, _ := json.Marshal(map[string]any{"question": pluginActionConfirmationText(owners[i], spec.Tool, args), "type": "confirm"})
				if a.awaitUserAnswer(ctx, task, question) != "确认" {
					set(i, "skipped", "User did not approve this control action.")
					terminal++
					continue
				}
				args["approved"] = true
			}
			if ctx.Err() != nil {
				set(i, "skipped", "Plan interrupted before dispatch.")
				terminal++
				continue
			}
			if err := a.runHarnessHooks(ctx, task, step, chain, "before_tool", spec.Tool); err != nil {
				fatal = err
				cancel()
				continue
			}
			if err := a.pluginPlanPolicy(task, spec.Tool, owners[i], scope, workspace, epoch); err != nil {
				set(i, "skipped", err.Error())
				terminal++
				continue
			}
			call := nodeCall(i, args)
			a.mu.Lock()
			plan.Nodes[i].Arguments = args
			plan.Nodes[i].State = "dispatched"
			a.mu.Unlock()
			if err := checkpoint("tool_dispatch", &call, ""); err != nil {
				set(i, "skipped", "Intent persistence failed; not dispatched.")
				terminal++
				fatal = err
				cancel()
				continue
			}
			a.publishStream(task.ID, streamEvent{Event: "intent", Tool: spec.Tool, CallID: call.ID, Args: summarizeToolArgs(call.Function.Arguments)})
			active++
			go func(i int, args map[string]any) {
				admitted := context.WithValue(ctx, pluginPlanAdmissionKey{}, func() error {
					if err := a.pluginPlanPolicy(task, nodes[i].Tool, owners[i], scope, workspace, epoch); err != nil {
						return err
					}
					return a.recoveryOperationError(task, nodes[i].Tool, args, plan)
				})
				raw, err := a.callPluginToolContext(admitted, owners[i], nodes[i].Tool, args)
				results <- response{i, raw, err}
			}(i, args)
		}
		if active > 0 {
			finish(<-results)
		} else if terminal < len(nodes) { // A blocked dependency may precede its failed parent in input order.
			continue
		}
	}
	// Stable controller-only merge. No worker can append files or mutate versions.
	proposalOwners := map[string]string{}
	for i := range plan.Nodes {
		n := &plan.Nodes[i]
		if len(n.Proposals) > 0 {
			a.mu.Lock()
			n.MergeState = "withheld"
			a.mu.Unlock()
		}
		if ctx.Err() == nil && fatal == nil {
			if len(n.Proposals) > 0 {
				a.mu.Lock()
				n.MergeState = "queued"
				a.mu.Unlock()
			}
			accepted, rejected := 0, 0
			for _, proposal := range n.Proposals {
				var mergeErr error
				file, _ := proposal["path"].(string)
				kind, _ := proposal["type"].(string)
				target := ""
				if kind == "file" {
					target = path.Clean(file)
					if prior := proposalOwners[target]; prior != "" {
						mergeErr = fmt.Errorf("competing file proposals from %s and %s; reconcile explicitly", prior, n.ID)
					}
				}
				if mergeErr == nil {
					_, mergeErr = a.recordToolProposal(task, versions, proposal)
				}
				if mergeErr != nil {
					rejected++
					a.mu.Lock()
					n.Error = "Proposal rejected: " + mergeErr.Error()
					n.MergeState = "rejected"
					a.mu.Unlock()
				} else {
					accepted++
					if target != "" {
						proposalOwners[target] = n.ID
					}
				}
			}
			if accepted > 0 && rejected > 0 {
				a.mu.Lock()
				n.MergeState = "partial"
				a.mu.Unlock()
			}
		}
		if n.State == "succeeded" || n.State == "proposed" || n.State == "uncertain" {
			call := nodeCall(i, n.Arguments)
			a.mu.Lock()
			use := ToolUse{Tool: n.Tool, Args: call.Function.Arguments, Result: n.Result, Preview: parallelClip(n.Result, 2000), Who: "main"}
			if task.ParentSessionID != "" {
				use.Who = "sub"
				use.ChildSessionID = task.ChildSessionID
				use.ChildNumber = task.ChildNumber
				use.ChildTitle = task.ChildTitle
			}
			if n.State == "uncertain" {
				task.Failures++
			}
			task.ToolUses = append(task.ToolUses, use)
			a.mu.Unlock()
			a.recordPluginExperience(n.Tool, owners[i], n.State == "succeeded" || n.State == "proposed", n.Result)
		}
	}
	a.mu.Lock()
	plan.State = "completed"
	if ctx.Err() != nil || fatal != nil {
		plan.State = "interrupted"
	}
	out := encode(plan)
	a.mu.Unlock()
	if err := checkpoint("plugin_plan_result", nil, out); err != nil {
		return "错误：插件任务图结果保存失败，不得自动重试: " + err.Error()
	}
	if fatal != nil {
		return out + "\n执行记录错误，不得自动重试：" + fatal.Error()
	}
	return out
}
