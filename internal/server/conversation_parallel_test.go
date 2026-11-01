package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestParallelWorkerTokenBudgetPreservesDetailWithinContext(t *testing.T) {
	for _, tc := range []struct {
		name   string
		asked  int
		window int
		want   int
	}{
		{name: "default", asked: 0, window: 65536, want: defaultParallelWorkerTokens},
		{name: "profile detail budget", asked: 8192, window: 65536, want: 8192},
		{name: "small context reserve", asked: 8192, window: 4096, want: 2048},
		{name: "bounded by supported cap", asked: 16384, window: 65536, want: maxParallelWorkerTokens},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := parallelWorkerTokenBudget(tc.asked, tc.window); got != tc.want {
				t.Fatalf("parallelWorkerTokenBudget(%d, %d) = %d, want %d", tc.asked, tc.window, got, tc.want)
			}
		})
	}
}

func TestConversationParallelAdaptiveEvidence(t *testing.T) {
	for _, first := range []string{`{"tasks":[]}`, `invalid JSON`} {
		t.Run(first, func(t *testing.T) {
			a, s, task := parallelFixture(t, 2)
			var plans, workers atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Messages []Message         `json:"messages"`
					Tools    []any             `json:"tools"`
					Thinking map[string]string `json:"thinking"`
					Format   map[string]string `json:"response_format"`
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				if len(body.Tools) != 0 {
					t.Error("planner/worker received executable tools")
				}
				last := body.Messages[len(body.Messages)-1].Content
				if body.Messages[0].Role != "system" || len(body.Messages) != 3 {
					t.Error("analysis role still inherits executor user-task instructions")
				}
				if strings.HasPrefix(last, "[parallel planner]") {
					if body.Format["type"] != "json_object" {
						t.Error("scheduler response is not constrained to JSON")
					}
					if body.Thinking["type"] != "disabled" {
						t.Error("scheduling must use its output budget for JSON")
					}
					switch plans.Add(1) {
					case 1:
						parallelReply(w, first)
					case 2:
						parallelReply(w, `{"tasks":["architecture","tests"]}`)
					default:
						parallelReply(w, `{"tasks":["architecture","deployment"]}`) // one already done: do not launch duplicates
					}
					return
				}
				workers.Add(1)
				parallelReply(w, "new working note")
			}))
			defer provider.Close()
			cfg := Settings{BaseURL: provider.URL, Model: "fixture", ReasoningEffort: "high"}
			input := []Message{{Role: "user", Content: "Inspect before deciding how to split."}}
			input, err := a.prepareParallel(context.Background(), s, task, cfg, input, ProfileParams{MaxTokens: 4096})
			if err != nil {
				t.Fatal(err)
			}
			_, _ = a.prepareParallel(context.Background(), s, task, cfg, input, ProfileParams{MaxTokens: 4096})
			if plans.Load() != 1 {
				t.Fatal("unchanged checkpoint repeated planning")
			}
			input = append(input, Message{Role: "tool", ToolCallID: "read", Content: "Observed two independent components."})
			input, err = a.prepareParallel(context.Background(), s, task, cfg, input, ProfileParams{MaxTokens: 4096})
			if err != nil || plans.Load() != 2 || workers.Load() != 2 || task.Parallel.Round != 2 || len(task.Parallel.PlannerHistory) != 1 || task.Parallel.Phase != "merging" {
				t.Fatalf("new evidence did not unlock fanout: %v %+v", err, task.Parallel)
			}
			// Persist/recover the complete state. No new evidence must not replay workers.
			raw, _ := json.Marshal(task)
			var restored Task
			_ = json.Unmarshal(raw, &restored)
			s.Runs[0] = &restored
			_, err = a.prepareParallel(context.Background(), s, &restored, cfg, input, ProfileParams{MaxTokens: 4096})
			if err != nil || plans.Load() != 2 || workers.Load() != 2 {
				t.Fatal("resume replayed completed wave")
			}
			input = append(input, Message{Role: "user", Content: wrapSteer("Also inspect deployment.")})
			_, err = a.prepareParallel(context.Background(), s, &restored, cfg, input, ProfileParams{MaxTokens: 4096})
			if err != nil || plans.Load() != 3 || workers.Load() != 2 || len(restored.Parallel.Parts) != 2 || len(restored.Parallel.PlannerHistory) != 2 {
				t.Fatalf("duplicate completed work dispatched: %v %+v", err, restored.Parallel)
			}
			if restored.Usage.Total != 100 || restored.Parallel.Parts[0].Round != 2 {
				t.Fatal("multi-round usage/ledger lost")
			}
			if cfg.ReasoningEffort != "high" {
				t.Fatal("planner altered session configuration")
			}
		})
	}
}

func TestConversationParallelAnalysisContextIsolation(t *testing.T) {
	image := MessageImage{MediaType: "image/png", DataURL: "data:image/png;base64,private-pixels"}
	input := []Message{{Role: "system", Content: "Executor must maintain a ledger"}, {Role: "user", Content: "Inspect supplied image", Images: []MessageImage{image}, SourceRunID: "local-run-provenance"}, {Role: "assistant", ToolCalls: []ToolCall{readCall("read_file", `{"path":"a"}`)}}, {Role: "tool", ToolCallID: "one", Content: "Real evidence"}}
	got := parallelAnalysisMessages(input, "[parallel planner]\nReturn tasks JSON", true)
	if len(got) != 3 || got[0].Role != "system" || !strings.Contains(got[0].Content, "Return ONLY one JSON") || got[1].Role != "user" || len(got[1].Images) != 1 || got[1].Images[0] != image {
		t.Fatal("component roles/images not isolated")
	}
	if strings.Contains(got[1].Content, "private-pixels") || strings.Contains(got[1].Content, "local-run-provenance") {
		t.Fatal("serialized image payload or local provenance")
	}
	if !strings.Contains(got[1].Content, "Real evidence") || !strings.Contains(got[1].Content, "Executor must maintain a ledger") {
		t.Fatal("execution evidence was discarded")
	}
	for _, m := range got {
		if len(m.ToolCalls) > 0 || m.Role == "tool" {
			t.Fatal("executor tool protocol is still live in component request")
		}
	}
}

