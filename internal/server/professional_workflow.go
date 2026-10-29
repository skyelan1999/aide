package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type pwNode struct {
	Bindings        []pwBinding    `json:"bindings,omitempty"`
	Cases           []pwCase       `json:"cases,omitempty"`
	Selector        string         `json:"selector,omitempty"`
	ID              string         `json:"id"`
	Type            string         `json:"type"`
	Title           string         `json:"title"`
	X               float64        `json:"x"`
	Y               float64        `json:"y"`
	Text            string         `json:"text,omitempty"`
	Model           string         `json:"model,omitempty"`
	ReasoningEffort string         `json:"reasoningEffort,omitempty"`
	Profile         string         `json:"profile,omitempty"`
	ModelRouter     string         `json:"modelRouter,omitempty"`
	Parallelism     int            `json:"parallelism,omitempty"`
	MaxTokens       int            `json:"maxTokens,omitempty"`
	Tools           []string       `json:"tools,omitempty"`
	Tool            string         `json:"tool,omitempty"`
	Arguments       map[string]any `json:"arguments,omitempty"`
}
type pwEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Port string `json:"port,omitempty"`
}
type pwDefinition struct {
	Schema int      `json:"schema"`
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Nodes  []pwNode `json:"nodes"`
	Edges  []pwEdge `json:"edges"`
}
type pwStore struct {
	Revision  int                     `json:"revision"`
	Templates map[string]pwDefinition `json:"templates"`
	Default   string                  `json:"default"`
	Bindings  map[string]string       `json:"bindings"`
}
type pwNodeResult struct {
	Request  map[string]any       `json:"request,omitempty"`
	Parallel *ParallelProgress    `json:"parallel,omitempty"`
	Routes   []ModelRouteDecision `json:"routes,omitempty"`
	Usage    TokenUsage           `json:"usage,omitempty"`

	Status   string `json:"status"`
	Output   string `json:"output,omitempty"`
	Error    string `json:"error,omitempty"`
	Started  string `json:"started,omitempty"`
	Finished string `json:"finished,omitempty"`
}
type pwExecution struct {
	Result string `json:"result,omitempty"`

	Definition pwDefinition             `json:"definition"`
	Digest     string                   `json:"digest"`
	Nodes      map[string]*pwNodeResult `json:"nodes"`
	Decisions  map[string]bool          `json:"decisions,omitempty"`
	Current    string                   `json:"current,omitempty"`
}

var pwID = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)

