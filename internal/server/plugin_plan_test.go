package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func planCall(t *testing.T, id string, nodes []pluginPlanNodeSpec) ToolCall {
	t.Helper()
	b, err := json.Marshal(map[string]any{"nodes": nodes})
	if err != nil {
		t.Fatal(err)
	}
	c := readCall("run_plugin_plan", string(b))
	c.ID = id
	return c
}

func planTask(a *App, limit int) *Task {
	task := &Task{ID: "plan-task", Status: "running", WorkspaceID: a.wsID(), Parallelism: limit, CheckpointStep: "chat"}
	a.sessions["plan-session"] = &Session{ID: "plan-session", Runs: []*Task{task}}
	return task
}

func planAwait(t *testing.T, name string, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for !ready() {
		if time.Now().After(deadline) {
			t.Fatal("waiting for " + name)
		}
		time.Sleep(time.Millisecond)
	}
}
func planExists(p string) bool { _, err := os.Stat(p); return err == nil }
func planDecode(t *testing.T, text string) PluginExecutionPlan {
	t.Helper()
	var p PluginExecutionPlan
	if err := json.Unmarshal([]byte(text), &p); err != nil {
		t.Fatalf("plan result: %v: %s", err, text)
	}
	return p
}
func planInstall(t *testing.T, a *App, id, code string) {
	t.Helper()
	requireStatus(t, request(a, "POST", "/api/plugins", map[string]any{"id": id, "name": id, "code": code}), 201)
}

func TestPluginPlanValidationAndBudget(t *testing.T) {
	valid := []pluginPlanNodeSpec{{ID: "a", Tool: "read_part", Arguments: map[string]any{}}, {ID: "b", Tool: "join_parts", Arguments: map[string]any{}, DependsOn: []string{"a"}, Bindings: map[string]string{"content": "a"}}}
	c := planCall(t, "graph", valid)
	if _, err := parsePluginPlan(c.Function.Arguments); err != nil {
		t.Fatal(err)
	}
	if pluginPlanDispatchCost(c) != 3 || pluginPlanDispatchCost(readCall("read_file", "{}")) != 1 {
		t.Fatal("nested budget bypass")
	}
	for _, input := range []string{
		`{"nodes":[]}`, `{"nodes":[{"id":"a","tool":"t","arguments":{},"state":"succeeded"}]}`,
		`{"nodes":[{"id":"a","tool":"t","arguments":{},"dependsOn":["a"]}]}`,
		`{"nodes":[{"id":"a","tool":"t","arguments":{},"dependsOn":["missing"]}]}`,
		`{"nodes":[{"id":"a","tool":"t","arguments":{}},{"id":"a","tool":"t","arguments":{}}]}`,
		`{"nodes":[{"id":"a","tool":"t","arguments":{},"dependsOn":["b"]},{"id":"b","tool":"t","arguments":{},"dependsOn":["a"]}]}`,
		`{"nodes":[{"id":"a","tool":"t","arguments":{},"bindings":{"content":"b"}},{"id":"b","tool":"t","arguments":{}}]}`,
		`{"nodes":[{"id":"a","tool":"t","arguments":{}}]} null`,
	} {
		if _, err := parsePluginPlan(input); err == nil {
			t.Fatalf("accepted invalid graph: %s", input)
		}
	}
	tooMany := make([]pluginPlanNodeSpec, 21)
	for i := range tooMany {
		tooMany[i] = pluginPlanNodeSpec{ID: fmt.Sprint(i), Tool: "t", Arguments: map[string]any{}}
	}
	if _, err := parsePluginPlan(planCall(t, "large", tooMany).Function.Arguments); err == nil {
		t.Fatal("accepted excessive node budget")
	}
}