func TestConversationParallelEvidenceIgnoresBookkeepingAndReplay(t *testing.T) {
	read := readCall("read_file", `{"path":"discovery.md"}`)
	read.ID = "read-one"
	input := []Message{{Role: "user", Content: "Inspect first"}, {Role: "assistant", ToolCalls: []ToolCall{read}}, {Role: "tool", ToolCallID: read.ID, Content: "[工具记录1]\nObserved components"}}
	base := parallelEvidence(input)
	ledger := readCall("record_research_finding", `{"name":"components"}`)
	ledger.ID = "ledger-one"
	input = append(input, Message{Role: "assistant", ToolCalls: []ToolCall{ledger}}, Message{Role: "tool", ToolCallID: ledger.ID, Content: "[工具记录2]\nRecorded"}, Message{Role: "assistant", Content: "A draft"}, Message{Role: "user", Content: "A completion review nudge"})
	if parallelEvidence(input) != base {
		t.Fatal("internal bookkeeping/review became discovery")
	}
	read.ID = "read-two"
	input = append(input, Message{Role: "assistant", ToolCalls: []ToolCall{read}}, Message{Role: "tool", ToolCallID: read.ID, Content: "[工具记录3]\nObserved components"})
	if parallelEvidence(input) != base {
		t.Fatal("identical tool replay became new evidence")
	}
	input = append(input, Message{Role: "tool", ToolCallID: read.ID, Content: "[工具记录4]\nChanged component dependency"})
	if parallelEvidence(input) == base {
		t.Fatal("changed real tool result was ignored")
	}
	before := parallelEvidence(input)
	input = append(input, Message{Role: "user", Content: wrapSteer("Inspect deployment too")})
	if parallelEvidence(input) == before {
		t.Fatal("user steer ignored")
	}
}

func TestConversationParallelAdaptiveToolLoop(t *testing.T) {
	a, _, task := parallelFixture(t, 2)
	task.Prompt = "Read discovery.txt and analyse the components."
	task.Steer = make(chan string, 1)
	if err := os.WriteFile(filepath.Join(a.workPath, "discovery.txt"), []byte("COMPONENTS: engine, UI"), 0600); err != nil {
		t.Fatal(err)
	}
	var plans, mains, workers atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []Message `json:"messages"`
			Tools    []any     `json:"tools"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		last := body.Messages[len(body.Messages)-1].Content
		switch {
		case strings.HasPrefix(last, "[parallel planner]"):
			n := plans.Add(1)
			if n == 1 {
				parallelReply(w, `{"tasks":[]}`)
				return
			}
			found := false
			for _, m := range body.Messages {
				if strings.HasPrefix(m.Content, "[execution context - reference data]") && strings.Contains(m.Content, "COMPONENTS: engine, UI") {
					found = true
				}
			}
			if !found {
				t.Error("planner did not receive real tool evidence")
			}
			parallelReply(w, `{"tasks":["engine review","UI review"]}`)
		case strings.HasPrefix(last, "[parallel analysis subtask]"):
			workers.Add(1)
			if len(body.Tools) != 0 {
				t.Error("worker can execute tools")
			}
			parallelReply(w, "component analysis")
		default:
			n := mains.Add(1)
			if n == 1 {
				jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", ToolCalls: []ToolCall{readCall("read_file", `{"path":"discovery.txt"}`)}}}}})
				return
			}
			if n == 2 && !strings.HasPrefix(last, "[parallel analysis results") {
				t.Error("main did not receive late working notes")
			}
			parallelReply(w, "The two components have been analysed.")
		}
	}))
	defer provider.Close()
	input := []Message{{Role: "user", Content: task.Prompt}}
	out, chain, err := a.toolLoop(context.Background(), Settings{BaseURL: provider.URL, Model: "fixture"}, input, ProfileParams{MaxTokens: 4096}, builtinTools, task, map[string]Change{}, 0)
	if err != nil || !strings.Contains(out, "two components") || plans.Load() != 2 || workers.Load() != 2 || len(task.ToolUses) != 1 || len(chain) < 4 {
		t.Fatalf("late fanout failed: %q %v plans%d workers%d tools%d", out, err, plans.Load(), workers.Load(), len(task.ToolUses))
	}
	if task.Parallel.PlannerHistory[0].Usage.Total != 20 || task.Parallel.PlannerContext.Usage.Total != 20 || task.Parallel.MainContext == nil {
		t.Fatal("per-request context history lost")
	}
}

func TestConversationParallelAdaptiveWavesAndLifecycle(t *testing.T) {
	a, s, task := parallelFixture(t, 2)
	var plans atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []Message `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if strings.HasPrefix(body.Messages[len(body.Messages)-1].Content, "[parallel planner]") {
			n := plans.Add(1)
			parallelReply(w, fmt.Sprintf(`{"tasks":["wave%d-a","wave%d-b"]}`, n, n))
			return
		}
		parallelReply(w, "bounded note")
	}))
	defer provider.Close()
	cfg := Settings{BaseURL: provider.URL, Model: "fixture"}
	params := ProfileParams{MaxTokens: 4096}
	input := []Message{{Role: "user", Content: "Investigate incrementally"}}
	for wave := 1; wave <= 12; wave++ {
		input = append(input, Message{Role: "tool", Content: fmt.Sprintf("new discovery%d", wave)})
		var err error
		input, err = a.prepareParallel(context.Background(), s, task, cfg, input, params)
		if err != nil {
			t.Fatal(err)
		}
		expected := wave
		if expected > 3 {
			expected = 3
		}
		if len(task.Parallel.Parts) != expected*2 || len(task.Parallel.PlannerHistory) != expected-1 {
			t.Fatal("earlier wave ledger lost")
		}
	}
	input = append(input, Message{Role: "tool", Content: "next discovery"})
	_, err := a.prepareParallel(context.Background(), s, task, cfg, input, params)
	if err != nil || plans.Load() != 3 || task.Parallel.Round != 3 || task.Usage.Total != 3*60 {
		t.Fatal("probe-only checkpoints exceeded persisted planning budget")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = a.prepareParallel(ctx, s, task, cfg, input, params)
	if err != context.Canceled || plans.Load() != 3 {
		t.Fatal("cancelled checkpoint dispatched a planner")
	}
}

