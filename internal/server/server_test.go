package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
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

func testApp(t *testing.T) *App {
	t.Helper()
	root := t.TempDir()
	a, err := New(filepath.Join(root, "work"), filepath.Join(root, "ref"), filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	return a
}
func request(a *App, method, path string, body any) *httptest.ResponseRecorder {
	var b bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&b).Encode(body)
	}
	r := httptest.NewRequest(method, path, &b)
	r.Header.Set("Authorization", "Bearer "+a.token)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	return w
}
func requireStatus(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Fatalf("status %d want %d: %s", w.Code, want, w.Body.String())
	}
}
func TestAuthenticationAndOrigin(t *testing.T) {
	a := testApp(t)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/config", nil))
	requireStatus(t, w, 401)
	r := httptest.NewRequest("PUT", "/api/settings", strings.NewReader(`{}`))
	r.Header.Set("Authorization", "Bearer "+a.token)
	r.Header.Set("Origin", "https://evil.example")
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	requireStatus(t, w, 403)
	for _, path := range []string{"/", "/app.js", "/healthz"} {
		requireStatus(t, request(a, "GET", path, nil), 200)
	}
}
func TestFileBoundariesAndConflicts(t *testing.T) {
	a := testApp(t)
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(a.workPath, "escape")); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"../secret", "escape", ".git/config", ".env"} {
		requireStatus(t, request(a, "GET", "/api/file?path="+p, nil), 400)
	}
	body := map[string]string{"path": "nested/test.txt", "content": "hello"}
	requireStatus(t, request(a, "PUT", "/api/file", body), 200)
	requireStatus(t, request(a, "PUT", "/api/file", body), 409)
	body["hash"] = hash([]byte("hello"))
	body["content"] = "updated"
	requireStatus(t, request(a, "PUT", "/api/file", body), 200)
	b, err := os.ReadFile(filepath.Join(a.workPath, "nested/test.txt"))
	if err != nil || string(b) != "updated" {
		t.Fatalf("host write: %s %v", b, err)
	}
	if err := a.reference.WriteFile("ref.txt", []byte("reference"), 0600); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, "GET", "/api/file?root=context&path=ref.txt", nil), 200)
	requireStatus(t, request(a, "PUT", "/api/file", map[string]string{"path": "/context/ref.txt", "content": "bad"}), 400)
}
func TestSettingsNeverReturnKey(t *testing.T) {
	a := testApp(t)
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{"baseURL": "https://api.example.com/v1", "model": "model", "apiKey": "secret-test-key"}), 200)
	w := request(a, "GET", "/api/config", nil)
	requireStatus(t, w, 200)
	if strings.Contains(w.Body.String(), "secret-test-key") {
		t.Fatal("key leaked")
	}
	if mode, err := os.Stat(SettingsPath(a.dataPath)); err != nil || mode.Mode().Perm() != 0600 {
		t.Fatal("settings permissions")
	}
}
func TestWorkflowApprovalConflictAndPersistence(t *testing.T) {
	a := testApp(t)
	var formats sync.Map // step name -> response_format type
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer fake" {
			t.Error("provider contract")
		}
		var body struct {
			Messages       []Message         `json:"messages"`
			ResponseFormat map[string]string `json:"response_format"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if len(body.Messages) < 2 {
			t.Error("missing messages")
		}
		// summarizeTopic 与任务并发执行：不能按调用序号分派，按消息内容识别步骤。
		// 注意优先级：plan/propose 指令会作为历史回显在后继步骤调用里，
		// 而 review 的特征指令只出现在 review 调用；先匹配 review，再 propose，最后 plan。
		joined := ""
		for _, m := range body.Messages {
			joined += m.Content
		}
		step := "review"
		content := "审查结束：未执行测试。"
		switch {
		case strings.Contains(joined, "主题短语"):
			step = "topic"
			content = "更新主题"
		case strings.Contains(joined, "审查上述计划"):
			step = "review"
			content = "审查结束：未执行测试。"
		case strings.Contains(joined, "Generate an implementation proposal"):
			step = "propose"
			content = `{"summary":"change","files":[{"path":"hello.txt","content":"new"}],"commands":["cat hello.txt"]}`
		case strings.Contains(joined, "制定简短的实施计划"):
			step = "plan"
			content = "计划：修改 hello.txt，再验证内容。"
		}
		formats.Store(step, body.ResponseFormat["type"])
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: content}}}})
	}))
	defer provider.Close()
	a.settings = Settings{BaseURL: provider.URL, Model: "test", APIKey: "fake"}
	if err := a.workspace.WriteFile("hello.txt", []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	w := request(a, "POST", "/api/sessions", map[string]string{"title": "test"})
	requireStatus(t, w, 201)
	var s Session
	_ = json.Unmarshal(w.Body.Bytes(), &s)
	w = request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{"mode": "workflow", "prompt": "update hello", "attachments": []Attachment{{Root: "workspace", Path: "hello.txt"}}})
	requireStatus(t, w, 202)
	var task Task
	_ = json.Unmarshal(w.Body.Bytes(), &task)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		w = request(a, "GET", "/api/sessions/"+s.ID, nil)
		_ = json.Unmarshal(w.Body.Bytes(), &s)
		task = *s.Runs[0]
		if task.Status != "running" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if task.Status != "awaiting_approval" {
		t.Fatalf("task: %+v", task)
	}
	// propose 步骤必须强制 JSON 输出（FR-23 可靠性）；plan 不强制
	if f, _ := formats.Load("propose"); f != "json_object" {
		t.Fatalf("propose response_format = %v, want json_object", f)
	}
	if f, _ := formats.Load("plan"); f == "json_object" {
		t.Fatal("plan 步骤不应强制 json_object")
	}
	// 主题总结并发执行：标题异步落库
	for i := 0; i < 300; i++ {
		w = request(a, "GET", "/api/sessions/"+s.ID, nil)
		_ = json.Unmarshal(w.Body.Bytes(), &s)
		if s.Title == "更新主题" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if s.Title != "更新主题" {
		t.Fatalf("topic title: %s", s.Title)
	}
	b, _ := a.workspace.ReadFile("hello.txt")
	if string(b) != "old" {
		t.Fatal("changed without approval")
	}
	if err := a.workspace.WriteFile("hello.txt", []byte("external"), 0644); err != nil {
		t.Fatal(err)
	}
	url := "/api/sessions/" + s.ID + "/runs/" + task.ID + "/apply"
	requireStatus(t, request(a, "POST", url, map[string]any{}), 409)
	if err := a.workspace.WriteFile("hello.txt", []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, "POST", url, map[string]any{}), 200)
	b, _ = a.workspace.ReadFile("hello.txt")
	if string(b) != "new" {
		t.Fatal("change not applied")
	}
	a2, err := New(a.workPath, a.reference.Name(), a.dataPath)
	if err != nil {
		t.Fatal(err)
	}
	defer a2.Close()
	if !a2.sessions[s.ID].Runs[0].Applied {
		t.Fatal("state not persisted")
	}
}
func TestRejectUnattachedExistingFile(t *testing.T) {
	a := testApp(t)
	_ = a.workspace.WriteFile("existing", []byte("important"), 0644)
	if err := a.acceptProposal(&Session{ID: newID()}, &Task{}, `{"files":[{"path":"existing","content":"replace"}]}`, map[string]Change{}); err == nil {
		t.Fatal("unattached overwrite accepted")
	}
}
func TestProviderErrorsAndCancellation(t *testing.T) {
	p := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte("secret upstream data"))
	}))
	defer p.Close()
	_, _, _, err := complete(context.Background(), Settings{BaseURL: p.URL, Model: "test"}, nil, ProfileParams{}, nil, nil)
	if err == nil || strings.Contains(err.Error(), "secret upstream") {
		t.Fatal("provider error handling")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, _, err := complete(ctx, Settings{BaseURL: p.URL, Model: "test"}, nil, ProfileParams{}, nil, nil); err == nil {
		t.Fatal("cancel ignored")
	}
}
func TestCommandExecutionAndExitCode(t *testing.T) {
	a := testApp(t)
	w := request(a, "POST", "/api/command", map[string]string{"command": "printf 'hello-command'; exit 7"})
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), "hello-command") || !strings.Contains(w.Body.String(), `"code":7`) {
		t.Fatalf("output: %s", w.Body.String())
	}
	w = request(a, "POST", "/api/command", map[string]string{"command": "printf '%s' \"${AI_API_KEY-unset}\""})
	if !strings.Contains(w.Body.String(), "unset") {
		t.Fatal("provider key in shell environment")
	}
	requireStatus(t, request(a, "POST", "/api/command", map[string]string{"command": "pwd", "cwd": "../"}), 400)
}
func TestRestartMarksInterrupted(t *testing.T) {
	a := testApp(t)
	s := &Session{ID: newID(), Runs: []*Task{{ID: newID(), Status: "running"}}}
	if err := a.save(s); err != nil {
		t.Fatal(err)
	}
	a2, err := New(a.workPath, a.reference.Name(), a.dataPath)
	if err != nil {
		t.Fatal(err)
	}
	defer a2.Close()
	if a2.sessions[s.ID].Runs[0].Status != "interrupted" {
		t.Fatal("missing recovery state")
	}
}
func TestChatHistoryAndCancelEndpoint(t *testing.T) {
	a := testApp(t)
	entered := make(chan struct{})
	var calls atomic.Int32
	p := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		n := calls.Add(1)
		if n == 1 {
			jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: "主题标题"}}}})
			return
		}
		if n == 2 {
			jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: "first reply"}}}})
			return
		}
		close(entered)
		<-r.Context().Done()
	}))
	defer p.Close()
	a.settings = Settings{BaseURL: p.URL, Model: "test"}
	w := request(a, "POST", "/api/sessions", map[string]string{})
	var s Session
	_ = json.Unmarshal(w.Body.Bytes(), &s)
	url := "/api/sessions/" + s.ID + "/runs"
	requireStatus(t, request(a, "POST", url, map[string]string{"mode": "chat", "prompt": "hello"}), 202)
	for i := 0; i < 200; i++ {
		w = request(a, "GET", "/api/sessions/"+s.ID, nil)
		_ = json.Unmarshal(w.Body.Bytes(), &s)
		if s.Runs[0].Status != "running" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(s.Messages) != 2 || s.Messages[1].Content != "first reply" {
		t.Fatal("chat not saved")
	}
	// summarizeTopic 与任务并发执行：标题异步落库，轮询等待（响应体标题更新前有极小窗口）
	for i := 0; i < 300; i++ {
		w = request(a, "GET", "/api/sessions/"+s.ID, nil)
		_ = json.Unmarshal(w.Body.Bytes(), &s)
		if s.Title == "主题标题" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if s.Title != "主题标题" {
		t.Fatalf("topic summary title: %s", s.Title)
	}
	w = request(a, "POST", url, map[string]string{"mode": "chat", "prompt": "second"})
	requireStatus(t, w, 202)
	var task Task
	_ = json.Unmarshal(w.Body.Bytes(), &task)
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("provider not called")
	}
	requireStatus(t, request(a, "POST", url+"/"+task.ID+"/cancel", map[string]any{}), 200)
	for i := 0; i < 200; i++ {
		w = request(a, "GET", "/api/sessions/"+s.ID, nil)
		_ = json.Unmarshal(w.Body.Bytes(), &s)
		if s.Runs[1].Status != "running" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if s.Runs[1].Status != "cancelled" {
		t.Fatal("cancel state")
	}
}
