package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func webReadTestTools() []any {
	return []any{map[string]any{"type": "function", "function": map[string]any{"name": "browser_read", "parameters": map[string]any{"type": "object"}}}}
}

func TestWebResearchRoutingAndAttemptGate(t *testing.T) {
	for _, prompt := range []string{"你查下大疆官网，先把基本资料总结拿下来", "打开 https://example.com 并读取正文", "research the official website", "请查官网 Avata 360 参数"} {
		if !requestsWebResearch(prompt) || !needsWebReadAttempt(prompt, "chat", webReadTestTools(), nil) {
			t.Errorf("research not routed: %s", prompt)
		}
	}
	for _, prompt := range []string{"写代码实现浏览器插件", "不要联网，请总结这份官网附件", "debug website code", "你好", "修复官网页面样式"} {
		if requestsWebResearch(prompt) {
			t.Errorf("non-research incorrectly routed: %s", prompt)
		}
	}
	prompt := "查官网并总结"
	if needsWebReadAttempt(prompt, "chat", nil, nil) || needsWebReadAttempt(prompt, "workflow", webReadTestTools(), nil) {
		t.Fatal("disabled tools or planning must not trigger browsing")
	}
	if !needsWebReadAttempt(prompt, "chat", webReadTestTools(), []ToolUse{{Tool: "web_search"}}) {
		t.Fatal("a search snippet is not a page read")
	}
	if needsWebReadAttempt(prompt, "chat", webReadTestTools(), []ToolUse{{Tool: "browser_read", Result: "域名不允许"}}) {
		t.Fatal("real failed attempt must be explainable without another forced retry")
	}
}

func TestWebResearchCurrentSchemasOverrideOldHistory(t *testing.T) {
	a := testApp(t)
	a.pluginSurface = []byte(`{"plugins":[{"id":"browser-control","tools":[{"name":"browser_read","executable":true,"parameters":{"type":"object"}}]}]}`)
	s := &Session{Messages: []Message{{Role: "assistant", Content: "我没有浏览器控制工具"}}}
	preview := a.buildContextPreview(s, "查官网基本资料", "chat", "", nil, a.settings, ProfileParams{}, true)
	last := preview.Messages[len(preview.Messages)-1].Content
	if !strings.Contains(last, "本轮提供 browser_read") || !strings.Contains(last, "历史回复") || !strings.Contains(last, "网页研究流程") {
		t.Fatal("current capability policy missing after old history")
	}
	a.settings.DisabledTools = []string{"browser_read"}
	preview = a.buildContextPreview(s, "查官网基本资料", "chat", "", nil, a.settings, ProfileParams{}, true)
	last = preview.Messages[len(preview.Messages)-1].Content
	if strings.Contains(last, "本轮提供 browser_read") || strings.Contains(last, "网页研究流程") {
		t.Fatal("disabled tool incorrectly advertised as current capability")
	}
}

func TestWebResearchNoReadAnswerGetsOneCorrection(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if calls.Add(1) == 2 && !strings.Contains(body.Messages[len(body.Messages)-1].Content, "本轮执行校验") {
			t.Error("second round lacks actual-read correction")
		}
		sseRaw(w, w.(http.Flusher), `{"choices":[{"delta":{"content":"我查了，但没有浏览器能力"},"finish_reason":"stop"}]}`)
	}))
	defer srv.Close()
	a := testApp(t)
	task := &Task{ID: newID(), Mode: "chat", Prompt: "查官网并总结", Steer: make(chan string, 4), Steps: []Step{{Name: "chat"}}}
	registerHarnessFixtureTask(t, a, task)
	out, _, err := a.toolLoop(context.Background(), Settings{BaseURL: srv.URL, Model: "test"}, []Message{{Role: "user", Content: task.Prompt}}, ProfileParams{}, webReadTestTools(), task, nil, 0)
	if err != nil || calls.Load() != 2 || out != webReadNotCompleted {
		t.Fatalf("unexpected no-read fallback: calls=%d out=%s err=%v", calls.Load(), out, err)
	}
}

// A local document request must stay local even when browser tools are present.
func TestWebResearchLocalDocumentRouting(t *testing.T) {
	local := []string{
		"读取 before/spec.pdf 和 after/spec.pdf，调用 Compare 比较并生成报告",
		"执行 Compare 0.2.0 的真实 AI 端到端验收。只读取工作区样例；分别比较 spec.docx、budget.xlsx、brief.pptx、spec.pdf；使用 list_sources 确认来源，汇总结果并生成报告。",
		"打开本地 PDF 并总结这份文档",
		"请核实附件 PDF 的端口参数",
		"读取 system-docs 下的报告.pdf",
		"比较 /Users/test/Downloads/a.pdf 与 /Users/test/Downloads/b.pdf",
		"read and summarize the attached PDF",
		"open local report.PDF and check its contents",
		"compare report.pdf and report-v2.pdf, then summarize the differences",
		"Read the docx, xlsx and pptx files and generate a PDF report",
	}
	for _, prompt := range local {
		t.Run(prompt, func(t *testing.T) {
			if requestsWebResearch(prompt) || needsWebReadAttempt(prompt, "chat", webReadTestTools(), nil) {
				t.Fatal("local document request incorrectly requires a web read")
			}
			if strings.Contains(runtimeCapabilityInstruction(webReadTestTools(), prompt, "chat"), "【网页研究流程】") {
				t.Fatal("local document request receives webpage research workflow")
			}
		})
	}
	for _, prompt := range []string{
		"读取 https://example.com/manual.pdf 并总结",
		"read https://example.com/manual.PDF and summarize its contents",
		"查官网最新 PDF 资料并总结",
		"比较本地 PDF，再访问官网核对最新参数",
	} {
		if !requestsWebResearch(prompt) || !needsWebReadAttempt(prompt, "chat", webReadTestTools(), nil) {
			t.Errorf("explicit web research lost its read gate: %s", prompt)
		}
	}
}

func TestWebResearchLocalComparisonAnswerIsPreserved(t *testing.T) {
	var calls atomic.Int32
	const answer = "本地 PDF 比较完成，已定位文字差异。"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		delta, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]string{"content": answer}, "finish_reason": "stop"}}})
		sseRaw(w, w.(http.Flusher), string(delta))
	}))
	defer srv.Close()
	a := testApp(t)
	task := &Task{ID: newID(), Mode: "chat", Prompt: "读取工作区 before/spec.pdf 和 after/spec.pdf，用 Compare 比较并生成报告", Steer: make(chan string, 4), Steps: []Step{{Name: "chat"}}}
	registerHarnessFixtureTask(t, a, task)
	out, _, err := a.toolLoop(context.Background(), Settings{BaseURL: srv.URL, Model: "test"}, []Message{{Role: "user", Content: task.Prompt}}, ProfileParams{}, webReadTestTools(), task, nil, 0)
	if err != nil || calls.Load() != 1 || out != answer {
		t.Fatalf("local answer replaced or retried: calls=%d out=%s err=%v", calls.Load(), out, err)
	}
}