func TestConversationParallelUsesMainRoundBudget(t *testing.T) {
	a, _, task := parallelFixture(t, 2)
	task.ExecutionPolicy = &ExecutionPolicy{ToolMaxRounds: 2}
	task.Steer = make(chan string, 1)
	if err := os.WriteFile(filepath.Join(a.workPath, "budget.txt"), []byte("same observed fact"), 0600); err != nil {
		t.Fatal(err)
	}
	var plans, mains atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []Message `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if strings.HasPrefix(body.Messages[len(body.Messages)-1].Content, "[parallel planner]") {
			plans.Add(1)
			parallelReply(w, `{"tasks":[]}`)
			return
		}
		mains.Add(1)
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", ToolCalls: []ToolCall{readCall("read_file", `{"path":"budget.txt"}`)}}}}})
	}))
	defer provider.Close()
	_, _, err := a.toolLoop(context.Background(), Settings{BaseURL: provider.URL, Model: "fixture"}, []Message{{Role: "user", Content: "Read the fact"}}, ProfileParams{MaxTokens: 4096}, builtinTools, task, map[string]Change{}, 0)
	if err != nil || mains.Load() != 2 || plans.Load() != 2 || len(task.ToolUses) != 2 {
		t.Fatalf("main round policy not respected: %v main%d plans%d", err, mains.Load(), plans.Load())
	}
}

func TestConversationParallelPlan(t *testing.T) {
	for _, text := range []string{`{"tasks":["a","b"]}`, "```json\n{\"tasks\":[\"a\",\"b\"]}\n```"} {
		parts, err := parseParallelPlan(text, 2)
		if err != nil || len(parts) != 2 {
			t.Fatalf("parse %q: %v", text, err)
		}
	}
	for _, text := range []string{`garbage`, `{"tasks":["a","a"]}`, `{"tasks":[""]}`, `{"tasks":["a","b","c"]}`} {
		if _, err := parseParallelPlan(text, 2); err == nil {
			t.Fatalf("accepted %q", text)
		}
	}
}

func TestExplicitParallelTasks(t *testing.T) {
	prompt := "请分析以下相互独立的验收项，并分别给出建议：\n[CASE01] 身份与权限边界\n[CASE02] 备份恢复\n[CASE03] 数据一致性"
	got := explicitParallelTasks(prompt, 3)
	if len(got) != 3 || !strings.Contains(got[0], "CASE01") || !strings.Contains(got[2], "CASE03") {
		t.Fatalf("explicit tasks not extracted: %#v", got)
	}
	if got := explicitParallelTasks("以下验收项相互独立，但必须依次执行：\n[CASE01] 第一步\n[CASE02] 第二步", 2); len(got) != 0 {
		t.Fatalf("sequential work was split: %#v", got)
	}
	if got := explicitParallelTasks(strings.Replace(prompt, "[CASE02]", "[CASE01]", 1), 3); len(got) != 0 {
		t.Fatalf("duplicate labels were dispatched: %#v", got)
	}
	grouped := explicitParallelTasks(prompt, 2)
	if len(grouped) != 2 {
		t.Fatalf("expected two groups, got %#v", grouped)
	}
	combined := strings.Join(grouped, "\n")
	for _, label := range []string{"[CASE01]", "[CASE02]", "[CASE03]"} {
		if strings.Count(combined, label) != 1 {
			t.Fatalf("grouping dropped or duplicated %s: %s", label, combined)
		}
	}
	cleaned := stripExplicitParallelItems(prompt)
	if strings.Contains(cleaned, "[CASE") || !strings.Contains(cleaned, "相互独立") || !strings.Contains(cleaned, "分别给出建议") {
		t.Fatalf("worker shared context did not preserve common instructions: %q", cleaned)
	}

	numbered := "请分别完成以下相互独立的检查草案，不执行工具，每项给检查建议、证据和未知项：\n1. 身份认证与角色权限边界\n2. 备份恢复与灾难演练"
	got = explicitParallelTasks(numbered, 10)
	if len(got) != 2 || got[0] != "1. 身份认证与角色权限边界" || got[1] != "2. 备份恢复与灾难演练" {
		t.Fatalf("independent numbered tasks not extracted: %#v", got)
	}
	labels, ok := explicitParallelLabels(got)
	if !ok || labels[0] != "1." || labels[1] != "2." {
		t.Fatalf("numbered labels not preserved for assembly: %#v, %v", labels, ok)
	}
	cleaned = stripExplicitParallelItems(numbered)
	if strings.Contains(cleaned, "1. 身份认证") || strings.Contains(cleaned, "2. 备份恢复") || !strings.Contains(cleaned, "相互独立") {
		t.Fatalf("numbered task list not isolated from common worker context: %q", cleaned)
	}
	if got := explicitParallelTasks("依次完成以下相互独立的步骤：\n1. 检查身份\n2. 检查权限", 2); len(got) != 0 {
		t.Fatalf("sequential numbered steps were dispatched: %#v", got)
	}
	if got := explicitParallelTasks("请分析以下相互独立的事项：\n1. 身份认证与权限边界\n3. 备份恢复与灾难演练", 2); len(got) != 0 {
		t.Fatalf("noncontiguous numbered items were dispatched: %#v", got)
	}
	chineseNumbered := "请分别分析以下相互独立的部分：\n一、身份认证与角色权限边界\n二、备份恢复与灾难演练\n三、数据一致性与并发处理"
	got = explicitParallelTasks(chineseNumbered, 10)
	if len(got) != 3 || !strings.HasPrefix(got[0], "一、") || !strings.HasPrefix(got[2], "三、") {
		t.Fatalf("independent Chinese-numbered tasks not extracted: %#v", got)
	}
	if labels, ok := explicitParallelLabels(got); !ok || len(labels) != 3 || labels[1] != "二、" {
		t.Fatalf("Chinese-numbered labels not preserved for assembly: %#v, %v", labels, ok)
	}
	if got := explicitParallelTasks("分别分析相互独立的部分：\n一、身份认证与权限边界\n三、备份恢复与灾难演练", 2); len(got) != 0 {
		t.Fatalf("noncontiguous Chinese-numbered items were dispatched: %#v", got)
	}
	if got := explicitParallelTasks("分别分析以下相互独立的事项：\n1. 身份认证与权限边界\n二、备份恢复与灾难演练", 2); len(got) != 0 {
		t.Fatalf("mixed numbering styles were dispatched: %#v", got)
	}
}

