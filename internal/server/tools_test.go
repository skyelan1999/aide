package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// toolProvider 返回带 tool_calls 的模型响应序列。
type toolProvider struct {
	*httptest.Server
	requests [][]Message
	script   []func() (string, []ToolCall)
	idx      int
}

func (p *toolProvider) requestsEmpty() int { return len(p.requests) }

func newToolProvider(t *testing.T, script []func() (string, []ToolCall)) *toolProvider {
	p := &toolProvider{script: script}
	p.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []Message `json:"messages"`
			Tools    []any     `json:"tools"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		first := len(p.requests) == 0
		p.requests = append(p.requests, body.Messages)
		if !first && len(body.Tools) == 0 {
			t.Error("tools not sent to provider")
		}
		var content string
		var calls []ToolCall
		if first {
			content, calls = "主题", nil // 首个调用是任务主题总结（FR-88）
		} else if p.idx < len(p.script) {
			content, calls = p.script[p.idx]()
			p.idx++
		}
		msg := map[string]any{"role": "assistant", "content": content}
		if calls != nil {
			msg["tool_calls"] = calls
		}
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": msg}}})
	}))
	return p
}

func readCall(name, args string) ToolCall {
	c := ToolCall{ID: "call-1", Type: "function"}
	c.Function.Name = name
	c.Function.Arguments = args
	return c
}

// FR-33 工具闭环：read_file 直接执行，结果回传给模型。
func TestToolLoopReadFile(t *testing.T) {
	a := testApp(t)
	if err := a.workspace.WriteFile("hello.txt", []byte("tool-readable-content"), 0644); err != nil {
		t.Fatal(err)
	}
	provider := newToolProvider(t, []func() (string, []ToolCall){
		func() (string, []ToolCall) { return "", []ToolCall{readCall("read_file", `{"path":"hello.txt"}`)} },
		func() (string, []ToolCall) { return "已读取文件，内容是 tool-readable-content。", nil },
	})
	defer provider.Close()
	a.settings = Settings{BaseURL: provider.URL, Model: "test"}
	s := createSession(t, a)
	w := request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{"mode": "chat", "prompt": "读一下 hello.txt"})
	requireStatus(t, w, 202)
	waitTaskDone(t, a, s.ID)
	w = request(a, "GET", "/api/sessions/"+s.ID, nil)
	requireStatus(t, w, 200)
	var sess Session
	_ = json.Unmarshal(w.Body.Bytes(), &sess)
	if sess.Runs[0].Status != "completed" {
		t.Fatalf("task: %+v", sess.Runs[0])
	}
	// 第二轮请求必须包含 tool 角色消息与文件内容
	found := false
	for _, req := range provider.requests {
		for _, m := range req {
			if m.Role == "tool" && strings.Contains(m.Content, "tool-readable-content") {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("tool result not passed back to model: %+v", provider.requests)
	}
}

// write_file / run_shell 只生成提案（P2）：任务进入 awaiting_approval。
func TestToolLoopWriteAndShellProposals(t *testing.T) {
	a := testApp(t)
	provider := newToolProvider(t, []func() (string, []ToolCall){
		func() (string, []ToolCall) {
			return "", []ToolCall{
				readCall("write_file", `{"path":"new.txt","content":"hello-from-tool"}`),
				readCall("run_shell", `{"command":"go test ./..."}`),
			}
		},
		func() (string, []ToolCall) { return "提案已生成，请用户批准。", nil },
	})
	defer provider.Close()
	a.settings = Settings{BaseURL: provider.URL, Model: "test"}
	s := createSession(t, a)
	w := request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{"mode": "chat", "prompt": "创建 new.txt 并测试"})
	requireStatus(t, w, 202)
	waitTaskDone(t, a, s.ID)
	w = request(a, "GET", "/api/sessions/"+s.ID, nil)
	requireStatus(t, w, 200)
	var sess Session
	_ = json.Unmarshal(w.Body.Bytes(), &sess)
	task := sess.Runs[0]
	if task.Status != "awaiting_approval" || len(task.Files) != 1 || task.Files[0].Path != "new.txt" || task.Files[0].Content != "hello-from-tool" {
		t.Fatalf("write proposal: %+v", task)
	}
	if len(task.Commands) != 1 || task.Commands[0] != "go test ./..." {
		t.Fatalf("command proposal: %+v", task)
	}
	// 批准应用 → 写入
	w = request(a, "POST", "/api/sessions/"+s.ID+"/runs/"+task.ID+"/apply", map[string]any{})
	requireStatus(t, w, 200)
	b, err := os.ReadFile(filepath.Join(a.workPath, "new.txt"))
	if err != nil || string(b) != "hello-from-tool" {
		t.Fatalf("applied: %s %v", b, err)
	}
}

// 插件工具（协议 v1.1）：上传带 handler 的插件 → 模型调用 → 宿主执行。
func TestPluginToolExecution(t *testing.T) {
	a := testApp(t)
	code := `'use strict';
module.exports = {
  name: 'greet',
  apply(ctx) {
    ctx.tool({ name: 'greet_tool', description: '按名字问好', handler: (args, api) => ({ text: 'hello ' + args.name }) });
  },
};
`
	requireStatus(t, request(a, "POST", "/api/plugins", map[string]any{"id": "greet", "name": "问好", "code": code}), 201)
	// surface 标记可执行
	w := request(a, "GET", "/api/plugin-surface", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"executable":true`) {
		t.Fatalf("surface executable: %s", w.Body.String())
	}
	// 直接调用
	result, err := a.callPluginTool("greet", "greet_tool", map[string]any{"name": "world"})
	if err != nil {
		t.Fatal(err)
	}
	if m, ok := result.(map[string]any); !ok || m["text"] != "hello world" {
		t.Fatalf("plugin tool result: %v", result)
	}
	// 模型循环路由到插件工具
	provider := newToolProvider(t, []func() (string, []ToolCall){
		func() (string, []ToolCall) { return "", []ToolCall{readCall("greet_tool", `{"name":"aide"}`)} },
		func() (string, []ToolCall) { return "问好完成。", nil },
	})
	defer provider.Close()
	a.settings = Settings{BaseURL: provider.URL, Model: "test"}
	s := createSession(t, a)
	w = request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{"mode": "chat", "prompt": "用插件工具问好"})
	requireStatus(t, w, 202)
	waitTaskDone(t, a, s.ID)
	found := false
	for _, req := range provider.requests {
		for _, m := range req {
			if m.Role == "tool" && strings.Contains(m.Content, "hello aide") {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("plugin tool result not in loop: %+v", provider.requests)
	}
}

// 未附加的现有文件：write_file 工具必须被拒绝（P3）。
func TestToolLoopWriteUnattachedRejected(t *testing.T) {
	a := testApp(t)
	_ = a.workspace.WriteFile("existing.txt", []byte("important"), 0644)
	provider := newToolProvider(t, []func() (string, []ToolCall){
		func() (string, []ToolCall) { return "", []ToolCall{readCall("write_file", `{"path":"existing.txt","content":"evil"}`)} },
		func() (string, []ToolCall) { return "收到。", nil },
	})
	defer provider.Close()
	a.settings = Settings{BaseURL: provider.URL, Model: "test"}
	s := createSession(t, a)
	w := request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{"mode": "chat", "prompt": "改 existing.txt"})
	requireStatus(t, w, 202)
	waitTaskDone(t, a, s.ID)
	var sess Session
	w = request(a, "GET", "/api/sessions/"+s.ID, nil)
	_ = json.Unmarshal(w.Body.Bytes(), &sess)
	if len(sess.Runs[0].Files) != 0 {
		t.Fatalf("unattached write must not create proposal: %+v", sess.Runs[0].Files)
	}
	if b, _ := a.workspace.ReadFile("existing.txt"); string(b) != "important" {
		t.Fatal("file must remain unchanged")
	}
}