// Host limits and authorization are independent of the private scheduler.
func validatePW(d pwDefinition) error {
	raw, _ := json.Marshal(d)
	if len(raw) > 196608 || d.Schema != 1 || !pwID.MatchString(d.ID) || strings.TrimSpace(d.Name) == "" || len(d.Name) > 160 || len(d.Nodes) < 2 || len(d.Nodes) > 64 || len(d.Edges) > 128 {
		return errors.New("无效 .pw 定义或超过宿主限制")
	}
	seen := map[string]bool{}
	for _, n := range d.Nodes {
		if !pwID.MatchString(n.ID) || seen[n.ID] || len(n.Text) > 20000 || len(n.Title) > 160 {
			return errors.New("无效或重复节点")
		}
		seen[n.ID] = true
		switch n.Type {
		case "start", "end", "llm", "agent", "plugin", "switch", "condition", "template", "approval":
		case "tool":
			if !pwBuiltinAllowed(n.Tool) {
				return errors.New("PW 不支持此内置工具")
			}
		default:
			return errors.New("未知节点类型")
		}
		if err := validatePWBindings(n); err != nil {
			return err
		}
		if err := validatePWInference(n); err != nil {
			return err
		}
	}
	return nil
}
func (a *App) pwAvailableLocked() bool {
	if !a.pwEnabledLocked() {
		return false
	}
	_, err := loadPWRuntime(filepath.Join(a.pluginsPath, "professional-workflow"))
	return err == nil
}
func (a *App) pwEnabledLocked() bool {
	for _, p := range a.pluginRegistry.Plugins {
		if p.Enabled && p.ID == "professional-workflow" && p.Main == "index.js" && p.Version == "0.4.0" && p.Workbench != nil && p.Workbench.Entry == "workbench.html" && p.Workbench.ProfessionalWorkflow && a.pluginError(p.ID) == "" {
			return true
		}
	}
	return false
}
func (a *App) pwStorePath() string {
	return filepath.Join(a.dataPath, "professional-workflow", a.wsID()+".json")
}
func (a *App) loadPWLocked() pwStore {
	s := pwStore{Templates: map[string]pwDefinition{}, Bindings: map[string]string{}}
	raw, err := os.ReadFile(a.pwStorePath())
	if err == nil {
		if json.Unmarshal(raw, &s) != nil {
			s.Revision = -1
		}
	}
	if s.Templates == nil {
		s.Templates = map[string]pwDefinition{}
	}
	if s.Bindings == nil {
		s.Bindings = map[string]string{}
	}
	return s
}
func (a *App) pwControlLocked(args map[string]any) (any, error) {
	if !a.pwEnabledLocked() || a.loadLockStateLocked().Locked {
		return nil, errors.New("PW 插件不可用或工作台已锁定")
	}
	if _, err := loadPWRuntime(filepath.Join(a.pluginsPath, "professional-workflow")); err != nil {
		return nil, fmt.Errorf("PW runtime unavailable: %w", err)
	}
	action := stringValue(args["action"])
	store := a.loadPWLocked()
	if store.Revision < 0 {
		return nil, errors.New("PW 模板库损坏，已拒绝覆盖；请先恢复备份")
	}
	if action == "status" {
		sessions := []map[string]any{}
		for _, s := range a.sessions {
			if !s.Deleted && s.Kind != "assistant" {
				runs := []*Task{}
				for _, t := range s.Runs {
					if t.PW != nil && t.WorkspaceID == a.wsID() {
						runs = append(runs, t)
					}
				}
				if len(runs) > 10 {
					runs = runs[len(runs)-10:]
				}
				sessions = append(sessions, map[string]any{"id": s.ID, "title": s.Title, "runs": runs})
			}
		}
		return map[string]any{"store": store, "sessions": sessions, "models": a.settings.Models, "tools": json.RawMessage(a.pluginSurface), "builtinTools": pwBuiltinDescriptors(a.settings.DisabledTools), "inference": a.pwInferenceCatalogLocked()}, nil
	}
	if action == "approve" || action == "cancel" {
		sid, rid := stringValue(args["sessionId"]), stringValue(args["runId"])
		s := a.sessions[sid]
		if s == nil || s.Deleted {
			return nil, errors.New("会话不存在")
		}
		for _, t := range s.Runs {
			if t.ID != rid || t.PW == nil {
				continue
			}
			if t.WorkspaceID != a.wsID() || t.WorkspaceRev != a.wsRevision {
				return nil, errors.New("PW 运行所属工作区已变化")
			}
			if action == "cancel" {
				if c := a.cancels[t.ID]; c != nil {
					c()
				}
				return map[string]bool{"ok": true}, nil
			}
			node := stringValue(args["nodeId"])
			if t.Status != "running" || t.PW.Current != node || t.PW.Nodes[node] == nil || t.PW.Nodes[node].Status != "waiting" {
				return nil, errors.New("该节点未等待确认")
			}
			approved, ok := args["approved"].(bool)
			if !ok {
				return nil, errors.New("需要明确确认决定")
			}
			if t.PW.Decisions == nil {
				t.PW.Decisions = map[string]bool{}
			}
			if _, exists := t.PW.Decisions[node]; exists {
				return nil, errors.New("该确认已处理")
			}
			t.PW.Decisions[node] = approved
			return map[string]bool{"ok": true}, nil
		}
		return nil, errors.New("运行不存在")
	}
	rev, validRevision := args["revision"].(float64)
	if !validRevision || rev != float64(store.Revision) {
		return nil, errors.New("模板库已变化，请刷新后重试")
	}
	switch action {
	case "save":
		raw, _ := json.Marshal(args["definition"])
		var d pwDefinition
		if json.Unmarshal(raw, &d) != nil {
			return nil, errors.New("无效定义")
		}
		if err := a.validatePWRuntimeLocked(d); err != nil {
			return nil, err
		}
		if len(store.Templates) >= 64 && store.Templates[d.ID].ID == "" {
			return nil, errors.New("模板库上限64")
		}
		store.Templates[d.ID] = d
	case "bind":
		id := stringValue(args["templateId"])
		if store.Templates[id].ID == "" {
			return nil, errors.New("请先保存模板")
		}
		scope := stringValue(args["scope"])
		if scope == "default" || scope == "all" {
			store.Default = id
			if scope == "all" {
				store.Bindings = map[string]string{}
			}
		} else {
			sid := stringValue(args["sessionId"])
			s := a.sessions[sid]
			if s == nil || s.Deleted || s.Kind == "assistant" {
				return nil, errors.New("无效会话")
			}
			store.Bindings[sid] = id
		}
	default:
		return nil, errors.New("未知 PW 操作")
	}
	store.Revision++
	if err := os.MkdirAll(filepath.Dir(a.pwStorePath()), 0700); err != nil {
		return nil, err
	}
	if err := atomicJSON(a.pwStorePath(), store); err != nil {
		return nil, err
	}
	return map[string]any{"store": store}, nil
}
func (a *App) pwControlHTTP(w http.ResponseWriter, r *http.Request) {
	var in map[string]any
	if err := decode(w, r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	out, err := a.pwControlLocked(in)
	if err != nil {
		fail(w, 400, err)
		return
	}
	jsonOut(w, 200, out)
}
func (a *App) startPW(w http.ResponseWriter, r *http.Request, prompt string, attachments []Attachment, choice *SessionModelSelection) {
	if _, ok := r.Context().Value(assistantDispatchKey{}).(string); ok {
		fail(w, 403, errors.New("PW 仅允许用户主动运行"))
		return
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" || len(prompt) > 20000 || len(attachments) > 0 {
		fail(w, 400, errors.New("PW 首版需要1–20000字节文本；附件请通过模板中的插件节点读取"))
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.sessions[r.PathValue("id")]
	if s == nil || s.Deleted || s.Kind == "assistant" || !a.pwEnabledLocked() || a.loadLockStateLocked().Locked {
		fail(w, 403, errors.New("PW 会话或插件不可用"))
		return
	}
	if _, err := loadPWRuntime(filepath.Join(a.pluginsPath, "professional-workflow")); err != nil {
		fail(w, 503, err)
		return
	}
	for _, t := range s.Runs {
		if t.Status == "running" || t.Status == "awaiting_clarification" {
			fail(w, 409, errors.New("会话已有运行，请等待或停止"))
			return
		}
	}
	store := a.loadPWLocked()
	id := store.Bindings[s.ID]
	if id == "" {
		id = store.Default
	}
	d := store.Templates[id]
	if err := a.validatePWRuntimeLocked(d); err != nil {
		fail(w, 400, errors.New("请先在 PW 工作台保存并分配有效模板"))
		return
	}
	selected := a.sessionModelSelectionLocked(s)
	if choice != nil {
		selected = *choice
	}
	cfg, err := a.selectionSettingsLocked(selected)
	if err != nil {
		fail(w, 400, err)
		return
	}
	raw, _ := json.Marshal(d)
	snapshot := pwDefinition{}
	_ = json.Unmarshal(raw, &snapshot)
	if len(a.cancels) >= 4 {
		fail(w, 429, errors.New("运行中的任务过多"))
		return
	}
	t := &Task{Steps: []Step{}, Files: []Change{}, Commands: []string{}, Attachments: []Attachment{}, ID: newID(), Mode: "pw", Prompt: prompt, Status: "running", Created: time.Now().UTC().Format(time.RFC3339Nano), WorkspaceID: a.wsID(), WorkspaceRev: a.wsRevision, WorkspaceMode: a.workspaceMode(), WorkspaceRemotePath: a.wsConfig.Workspace.Path, Model: cfg.Model, ModelSelection: &selected, PW: &pwExecution{Definition: snapshot, Digest: hash(raw), Nodes: map[string]*pwNodeResult{}, Decisions: map[string]bool{}}}
	policy, policyErr := a.loadExecutionPolicy()
	if policyErr != nil {
		fail(w, 400, policyErr)
		return
	}
	harness, harnessErr := a.loadHarnessConfig()
	if harnessErr != nil {
		fail(w, 400, harnessErr)
		return
	}
	t.ExecutionPolicy, t.HarnessConfig = &policy, &harness
	t.AgentRoot = a.snapshotAgentRootLocked()
	t.WorktreeID = a.currentWorktreeIDLocked()
	t.Steer = make(chan string, 4)
	for _, n := range d.Nodes {
		t.PW.Nodes[n.ID] = &pwNodeResult{Status: "pending"}
	}
	ctx, cancel := context.WithTimeout(a.bgCtx, 15*time.Minute)
	a.cancels[t.ID] = cancel
	s.Runs = append(s.Runs, t)
	s.Messages = append(s.Messages, Message{Role: "user", Content: prompt, SourceRunID: t.ID, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)})
	s.Updated = t.Created
	if err := a.save(s); err != nil {
		cancel()
		delete(a.cancels, t.ID)
		s.Runs = s.Runs[:len(s.Runs)-1]
		s.Messages = s.Messages[:len(s.Messages)-1]
		fail(w, 500, err)
		return
	}
	a.background(func() { a.executePW(ctx, s, t, cfg) })
	jsonOut(w, 201, t)
}
func (a *App) executePW(ctx context.Context, s *Session, t *Task, cfg Settings) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	defer close(done)
	admission := func() error {
		a.mu.Lock()
		defer a.mu.Unlock()
		if !a.pwEnabledLocked() || a.wsID() != t.WorkspaceID || a.wsRevision != t.WorkspaceRev || a.loadLockStateLocked().Locked || s.Deleted {
			return errors.New("PW 权限或工作区已变化")
		}
		return nil
	}
	go func() {
		tick := time.NewTicker(200 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-tick.C:
				if admission() != nil {
					cancel()
					return
				}
			}
		}
	}()
	update := func() {
		a.mu.Lock()
		_ = a.save(s)
		a.mu.Unlock()
		a.broadcastSessionsChanged(s.ID)
		a.publishStream(t.ID, streamEvent{Event: "update"})
	}
	values := map[string]string{"input": t.Prompt}
	completed := map[string]bool{}
	active := map[string]bool{}
	versions := map[string]Change{}
	var runErr error
	final := ""
	for len(completed) < len(t.PW.Definition.Nodes) && runErr == nil {
		step, done, err := a.advancePW(ctx, t.PW.Definition, t.Prompt, values, completed, active)
		if err != nil {
			runErr = err
			break
		}
		if done {
			break
		}
		if step == nil {
			runErr = errors.New("PW 未返回可执行节点")
			break
		}
		var n pwNode
		for _, candidate := range t.PW.Definition.Nodes {
			if candidate.ID == step.NodeID {
				n = candidate
				break
			}
		}
		if n.ID == "" || completed[n.ID] || n.Type != step.Type {
			runErr = errors.New("PW 引擎返回无效节点标识或类型")
			break
		}
		pure := n.Type == "start" || n.Type == "template" || n.Type == "end" || n.Type == "condition" || n.Type == "switch"
		if !step.Skipped && pure != step.Pure {
			runErr = errors.New("PW 引擎节点执行类别无效")
			break
		}
		completed[n.ID] = true
		on := !step.Skipped
		a.mu.Lock()
		nr := t.PW.Nodes[n.ID]
		t.PW.Current = n.ID
		nr.Status = "running"
		nr.Started = time.Now().UTC().Format(time.RFC3339Nano)
		if !on {
			nr.Status = "skipped"
		}
		a.mu.Unlock()
		update()
		if !on {
			continue
		}
		active[n.ID] = true
		if runErr = ctx.Err(); runErr == nil {
			runErr = admission()
		}
		if runErr != nil {
			break
		}
		text := step.Text
		output := ""
		switch n.Type {
		case "start", "template", "end", "condition", "switch":
			output = step.Output
			if n.Type == "end" {
				final = output
			}
		case "approval", "plugin", "tool", "agent":
			a.mu.Lock()
			nr.Status = "waiting"
			nr.Request = map[string]any{"tool": n.Tool, "arguments": step.Arguments, "text": text, "model": n.Model, "reasoningEffort": n.ReasoningEffort, "parallelism": n.Parallelism, "tools": n.Tools, "bindings": n.Bindings}
			a.mu.Unlock()
			update()
			approved := false
			for runErr == nil {
				a.mu.Lock()
				decision, ok := t.PW.Decisions[n.ID]
				a.mu.Unlock()
				if ok {
					approved = decision
					break
				}
				select {
				case <-ctx.Done():
					runErr = ctx.Err()
				case <-time.After(150 * time.Millisecond):
				}
			}
			if runErr == nil && !approved {
				runErr = errors.New("用户拒绝节点执行")
			}
			if runErr != nil {
				break
			}
			a.mu.Lock()
			nr.Status = "running"
			a.mu.Unlock()
			update()
			if n.Type == "approval" {
				output = text
				break
			}
			if n.Type == "agent" {
				output, runErr = a.executePWInference(ctx, s, t, cfg, n, text, versions)
				break
			}
			if n.Type == "tool" {
				args := step.Arguments
				if runErr = pwBuiltinArguments(n.Tool, args); runErr != nil {
					break
				}
				raw, _ := json.Marshal(args)
				call := ToolCall{ID: t.ID + "_" + n.ID}
				call.Function.Name, call.Function.Arguments = n.Tool, string(raw)
				outcome := &toolDispatchOutcome{}
				output = a.executeToolCall(context.WithValue(ctx, toolDispatchOutcomeKey{}, outcome), call, t, versions)
				runErr = outcome.err
				if runErr == nil {
					runErr = ctx.Err()
				}
				break
			}
			if n.Tool == "" || a.pluginOwnerOf(n.Tool) == "" || a.pluginOwnerOf(n.Tool) == "professional-workflow" {
				runErr = errors.New("插件工具未安装或已停用")
				break
			}
			args := step.Arguments
			raw, _ := json.Marshal(map[string]any{"nodes": []any{map[string]any{"id": n.ID, "tool": n.Tool, "arguments": args}}})
			call := ToolCall{ID: t.ID + "_" + n.ID}
			call.Function.Arguments = string(raw)
			output = a.executePluginPlan(ctx, call, t, versions)
			var result struct {
				State string `json:"state"`
				Nodes []struct {
					State string `json:"state"`
				} `json:"nodes"`
			}
			bad := json.Unmarshal([]byte(output), &result) != nil || result.State != "completed"
			for _, n := range result.Nodes {
				bad = bad || (n.State != "succeeded" && n.State != "proposed")
			}
			if bad {
				runErr = errors.New("插件节点未成功，请检查输出与原审批记录")
			}
		case "llm":
			output, runErr = a.executePWInference(ctx, s, t, cfg, n, text, versions)
		}
		if len(output) > 16000 {
			output = output[:16000] + "\n[输出超过16KiB，已截断]"
		}
		values[n.ID] = output
		a.mu.Lock()
		nr.Output = output
		nr.Finished = time.Now().UTC().Format(time.RFC3339Nano)
		if runErr != nil {
			nr.Status = "failed"
			nr.Error = runErr.Error()
		} else {
			nr.Status = "completed"
		}
		a.mu.Unlock()
		update()
		if runErr != nil {
			break
		}
	}
	a.mu.Lock()
	if runErr != nil {
		t.Status = "failed"
		t.Error = runErr.Error()
		if ctx.Err() != nil {
			t.Status = "interrupted"
		}
	} else {
		t.Status = "completed"
		if len(t.Files) > 0 && !t.Applied {
			t.Status = "awaiting_approval"
		}
		t.PW.Result = final
		t.Steps = append(t.Steps, Step{Name: "pw", Status: "completed", Content: final})
		s.Messages = append(s.Messages, Message{Role: "assistant", Content: final, SourceRunID: t.ID, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)})
	}
	for _, n := range t.PW.Nodes {
		if n.Status == "pending" || n.Status == "running" || n.Status == "waiting" {
			n.Status = "skipped"
		}
	}
	t.PW.Current = ""
	if c := a.cancels[t.ID]; c != nil {
		c()
	}
	delete(a.cancels, t.ID)
	_ = a.save(s)
	status, errText := t.Status, t.Error
	a.mu.Unlock()
	a.finishStream(t.ID, status, errText)
	a.broadcastSessionsChanged(s.ID)
}