func TestParallelExplicitNoToolDraftFastAssembly(t *testing.T) {
	a, _, task := parallelFixture(t, 2)
	task.Prompt = "请分析以下相互独立的验收项，形成建议草案，每项给两条具体检查建议、应留存证据和未知项，不执行工具；开头写明全局识别码 R-42。\n[CASE01] 身份认证\n[CASE02] 备份恢复"
	a.settings.Models = []ModelRef{{ID: "fixture", ContextWindow: 32768}}
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []Message `json:"messages"`
			Tools    []any     `json:"tools"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if len(body.Tools) != 0 {
			t.Error("parallel worker unexpectedly received tools")
		}
		last := body.Messages[len(body.Messages)-1].Content
		label := "[CASE01]"
		if strings.Contains(last, "[CASE02]") {
			label = "[CASE02]"
		}
		content := "### " + label + " 建议\n1. 检查建议：验证边界。\n2. 检查建议：核对失败路径。\n应留存证据：测试记录。\n未知项：策略待确认。"
		if strings.Contains(last, "required opening facts") {
			content = "全局识别码：R-42。\n\n" + content
		}
		calls.Add(1)
		parallelReply(w, content)
	}))
	defer provider.Close()
	input := []Message{{Role: "user", Content: task.Prompt}}
	out, chain, err := a.toolLoop(context.Background(), Settings{BaseURL: provider.URL, Model: "fixture"}, input, ProfileParams{MaxTokens: 2048}, builtinTools, task, map[string]Change{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("fast assembly should make only the two worker calls, got %d", calls.Load())
	}
	if !parallelDraftMeetsExplicitRequirements(task.Prompt, task.Parallel.Parts, out) {
		t.Fatalf("fast assembly did not satisfy explicit content requirements: %s", out)
	}
	for _, want := range []string{"R-42", "[CASE01]", "[CASE02]", "检查建议：", "证据：", "未知项："} {
		if !strings.Contains(out, want) {
			t.Fatalf("assembled answer missing %q: %s", want, out)
		}
	}
	for _, label := range []string{"[CASE01]", "[CASE02]"} {
		if strings.Count(out, label) != 1 {
			t.Fatalf("label %s was not present exactly once: %s", label, out)
		}
	}
	if len(chain) == 0 || chain[len(chain)-1].Role != "assistant" || task.Parallel.AssembledAnswer != out || task.Parallel.Phase != "completed" {
		t.Fatalf("fast assembled response not checkpointed: phase=%s chain=%#v", task.Parallel.Phase, chain)
	}
}

func TestNumberedParallelAssemblyPreservesDecimalFacts(t *testing.T) {
	if !parallelLineHasLabelHeading("## **7. Keyboard**", "7.") || parallelLineHasLabelHeading("27.4 MHz", "7.") || parallelLineHasLabelHeading("1.. Wrong", "1.") {
		t.Fatal("markdown headings and decimal boundaries were not distinguished")
	}
	parts := make([]ParallelPart, 10)
	labels := make([]string, 10)
	for i := range parts {
		labels[i] = fmt.Sprintf("%d.", i+1)
		parts[i] = ParallelPart{Status: "completed", Content: labels[i] + " Section\n检查建议：核验边界；检查建议：核验恢复。证据：日志。未知项：环境。"}
	}
	parts[0].Content = "尚未执行验证；阈值 27.4 MHz，版本 7.1。\n" + parts[0].Content
	answer, ok := assembleExplicitParallelParts(parts, labels)
	if !ok || !strings.Contains(answer, "27.4 MHz") {
		t.Fatal("decimal facts triggered an unnecessary main synthesis")
	}
	parts[0].Content += "\n## 7. Unexpected extra section"
	if _, ok := assembleExplicitParallelParts(parts, labels); ok {
		t.Fatal("cross-assignment section must still reject direct assembly")
	}
	parts[0].Content = "1. First\n1. Duplicate"
	if _, ok := assembleExplicitParallelParts(parts, labels); ok {
		t.Fatal("duplicate assigned heading must reject direct assembly")
	}
}

func TestExplicitParallelRetryReusesCompletedAssignments(t *testing.T) {
	a, s, task := parallelFixture(t, 2)
	task.Prompt = "分析以下相互独立的草案，不执行工具：\n[CASE01] 第一项分析内容\n[CASE02] 第二项分析内容"
	var first, second atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []Message `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		last := body.Messages[len(body.Messages)-1].Content
		if strings.Contains(last, "[CASE01]") {
			first.Add(1)
			parallelReply(w, "[CASE01] 已返回第一项候选内容。")
			return
		}
		if strings.Contains(last, "[CASE02]") {
			if second.Add(1) == 1 {
				http.Error(w, "temporary failure", 503)
				return
			}
			if strings.Contains(last, "Include the overall title") {
				t.Error("retry duplicated the global opening")
			}
			parallelReply(w, "[CASE02] 已返回第二项候选内容。")
			return
		}
		t.Error("explicit retry unexpectedly used the planner")
	}))
	defer provider.Close()
	cfg := Settings{BaseURL: provider.URL, Model: "fixture"}
	input := []Message{{Role: "user", Content: task.Prompt}}
	if _, err := a.prepareParallel(context.Background(), s, task, cfg, input, ProfileParams{MaxTokens: 512}); err != nil {
		t.Fatal(err)
	}
	if len(task.Parallel.Parts) != 2 || task.Parallel.AssembledAnswer != "" {
		t.Fatal("partial wave should not pass full delivery")
	}
	// The unchanged checkpoint must not automatically replay failed requests.
	if _, err := a.prepareParallel(context.Background(), s, task, cfg, input, ProfileParams{MaxTokens: 512}); err != nil {
		t.Fatal(err)
	}
	if second.Load() != 1 {
		t.Fatal("unchanged evidence retried the failed worker")
	}
	input = append(input, Message{Role: "tool", ToolCallID: "new-evidence", Content: "connection restored"})
	if _, err := a.prepareParallel(context.Background(), s, task, cfg, input, ProfileParams{MaxTokens: 512}); err != nil {
		t.Fatal(err)
	}
	if first.Load() != 1 || second.Load() != 2 || len(task.Parallel.Parts) != 3 {
		t.Fatalf("wrong retry dispatch: first=%d second=%d history=%d", first.Load(), second.Load(), len(task.Parallel.Parts))
	}
	if task.Parallel.Phase != "completed" || !strings.Contains(task.Parallel.AssembledAnswer, "第一项候选") || !strings.Contains(task.Parallel.AssembledAnswer, "第二项候选") {
		t.Fatalf("retry lost earlier completed output: %+v", task.Parallel)
	}
	// A legacy checkpoint has no per-part human scope. Preserve its candidate
	// for main review, but never directly assemble it as satisfying current scope.
	task.Parallel.Parts[0].Scope = ""
	task.Parallel.Parts[2].Status = "failed"
	input = append(input, Message{Role: "tool", ToolCallID: "another-checkpoint", Content: "retry after verification"})
	merged, err := a.prepareParallel(context.Background(), s, task, cfg, input, ProfileParams{MaxTokens: 512})
	if err != nil {
		t.Fatal(err)
	}
	if first.Load() != 1 || second.Load() != 3 || task.Parallel.AssembledAnswer != "" || task.Parallel.Phase != "merging" {
		t.Fatalf("legacy candidates bypassed main reconciliation: %+v", task.Parallel)
	}
	if !strings.Contains(merged[len(merged)-1].Content, "第一项候选") || !strings.Contains(merged[len(merged)-1].Content, "第二项候选") {
		t.Fatal("main reconciliation lost a retained candidate")
	}
}