func TestPluginPlanROS2ParallelAndStableProposalMerge(t *testing.T) {
	for _, selected := range []int{1, 5, -5} {
		t.Run(fmt.Sprint(selected), func(t *testing.T) {
			a := testApp(t)
			limit, requested := selected, selected
			if selected < 0 {
				limit, requested = 1, -selected
			}
			task := planTask(a, requested)
			if selected < 0 {
				task.HarnessConfig = &HarnessConfig{Hooks: []HarnessHook{{Name: "check", Event: "before_tool", Tool: "part0_read", Command: "pwd", Enabled: true, TimeoutSec: 5}}}
			}
			parts := []pluginPlanNodeSpec{}
			deps := []string{}
			bindings := map[string]string{}
			for i := 0; i < 5; i++ {
				id := fmt.Sprintf("part%d", i)
				filename := id + ".txt"
				content := fmt.Sprintf("ROS2 chapter%d", i)
				p := filepath.Join(a.workPath, filename)
				if err := os.WriteFile(p, []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
				encoded, _ := json.Marshal(p)
				code := fmt.Sprintf(`const fs=require('fs'),path=require('path');module.exports={apply(ctx){ctx.tool({name:%q,scheduling:{resources:[{root:'workspace',path:%q,access:'read'}]},handler:async()=>{fs.writeFileSync(path.join(__dirname,'entered'),'1');for(let i=0;!fs.existsSync(path.join(__dirname,'release'));i++){if(i>1200)throw Error('barrier timeout');await new Promise(r=>setTimeout(r,10));}return {text:fs.readFileSync(%s,'utf8')};}})}};`, id+"_read", filename, encoded)
				planInstall(t, a, id, code)
				parts = append(parts, pluginPlanNodeSpec{ID: id, Tool: id + "_read", Arguments: map[string]any{}})
				deps = append(deps, id)
				bindings[id] = id
			}
			planInstall(t, a, "joiner", `module.exports={apply(ctx){ctx.tool({name:'join_ros2',scheduling:{resources:[{root:'workspace',path:'ros2.md',access:'write'}]},handler:(args,api)=>api.proposeWrite('ros2.md',[args.part0,args.part1,args.part2,args.part3,args.part4].join('\n'))})}};`)
			nodes := append([]pluginPlanNodeSpec{{ID: "join", Tool: "join_ros2", Arguments: map[string]any{}, DependsOn: deps, Bindings: bindings}}, parts...)
			call := planCall(t, "ros2", nodes)
			task.CheckpointMessages = []Message{{Role: "assistant", ToolCalls: []ToolCall{call}}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := make(chan string, 1)
			go func() { result <- a.executeToolCall(ctx, call, task, map[string]Change{}) }()
			marker := func(i int, suffix string) string {
				return filepath.Join(a.pluginsPath, fmt.Sprintf("part%d", i), suffix)
			}
			defer func() {
				for i := 0; i < 5; i++ {
					_ = os.WriteFile(marker(i, "release"), []byte("1"), 0600)
				}
			}()
			if limit == 5 {
				planAwait(t, "all five actual handlers", func() bool {
					for i := 0; i < 5; i++ {
						if !planExists(marker(i, "entered")) {
							return false
						}
					}
					return true
				})
				for i := 0; i < 5; i++ {
					if err := os.WriteFile(marker(i, "release"), []byte("1"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			} else {
				for i := 0; i < 5; i++ {
					current := i
					planAwait(t, "sequential handler", func() bool { return planExists(marker(current, "entered")) })
					for j := i + 1; j < 5; j++ {
						if planExists(marker(j, "entered")) {
							t.Fatal("1x dispatched overlapping handlers")
						}
					}
					if err := os.WriteFile(marker(i, "release"), []byte("1"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			var text string
			select {
			case text = <-result:
			case <-time.After(10 * time.Second):
				t.Fatal("plan did not finish")
			}
			p := planDecode(t, text)
			if p.Requested != requested || selected < 0 && p.Reason == "" {
				t.Fatal("effective hook concurrency hidden", p)
			}
			if p.State != "completed" || p.Limit != limit || len(p.Nodes) != 6 || p.Nodes[0].State != "proposed" || p.Nodes[0].MergeState != "queued" {
				t.Fatalf("plan: %+v", p)
			}
			if len(task.Files) != 1 || task.Files[0].Path != "ros2.md" || task.Files[0].Content != "ROS2 chapter0\nROS2 chapter1\nROS2 chapter2\nROS2 chapter3\nROS2 chapter4" {
				t.Fatalf("proposal merge: %+v", task.Files)
			}
			if planExists(filepath.Join(a.workPath, "ros2.md")) {
				t.Fatal("proposal applied without approval")
			}
			if len(task.ToolUses) != 6 || task.ToolUses[0].Tool != "join_ros2" || task.ToolUses[5].Tool != "part4_read" {
				t.Fatal("tool records were not merged in input order")
			}
			journal, err := os.ReadFile(filepath.Join(a.dataPath, "execution-events", task.ID+".jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			for _, node := range nodes {
				if !strings.Contains(string(journal), `"callId":"ros2/`+node.ID+`"`) {
					t.Fatal("missing node receipt", node.ID)
				}
			}
			if len(task.ToolExecutionIntents) != 0 {
				t.Fatal("finished node left unresolved intent")
			}
			// The same persisted call ID is read back, never executed again.
			_ = a.executeToolCall(ctx, call, task, map[string]Change{})
			if len(task.ToolUses) != 6 || len(task.Files) != 1 {
				t.Fatal("persisted graph replayed")
			}
			nodes[0].Title = "changed parameters"
			if out := a.executeToolCall(ctx, planCall(t, "ros2", nodes), task, map[string]Change{}); !strings.HasPrefix(out, "错误") {
				t.Fatal("persisted ID accepted different plan parameters")
			}
		})
	}
}

func TestPluginPlanFailureBlocksDependenciesAndReplay(t *testing.T) {
	a := testApp(t)
	task := planTask(a, 5)
	planInstall(t, a, "broken", `const fs=require('fs'),path=require('path');module.exports={apply(ctx){ctx.tool({name:'fail_node',handler:()=>{fs.appendFileSync(path.join(__dirname,'attempts'),'x');throw Error('expected failure');}});ctx.tool({name:'never_node',handler:()=>{fs.writeFileSync(path.join(__dirname,'unexpected'),'1');return 'unexpected';}});ctx.tool({name:'okay_node',handler:()=>({text:'independent result'})});}};`)
	nodes := []pluginPlanNodeSpec{{ID: "dependent", Tool: "never_node", Arguments: map[string]any{}, DependsOn: []string{"failed"}}, {ID: "failed", Tool: "fail_node", Arguments: map[string]any{}}, {ID: "independent", Tool: "okay_node", Arguments: map[string]any{}}}
	call := planCall(t, "fail-graph", nodes)
	p := planDecode(t, a.executeToolCall(context.Background(), call, task, map[string]Change{}))
	if p.Nodes[0].State != "skipped" || p.Nodes[1].State != "uncertain" || p.Nodes[2].State != "succeeded" {
		t.Fatalf("failure propagation: %+v", p.Nodes)
	}
	if planExists(filepath.Join(a.pluginsPath, "broken", "unexpected")) {
		t.Fatal("failed dependency executed")
	}
	if len(task.ToolExecutionIntents) != 1 {
		t.Fatal("uncertain effect lost intent")
	}
	_ = a.executeToolCall(context.Background(), call, task, map[string]Change{})
	call.ID = "new-graph"
	out := a.executeToolCall(context.Background(), call, task, map[string]Change{})
	if !strings.HasPrefix(out, "错误") {
		t.Fatal("new plan bypassed unresolved effect")
	}
	b, err := os.ReadFile(filepath.Join(a.pluginsPath, "broken", "attempts"))
	if err != nil || string(b) != "x" {
		t.Fatalf("effect replayed: %q %v", b, err)
	}
}

func TestPluginPlanRejectsWholeGraphBeforeDispatch(t *testing.T) {
	for _, kind := range []string{"disabled", "sandbox", "agent", "source", "builtin", "cycle"} {
		t.Run(kind, func(t *testing.T) {
			a := testApp(t)
			task := planTask(a, 5)
			planInstall(t, a, "marker", `const fs=require('fs'),path=require('path');module.exports={apply(ctx){for(const name of ['marker_one','marker_two'])ctx.tool({name,handler:()=>{fs.writeFileSync(path.join(__dirname,'unexpected'),'1');return 'done';}})}};`)
			nodes := []pluginPlanNodeSpec{{ID: "first", Tool: "marker_one", Arguments: map[string]any{}}, {ID: "second", Tool: "marker_two", Arguments: map[string]any{}}}
			switch kind {
			case "disabled":
				a.settings.DisabledTools = []string{"marker_two"}
			case "sandbox":
				a.settings.SandboxMode = "read-only"
			case "agent":
				task.AgentToolsSet = true
				task.AgentTools = []string{"run_plugin_plan", "marker_one"}
			case "source":
				nodes[1].Arguments["source"] = "reference"
			case "builtin":
				nodes[1].Tool = "run_shell"
			case "cycle":
				nodes[0].DependsOn = []string{"second"}
				nodes[1].DependsOn = []string{"first"}
			}
			out := a.executeToolCall(context.Background(), planCall(t, "invalid", nodes), task, map[string]Change{})
			if !strings.HasPrefix(out, "错误") {
				t.Fatal("invalid plan accepted", out)
			}
			if planExists(filepath.Join(a.pluginsPath, "marker", "unexpected")) || len(task.ToolExecutionIntents) > 0 || len(task.PluginPlans) > 0 {
				t.Fatal("partially dispatched invalid graph")
			}
		})
	}
}

func TestPluginPlanAdmissionRechecksSandbox(t *testing.T) {
	a := testApp(t)
	task := planTask(a, 1)
	planInstall(t, a, "marker", `const fs=require('fs'),path=require('path');module.exports={apply(ctx){ctx.tool({name:'marker_one',handler:()=>{fs.writeFileSync(path.join(__dirname,'unexpected'),'1');return 'done';}})}};`)
	release, err := a.pluginResources.acquire(context.Background(), pluginFootprint{exclusive: true})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan string, 1)
	go func() {
		done <- a.executeToolCall(context.Background(), planCall(t, "queued", []pluginPlanNodeSpec{{ID: "one", Tool: "marker_one", Arguments: map[string]any{}}}), task, map[string]Change{})
	}()
	waitPluginResources(t, &a.pluginResources, 2)
	a.mu.Lock()
	a.settings.SandboxMode = "read-only"
	a.mu.Unlock()
	release()
	select {
	case out := <-done:
		p := planDecode(t, out)
		if p.Nodes[0].State != "skipped" {
			t.Fatal("missing blocked result")
		}
	case <-time.After(8 * time.Second):
		t.Fatal("blocked graph did not finish")
	}
	if planExists(filepath.Join(a.pluginsPath, "marker", "unexpected")) {
		t.Fatal("sandbox change ignored after resource wait")
	}
}

func TestPluginPlanCancellationRecoveryAndNoReplay(t *testing.T) {
	a := testApp(t)
	task := planTask(a, 1)
	planInstall(t, a, "slow", `const fs=require('fs'),path=require('path');module.exports={apply(ctx){ctx.tool({name:'slow_node',handler:async()=>{fs.writeFileSync(path.join(__dirname,'entered'),'1');await new Promise(r=>setTimeout(r,800));fs.writeFileSync(path.join(__dirname,'late'),'1');return 'late';}})}};`)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	call := planCall(t, "cancelled", []pluginPlanNodeSpec{{ID: "slow", Tool: "slow_node", Arguments: map[string]any{}}, {ID: "later", Tool: "slow_node", Arguments: map[string]any{}, DependsOn: []string{"slow"}}})
	task.CheckpointMessages = []Message{{Role: "assistant", ToolCalls: []ToolCall{call}}}
	done := make(chan string, 1)
	go func() { done <- a.executeToolCall(ctx, call, task, map[string]Change{}) }()
	planAwait(t, "slow handler", func() bool { return planExists(filepath.Join(a.pluginsPath, "slow", "entered")) })
	cancel()
	select {
	case out := <-done:
		p := planDecode(t, out)
		if p.State != "interrupted" || p.Nodes[0].State != "uncertain" || p.Nodes[1].State != "skipped" {
			t.Fatalf("pause state: %+v", p)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("cancel did not join worker")
	}
	time.Sleep(900 * time.Millisecond)
	if planExists(filepath.Join(a.pluginsPath, "slow", "late")) {
		t.Fatal("cancelled graph left handler writing")
	}
	b, err := os.ReadFile(SessionPath(a.dataPath, "plan-session", sessionBucketFor(a.sessions["plan-session"])))
	if err != nil {
		t.Fatal(err)
	}
	var persisted Session
	if err := json.Unmarshal(b, &persisted); err != nil {
		t.Fatal(err)
	}
	recovered := persisted.Runs[0]
	recoverExecutionCheckpoint(recovered)
	if recovered.PluginPlans[0].Nodes[0].State != "uncertain" || !recovered.HookRecoveryRequired {
		t.Fatal("durable uncertainty lost on recovery")
	}
	if !strings.Contains(a.executePluginPlan(context.Background(), call, recovered, map[string]Change{}), "uncertain") {
		t.Fatal("recovery replayed plan")
	}
	manual := &Task{PluginPlans: []*PluginExecutionPlan{{State: "running", Nodes: []PluginPlanNode{{State: "dispatched"}, {State: "waiting"}, {State: "succeeded"}}}}}
	recoverPluginPlans(manual)
	if manual.PluginPlans[0].Nodes[0].State != "uncertain" || manual.PluginPlans[0].Nodes[1].State != "skipped" || manual.PluginPlans[0].Nodes[2].State != "succeeded" {
		t.Fatal("recovery invented node execution")
	}
}

func TestPluginPlanControlApprovalsAreSerialized(t *testing.T) {
	a := testApp(t)
	task := planTask(a, 5)
	planInstall(t, a, "browser-control", `const fs=require('fs'),path=require('path');module.exports={apply(ctx){ctx.tool({name:'browser_click',handler:(args)=>{if(args.approved!==true)throw Error('unapproved');fs.writeFileSync(path.join(__dirname,args.selector),'1');return 'clicked';}})}};`)
	call := planCall(t, "controls", []pluginPlanNodeSpec{{ID: "first", Tool: "browser_click", Arguments: map[string]any{"selector": "first"}}, {ID: "second", Tool: "browser_click", Arguments: map[string]any{"selector": "second"}}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan string, 1)
	go func() { done <- a.executeToolCall(ctx, call, task, map[string]Change{}) }()
	var first chan string
	var firstRound int64
	planAwait(t, "first approval", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		first = task.AnswerCh
		firstRound = task.answerRound
		return first != nil
	})
	if planExists(filepath.Join(a.pluginsPath, "browser-control", "first")) {
		t.Fatal("control executed before approval")
	}
	first <- "确认"
	var second chan string
	planAwait(t, "second approval", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		second = task.AnswerCh
		return second != nil && second != first && task.answerRound > firstRound
	})
	second <- "取消"
	select {
	case out := <-done:
		p := planDecode(t, out)
		if p.Nodes[0].State != "succeeded" || p.Nodes[1].State != "skipped" {
			t.Fatalf("approval results: %+v", p.Nodes)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("approval graph did not finish")
	}
	if !planExists(filepath.Join(a.pluginsPath, "browser-control", "first")) || planExists(filepath.Join(a.pluginsPath, "browser-control", "second")) {
		t.Fatal("approval applied to wrong node")
	}
}

func TestPluginPlanModelToolLoopIntegration(t *testing.T) {
	a := testApp(t)
	planInstall(t, a, "chapters", `module.exports={apply(ctx){ctx.tool({name:'chapter_read',handler:()=>({text:'chapter evidence'})});ctx.tool({name:'chapter_join',handler:(args)=>({text:'final:'+args.content})});}};`)
	call := planCall(t, "native-plan", []pluginPlanNodeSpec{{ID: "read", Tool: "chapter_read", Arguments: map[string]any{}}, {ID: "join", Tool: "chapter_join", Arguments: map[string]any{}, DependsOn: []string{"read"}, Bindings: map[string]string{"content": "read"}}})
	provider := newToolProvider(t, []func() (string, []ToolCall){func() (string, []ToolCall) { return "", []ToolCall{call} }, func() (string, []ToolCall) { return "Task complete with returned evidence.", nil }})
	defer provider.Close()
	a.settings = Settings{BaseURL: provider.URL, Model: "test"}
	session := createSession(t, a)
	requireStatus(t, request(a, "POST", "/api/sessions/"+session.ID+"/runs", map[string]any{"mode": "chat", "prompt": "Use the plugin tools to combine chapter evidence."}), 202)
	waitTaskDone(t, a, session.ID)
	a.mu.Lock()
	b, _ := json.Marshal(a.sessions[session.ID].Runs[0])
	a.mu.Unlock()
	var task Task
	if err := json.Unmarshal(b, &task); err != nil {
		t.Fatal(err)
	}
	if task.Status != "completed" || len(task.PluginPlans) != 1 || task.PluginPlans[0].State != "completed" || len(task.ToolUses) != 3 {
		t.Fatalf("central tool execution: status=%s plans=%d uses=%d error=%s", task.Status, len(task.PluginPlans), len(task.ToolUses), task.Error)
	}
	sawResult, sawSchema := false, false
	for _, messages := range provider.snapshotRequests() {
		for _, m := range messages {
			if m.Role == "tool" && m.ToolCallID == call.ID && strings.Contains(m.Content, "final:chapter evidence") {
				sawResult = true
			}
		}
	}
	for _, snapshot := range task.RequestSnapshots {
		var wire struct {
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		_ = json.Unmarshal(snapshot.Body, &wire)
		for _, tool := range wire.Tools {
			if tool.Function.Name == "run_plugin_plan" {
				sawSchema = true
			}
		}
	}
	if !sawResult || !sawSchema {
		t.Fatalf("wire integration: result=%t schema=%t", sawResult, sawSchema)
	}
}

func TestPluginPlanCentralBudgetPreventsNestedBypass(t *testing.T) {
	a := testApp(t)
	task := planTask(a, 5)
	task.Steps = []Step{{Name: "chat"}}
	task.ExecutionPolicy = &ExecutionPolicy{ToolMaxRounds: 1}
	planInstall(t, a, "marker", `const fs=require('fs'),path=require('path');module.exports={apply(ctx){ctx.tool({name:'budget_marker',handler:()=>{fs.writeFileSync(path.join(__dirname,'unexpected'),'1');return 'done';}})}};`)
	nodes := []pluginPlanNodeSpec{}
	for i := 0; i < 20; i++ {
		nodes = append(nodes, pluginPlanNodeSpec{ID: fmt.Sprint(i), Tool: "budget_marker", Arguments: map[string]any{}})
	}
	calls := []ToolCall{}
	for i := 0; i < 4; i++ {
		calls = append(calls, planCall(t, fmt.Sprintf("budget%d", i), nodes))
	}
	provider := newToolProvider(t, []func() (string, []ToolCall){func() (string, []ToolCall) { return "", calls }})
	defer provider.Close()
	cfg := Settings{BaseURL: provider.URL, Model: "test"}
	out, _, err := a.toolLoop(context.Background(), cfg, []Message{{Role: "user", Content: "budget check"}}, ProfileParams{MaxTokens: 128}, builtinTools, task, map[string]Change{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "64") || len(task.PluginPlans) > 0 || len(task.ToolUses) > 0 || planExists(filepath.Join(a.pluginsPath, "marker", "unexpected")) {
		t.Fatalf("nested budget bypass: %s plans=%d uses=%d", out, len(task.PluginPlans), len(task.ToolUses))
	}
}

func TestPluginPlanProposalDependenciesAndOutcomeUnknown(t *testing.T) {
	a := testApp(t)
	task := planTask(a, 5)
	planInstall(t, a, "proposal", `const fs=require('fs'),path=require('path');module.exports={apply(ctx){ctx.tool({name:'propose_part',handler:(_,api)=>api.proposeWrite('chapter.md','pending chapter')});ctx.tool({name:'after_proposal',handler:()=>{fs.writeFileSync(path.join(__dirname,'unexpected'),'1');return 'unexpected';}})}};`)
	call := planCall(t, "proposal-graph", []pluginPlanNodeSpec{{ID: "write", Tool: "propose_part", Arguments: map[string]any{}}, {ID: "read", Tool: "after_proposal", Arguments: map[string]any{}, DependsOn: []string{"write"}}})
	p := planDecode(t, a.executeToolCall(context.Background(), call, task, map[string]Change{}))
	if p.Nodes[0].State != "proposed" || p.Nodes[1].State != "skipped" || len(task.Files) != 1 || planExists(filepath.Join(a.pluginsPath, "proposal", "unexpected")) {
		t.Fatal("proposal treated as applied write")
	}
	unknown := &Task{ID: "unknown", PluginPlans: []*PluginExecutionPlan{{ID: "graph", State: "interrupted", Nodes: []PluginPlanNode{{pluginPlanNodeSpec: pluginPlanNodeSpec{ID: "node", Tool: "external_write"}, State: "uncertain"}}}}}
	out, _ := json.Marshal(taskOutcomeProjection(unknown, "session", 1, "title", 0, 0, 40))
	if !strings.Contains(string(out), "graph/node") {
		t.Fatal("outcome lost recovered uncertain graph effect", string(out))
	}
}

func TestPluginPlanPersistenceFailurePreventsDispatch(t *testing.T) {
	a := testApp(t)
	task := planTask(a, 5)
	planInstall(t, a, "marker", `const fs=require('fs'),path=require('path');module.exports={apply(ctx){ctx.tool({name:'persist_marker',handler:()=>{fs.writeFileSync(path.join(__dirname,'unexpected'),'1');return 'done';}})}};`)
	if err := os.WriteFile(filepath.Join(a.dataPath, "execution-events"), []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	out := a.executeToolCall(context.Background(), planCall(t, "no-persist", []pluginPlanNodeSpec{{ID: "one", Tool: "persist_marker", Arguments: map[string]any{}}}), task, map[string]Change{})
	if !strings.HasPrefix(out, "错误") || planExists(filepath.Join(a.pluginsPath, "marker", "unexpected")) || task.PluginPlans[0].Nodes[0].State != "skipped" {
		t.Fatal("failed journal dispatched a handler", out)
	}
}

func TestPluginPlanCompetingProposalsDoNotOverwrite(t *testing.T) {
	a := testApp(t)
	task := planTask(a, 5)
	planInstall(t, a, "competing", `module.exports={apply(ctx){ctx.tool({name:'proposal_one',handler:(_,api)=>api.proposeWrite('shared.md','first')});ctx.tool({name:'proposal_two',handler:(_,api)=>api.proposeWrite('shared.md','second')});}};`)
	call := planCall(t, "competing", []pluginPlanNodeSpec{{ID: "first", Tool: "proposal_one", Arguments: map[string]any{}}, {ID: "second", Tool: "proposal_two", Arguments: map[string]any{}}})
	p := planDecode(t, a.executeToolCall(context.Background(), call, task, map[string]Change{}))
	if len(task.Files) != 1 || task.Files[0].Content != "first" || p.Nodes[0].MergeState != "queued" || p.Nodes[1].MergeState != "rejected" || !strings.Contains(p.Nodes[1].Error, "competing file proposals") {
		t.Fatalf("lost proposal or hidden conflict: %+v files=%+v", p.Nodes, task.Files)
	}
}

func TestPluginPlanOutcomeSnapshotCopiesOnlyUncertainty(t *testing.T) {
	a := testApp(t)
	task := planTask(a, 1)
	task.PluginPlans = []*PluginExecutionPlan{{ID: "graph", Nodes: []PluginPlanNode{{pluginPlanNodeSpec: pluginPlanNodeSpec{ID: "node", Tool: "external_write", Arguments: map[string]any{"secret": "private argument"}}, State: "uncertain", Result: "private result", Proposals: []map[string]any{{"content": "private proposal"}}}}}}
	snapshot, _, _, err := a.outcomeTaskSnapshot("plan-session", task.ID)
	if err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	task.PluginPlans[0].Nodes[0].State = "succeeded"
	a.mu.Unlock()
	node := snapshot.PluginPlans[0].Nodes[0]
	if node.State != "uncertain" || node.Arguments != nil || node.Result != "" || node.Proposals != nil {
		t.Fatal("snapshot aliases source or copies private plan payload")
	}
	out, _ := json.Marshal(taskOutcomeProjection(snapshot, "plan-session", 1, "title", 0, 0, 40))
	if !strings.Contains(string(out), "graph/node") || strings.Contains(string(out), "private") {
		t.Fatal("uncertainty boundary lost", string(out))
	}
}

func TestPluginPlanOwnerLookupSurvivesSurfaceRefresh(t *testing.T) {
	a := testApp(t)
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		for i := 0; i < 200; i++ {
			a.mu.Lock()
			a.pluginSurface = []byte(`{"plugins":[{"id":"refresh","tools":[{"name":"refresh_tool","executable":true}]}]}`)
			a.mu.Unlock()
		}
	}()
	go func() {
		defer workers.Done()
		for i := 0; i < 200; i++ {
			_ = a.pluginOwnerOf("refresh_tool")
		}
	}()
	workers.Wait()
	if a.pluginOwnerOf("refresh_tool") != "refresh" {
		t.Fatal("lost refreshed plugin owner")
	}
}