func TestParallelDraftQualityRejectsMissingRequestedContent(t *testing.T) {
	if !parallelDraftHasTwoItems("检查建议：\n• 核验登录失效后的会话处理。\n• 核验低权限用户的访问边界。\n证据：日志。") {
		t.Fatal("two explicit recommendation bullets were rejected")
	}
	if parallelDraftHasTwoItems("检查建议：核验登录。\n证据：• 登录日志。• 访问记录；审计记录。") {
		t.Fatal("evidence bullets were mistaken for check recommendations")
	}
	prompt := "每项提供两条具体检查建议、应留存证据和未知项，不把建议称为已执行"
	good := []ParallelPart{
		{Status: "completed", Content: "[CASE01] 1. 检查A。2. 检查B。证据：记录。未知项：环境。"},
		{Status: "completed", Content: "[CASE02] ①检查A。②检查B。证据：记录。未知项：环境。"},
	}
	answer := "未执行。\n" + good[0].Content + "\n" + good[1].Content
	if !parallelDraftMeetsExplicitRequirements(prompt, good, answer) {
		t.Fatal("complete draft should pass requested-content validation")
	}
	if !parallelDraftMeetsExplicitRequirements(prompt, good, "建议为待执行事项，不代表已经检查或通过。\n"+good[0].Content+"\n"+good[1].Content) {
		t.Fatal("clear Chinese non-execution caveat should pass validation")
	}
	bad := append([]ParallelPart(nil), good...)
	bad[1].Content = "[CASE02] 只给一个检查建议。证据：记录。未知项：环境。"
	if parallelDraftMeetsExplicitRequirements(prompt, bad, answer) {
		t.Fatal("draft missing the second requested check should fail validation")
	}
	if parallelDraftMeetsExplicitRequirements(prompt, good, "[CASE01] [CASE02]") {
		t.Fatal("draft without a non-execution caveat should fail validation")
	}
	extremePrompt := "每个独立领域都提供两条具体检查建议、证据、主要风险、前置依赖、通过标准和未知项；不把建议称为已执行"
	extremePart := []ParallelPart{{Status: "completed", Content: "[CASE01] 检查建议：核验边界；检查建议：核验恢复。证据：审计日志。主要风险：权限绕过。前置依赖：测试账户。通过标准：未授权请求全部拒绝。未知项：生产策略。"}}
	if !parallelDraftMeetsExplicitRequirements(extremePrompt, extremePart, "未执行。"+extremePart[0].Content) {
		t.Fatal("complete complex section should pass risk/dependency/pass-criteria validation")
	}
	for _, missing := range []string{"主要风险", "前置依赖", "通过标准"} {
		candidate := append([]ParallelPart(nil), extremePart...)
		candidate[0].Content = strings.Replace(candidate[0].Content, missing+"：", "遗漏：", 1)
		if parallelDraftMeetsExplicitRequirements(extremePrompt, candidate, "未执行。"+candidate[0].Content) {
			t.Fatalf("section missing %s passed complex quality validation", missing)
		}
	}
}

func parallelFixture(t *testing.T, limit int) (*App, *Session, *Task) {
	t.Helper()
	a := testApp(t)
	task := &Task{Parallel: &ParallelProgress{Requested: limit}, ID: "parallel-fixture", Mode: "chat", Parallelism: limit, Prompt: "Analyze independent sections.", Status: "running", Steps: []Step{{Name: "chat", Status: "running"}}}
	s := &Session{ID: "parallel-session", Runs: []*Task{task}}
	a.sessions[s.ID] = s
	return a, s, task
}
func parallelReply(w http.ResponseWriter, text string) {
	jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": text}, "finish_reason": "stop"}}, "usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 10, "total_tokens": 20}})
}
func TestConversationParallelFanout(t *testing.T) {
	for _, limit := range []int{5, 10} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) { parallelFanout(t, limit) })
	}
}

func TestConversationParallelFitsSharedWorkerContextOnce(t *testing.T) {
	a, s, task := parallelFixture(t, 10)
	task.Prompt = "审查以下相互独立的十项，并分别给出结论：\n" + strings.Join([]string{
		"[CASE01] 身份权限", "[CASE02] 备份恢复", "[CASE03] 数据迁移", "[CASE04] 幂等重试", "[CASE05] 时钟漂移",
		"[CASE06] 并发容量", "[CASE07] 无障碍", "[CASE08] 文件编码", "[CASE09] 密钥轮换", "[CASE10] 审计证据",
	}, "\n")
	a.settings.Models = []ModelRef{{ID: "fixture", ContextWindow: 4096}}
	var summaries, workers atomic.Int32
	var workerMu sync.Mutex
	var workerContexts []string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []Message `json:"messages"`
			Tools    []any     `json:"tools"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if len(body.Tools) != 0 {
			t.Error("parallel workers and compaction must not receive tools")
		}
		if strings.Contains(body.Messages[0].Content, "压缩给定历史") {
			summaries.Add(1)
			parallelReply(w, `{"goal":"基于提供的历史证据分析验收","decisions":[],"files":[],"facts":["保留所有精确标识符"],"pending":[]}`)
			return
		}
		last := body.Messages[len(body.Messages)-1].Content
		if !strings.HasPrefix(last, "[parallel analysis subtask]") {
			t.Errorf("explicit cases unexpectedly used a planner: %q", last)
			parallelReply(w, `{"tasks":[]}`)
			return
		}
		workers.Add(1)
		workerMu.Lock()
		workerContexts = append(workerContexts, strings.Join(func() []string {
			contents := make([]string, 0, len(body.Messages))
			for _, message := range body.Messages {
				contents = append(contents, message.Content)
			}
			return contents
		}(), "\n"))
		workerMu.Unlock()
		parallelReply(w, "分析完成；未执行外部验证。")
	}))
	defer provider.Close()
	input := []Message{{Role: "system", Content: "只依据当前任务和历史证据。"}, {Role: "assistant", Content: strings.Repeat("historical evidence; retain exact identifiers. ", 1600)}, {Role: "user", Content: task.Prompt}}
	merged, err := a.prepareParallel(context.Background(), s, task, Settings{BaseURL: provider.URL, Model: "fixture"}, input, ProfileParams{MaxTokens: 512})
	if err != nil {
		t.Fatal(err)
	}
	if summaries.Load() == 0 || summaries.Load() >= int32(task.Parallelism) {
		t.Fatalf("expected shared context compression, summaries=%d", summaries.Load())
	}
	if workers.Load() != int32(task.Parallelism) || len(task.Parallel.Parts) != task.Parallelism {
		t.Fatalf("fanout incomplete: workers=%d parts=%d", workers.Load(), len(task.Parallel.Parts))
	}
	workerMu.Lock()
	defer workerMu.Unlock()
	if len(workerContexts) != task.Parallelism {
		t.Fatalf("captured %d worker contexts, want %d", len(workerContexts), task.Parallelism)
	}
	for i, context := range workerContexts {
		if strings.Contains(context, "historical evidence; retain exact identifiers. historical evidence") || !strings.Contains(context, "保留所有精确标识符") {
			t.Fatalf("worker %d did not receive the fitted shared context: bytes=%d", i, len(context))
		}
	}
	_ = merged // The original main context may exceed the merge budget and safely fall back unchanged.
}

func parallelFanout(t *testing.T, limit int) {
	a, s, task := parallelFixture(t, limit)
	started := make(chan struct{}, limit)
	release := make(chan struct{})
	var active, maximum atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []Message `json:"messages"`
			Tools    []any     `json:"tools"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if len(body.Tools) > 0 {
			t.Error("analysis must not have tools")
		}
		last := body.Messages[len(body.Messages)-1].Content
		if strings.HasPrefix(last, "[parallel planner]") {
			titles := make([]string, limit)
			for i := range titles {
				titles[i] = fmt.Sprintf("section-%d", i+1)
			}
			plan, _ := json.Marshal(map[string]any{"tasks": titles})
			parallelReply(w, string(plan))
			return
		}
		n := active.Add(1)
		defer active.Add(-1)
		for {
			old := maximum.Load()
			if n <= old || maximum.CompareAndSwap(old, n) {
				break
			}
		}
		started <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		parallelReply(w, "analysis-"+strings.Split(last, "\n")[1])
	}))
	defer provider.Close()
	cfg := Settings{BaseURL: provider.URL, Model: "fixture"}
	input := []Message{{Role: "user", Content: "Analyze independent sections."}}
	done := make(chan error, 1)
	var merged []Message
	go func() {
		var err error
		merged, err = a.prepareParallel(context.Background(), s, task, cfg, input, ProfileParams{MaxTokens: 4096})
		done <- err
	}()
	for i := 0; i < limit; i++ {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			close(release)
			t.Fatal("workers did not overlap")
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if maximum.Load() != int32(limit) {
		t.Fatalf("maximum %d", maximum.Load())
	}
	if len(merged) != 2 || !strings.Contains(merged[1].Content, fmt.Sprintf("analysis-section-%d", limit)) || !strings.Contains(merged[1].Content, "ONE coherent final answer") || !strings.Contains(merged[1].Content, "Do not redispatch") {
		t.Fatalf("bad merge: %+v", merged)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if task.Usage.Total != (limit+1)*20 || len(task.RequestSnapshots) != limit+1 || task.Parallel.Phase != "merging" || !task.ParallelPrepared {
		t.Fatalf("missing accounting/progress: %+v", task)
	}
	if task.Parallel.PlannerContext == nil || task.Parallel.PlannerContext.Usage.Total != 20 || task.Parallel.MainContext == nil || task.Parallel.MainContext.InputEstimate != estimateProviderPromptTokens(merged, a.contextToolsFor(s)) {
		t.Fatal("missing independent planner/merge context")
	}
	for _, snapshot := range task.RequestSnapshots {
		if snapshot.CompletedAt == "" || snapshot.ElapsedMillis < 0 {
			t.Fatalf("parallel request timing was not completed: purpose=%s", snapshot.Purpose)
		}
	}
	for _, part := range task.Parallel.Parts {
		if part.Context == nil || part.Context.Usage.Total != 20 || part.Context.OutputReserve != 4096 || part.Context.ContextWindow <= 0 || part.Context.Usage.Prompt >= task.Usage.Prompt || part.Context.InputEstimate != estimateProviderPromptTokens(parallelWorkerMessages(input, part.Title), nil) {
			t.Fatalf("worker context confused with aggregate %+v", part.Context)
		}
		if part.Status != "completed" {
			t.Fatal(part)
		}
		if part.StartedAt == "" || part.CompletedAt == "" || part.ElapsedMillis < 0 {
			t.Fatalf("worker timing evidence missing: %+v", part)
		}
		if _, err := time.Parse(time.RFC3339Nano, part.StartedAt); err != nil {
			t.Fatalf("invalid worker start timestamp: %q", part.StartedAt)
		}
		if _, err := time.Parse(time.RFC3339Nano, part.CompletedAt); err != nil {
			t.Fatalf("invalid worker completion timestamp: %q", part.CompletedAt)
		}
	}
}
func TestConversationParallelFallback(t *testing.T) {
	for _, plan := range []string{`{"tasks":[]}`, `bad json`, `{"tasks":["a","b","c"]}`} {
		t.Run(plan, func(t *testing.T) {
			a, s, task := parallelFixture(t, 2)
			var calls atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); parallelReply(w, plan) }))
			defer provider.Close()
			input := []Message{{Role: "user", Content: "simple"}}
			merged, err := a.prepareParallel(context.Background(), s, task, Settings{BaseURL: provider.URL, Model: "fixture"}, input, ProfileParams{MaxTokens: 4096})
			if err != nil || len(merged) != 1 || calls.Load() != 1 || task.Parallel.Phase != "fallback" {
				t.Fatalf("fallback failed %v %+v", err, task.Parallel)
			}
		})
	}
}
func TestConversationParallelCancel(t *testing.T) {
	a, s, task := parallelFixture(t, 10)
	started := make(chan struct{}, 10)
	var exited atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []Message `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if strings.Contains(body.Messages[len(body.Messages)-1].Content, "[parallel planner]") {
			parallelReply(w, `{"tasks":["a","b","c","d","e","f","g","h","i","j"]}`)
			return
		}
		started <- struct{}{}
		select {
		case <-r.Context().Done():
		case <-time.After(4 * time.Second):
		}
		exited.Add(1)
	}))
	defer provider.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := a.prepareParallel(ctx, s, task, Settings{BaseURL: provider.URL, Model: "fixture"}, []Message{{Role: "user", Content: "request"}}, ProfileParams{MaxTokens: 4096})
		done <- err
	}()
	for i := 0; i < 10; i++ {
		select {
		case <-started:
		case <-time.After(4 * time.Second):
			t.Fatal("worker did not start")
		}
	}
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation did not stop fanout")
	}
	if task.Parallel.Phase != "interrupted" || task.ParallelPrepared {
		t.Fatalf("incorrect cancellation %+v", task.Parallel)
	}
	for _, part := range task.Parallel.Parts {
		if part.Status != "cancelled" {
			t.Fatalf("cancel left unfinished worker %+v", part)
		}
	}
	_ = exited.Load()
}
func TestConversationParallelValidation(t *testing.T) {
	a := testApp(t)
	s := createSession(t, a)
	for _, value := range []int{-1, 6, 7, 8, 9, 11, 100} {
		w := request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{"mode": "chat", "prompt": "request", "parallelism": value})
		if w.Code != 400 || !strings.Contains(w.Body.String(), "parallelism") {
			t.Fatal(fmt.Sprint(w.Code, w.Body.String()))
		}
	}
	w := request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{"mode": "workflow", "prompt": "request", "parallelism": 2})
	requireStatus(t, w, 400)
}

func TestConversationParallelEndToEnd(t *testing.T) {
	for _, limit := range []int{3, 10} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) { parallelEndToEnd(t, limit) })
	}
}
func parallelEndToEnd(t *testing.T, limit int) {
	a := testApp(t)
	var analyses, merged atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []Message `json:"messages"`
			Tools    []any     `json:"tools"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		last := body.Messages[len(body.Messages)-1].Content
		switch {
		case strings.HasPrefix(last, "[parallel planner]"):
			titles := []string{"section-a", "section-b", "section-c"}
			for i := 4; i <= limit; i++ {
				titles = append(titles, fmt.Sprintf("section-%d", i))
			}
			plan, _ := json.Marshal(map[string]any{"tasks": titles})
			parallelReply(w, string(plan))
		case strings.HasPrefix(last, "[parallel analysis subtask]"):
			analyses.Add(1)
			if len(body.Tools) > 0 {
				t.Error("worker has tools")
			}
			if strings.Contains(last, "section-b") {
				http.Error(w, "mock failure", 500)
				return
			}
			parallelReply(w, "candidate "+strings.Split(last, "\n")[1])
		case strings.HasPrefix(last, "[parallel analysis results"):
			if !strings.Contains(last, "[original user request]\nCompare independent sections in supplied text.") || !strings.Contains(last, "a completion acknowledgement") {
				t.Error("merge lost original deliverable instruction")
			}
			merged.Add(1)
			if !strings.Contains(last, "candidate section-a") || !strings.Contains(last, "Status: failed") {
				t.Error("missing partial failure evidence")
			}
			parallelReply(w, "One reconciled final answer.")
		default:
			parallelReply(w, "topic")
		}
	}))
	defer provider.Close()
	a.settings = Settings{BaseURL: provider.URL, Model: "fixture"}
	s := createSession(t, a)
	// A checkpoint produced before environment scheduling retains legacy fanout.
	legacy := &Task{ID: newID(), Mode: "chat", Prompt: "Compare independent sections in supplied text.", Model: "fixture", Status: "running", Parallelism: limit, Parallel: &ParallelProgress{Requested: limit}, Steer: make(chan string, 4)}
	a.mu.Lock()
	s = a.sessions[s.ID]
	s.Runs = append(s.Runs, legacy)
	s.Messages = append(s.Messages, Message{Role: "user", Content: legacy.Prompt, SourceRunID: legacy.ID})
	a.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.execute(ctx, s, legacy, a.settings, []Message{{Role: "user", Content: legacy.Prompt}}, nil, nil, ProfileParams{MaxTokens: 4096})
	waitTaskDone(t, a, s.ID)
	response := request(a, "GET", "/api/sessions/"+s.ID, nil)
	var saved Session
	if err := json.Unmarshal(response.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.Runs) != 1 || analyses.Load() != int32(limit) || merged.Load() != 1 {
		t.Fatalf("unexpected fanout/merge %d/%d %+v", analyses.Load(), merged.Load(), saved)
	}
	task := saved.Runs[0]
	if task.Status != "completed" || task.Parallel.Phase != "completed" || task.Parallel.Parts[1].Status != "failed" || task.Steps[0].Content != "One reconciled final answer." {
		t.Fatalf("bad final task %+v", task)
	}
	if task.ContextAnchor == nil || task.ContextAnchor.Scope != "parallel_merge" || task.Parallel.MainContext == nil || task.Parallel.MainContext.Usage.Prompt != task.ContextAnchor.Usage.Prompt || task.Parallel.MainContext.Usage.Total >= task.Usage.Total {
		t.Fatal("main request context must be separate from cumulative usage")
	}
	if task.TopicContext != nil && task.TopicContext.Usage.Prompt > 0 {
		total := addUsage(task.TopicContext.Usage, task.Parallel.PlannerContext.Usage)
		for _, part := range task.Parallel.Parts {
			total = addUsage(total, part.Context.Usage)
		}
		total = addUsage(total, task.Parallel.MainContext.Usage)
		if total.Prompt != task.Usage.Prompt || total.Completion != task.Usage.Completion {
			t.Fatal("title/plan/workers/main usage did not reconcile")
		}
	}
	// Retry preserves the selected analysis limit, but starts a fresh plan.
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs/"+task.ID+"/retry", nil), 202)
	waitTaskDone(t, a, s.ID)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sessions[s.ID].Runs[1].Parallelism != limit {
		t.Fatal("retry lost parallel limit")
	}
}
func TestConversationParallelBudgetAndResume(t *testing.T) {
	a, s, task := parallelFixture(t, 5)
	a.settings.Models = []ModelRef{{ID: "fixture", ContextWindow: 32}}
	// Too little context must reject planning locally without any network request.
	input := []Message{{Role: "user", Content: "request"}}
	got, err := a.prepareParallel(context.Background(), s, task, Settings{BaseURL: "http://127.0.0.1:1", Model: "fixture"}, input, ProfileParams{MaxTokens: 4096})
	if err != nil || len(got) != 1 || len(task.RequestSnapshots) != 0 || task.Parallel.Phase != "fallback" {
		t.Fatalf("budget fallback %+v %v", task, err)
	}
	// A persisted prepared briefing must never start a second planner on resume.
	got, err = a.prepareParallel(context.Background(), s, task, Settings{}, input, ProfileParams{})
	if err != nil || len(got) != 1 || len(task.RequestSnapshots) != 0 {
		t.Fatal("prepared checkpoint replayed model requests")
	}
}

func TestConversationParallelROS2Document(t *testing.T) {
	a := testApp(t)
	var drafted, merged atomic.Int32
	started := make(chan struct{}, 5)
	release := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []Message `json:"messages"`
			Tools    []any     `json:"tools"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		last := body.Messages[len(body.Messages)-1].Content
		switch {
		case strings.HasPrefix(last, "[parallel planner]"):
			// User scope belongs in the reference-context message, not in the
			// scheduler's instruction message after role isolation.
			if !strings.Contains(last, "General technical knowledge MAY be used") || len(body.Messages) != 3 || !strings.Contains(body.Messages[1].Content, "ROS2") {
				t.Error("document planning still limited to supplied attachments")
			}
			parallelReply(w, `{"tasks":["ROS2 architecture","workspace setup","communication","implementation examples","testing and deployment"]}`)
		case strings.HasPrefix(last, "[parallel analysis subtask]"):
			if len(body.Tools) != 0 {
				t.Error("draft workers execute tools")
			}
			started <- struct{}{}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			drafted.Add(1)
			parallelReply(w, "ROS2 section draft; distribution-specific commands require verification.")
		case strings.HasPrefix(last, "[parallel analysis results"):
			if !strings.Contains(last, "[original user request]\n请写一个ROS2的文档") || !strings.Contains(last, "a completion acknowledgement") {
				t.Error("merge lost original deliverable instruction")
			}
			merged.Add(1)
			if drafted.Load() != 5 {
				t.Error("merged before five drafts finished")
			}
			parallelReply(w, "# ROS2 技术文档\n\n架构、工作区、通信、示例、测试与部署。发行版相关命令待核实。")
		default:
			parallelReply(w, "ROS2 文档")
		}
	}))
	defer provider.Close()
	a.settings = Settings{BaseURL: provider.URL, Model: "fixture"}
	s := createSession(t, a)
	legacy := &Task{ID: newID(), Mode: "chat", Prompt: "请写一个ROS2的文档", Model: "fixture", Status: "running", Parallelism: 5, Parallel: &ParallelProgress{Requested: 5}, Steer: make(chan string, 4)}
	a.mu.Lock()
	s = a.sessions[s.ID]
	s.Runs = append(s.Runs, legacy)
	s.Messages = append(s.Messages, Message{Role: "user", Content: legacy.Prompt, SourceRunID: legacy.ID})
	a.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.execute(ctx, s, legacy, a.settings, []Message{{Role: "user", Content: legacy.Prompt}}, nil, nil, ProfileParams{MaxTokens: 4096})
	for i := 0; i < 5; i++ {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			close(release)
			t.Fatal("ROS2 draft did not reach five simultaneous requests")
		}
	}
	close(release)
	waitTaskDone(t, a, s.ID)
	a.mu.Lock()
	defer a.mu.Unlock()
	task := a.sessions[s.ID].Runs[0]
	if task.Status != "completed" || task.Parallel.Phase != "completed" || len(task.Parallel.Parts) != 5 || drafted.Load() != 5 || merged.Load() != 1 {
		t.Fatalf("wrong ROS2 plan/fanout/merge: %+v drafts=%d merges=%d", task.Parallel, drafted.Load(), merged.Load())
	}
	if !strings.Contains(task.Steps[0].Content, "ROS2 技术文档") {
		t.Fatal("missing single final document")
	}
}
