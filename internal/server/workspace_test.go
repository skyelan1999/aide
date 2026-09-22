package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWorkspaceConfigCRUD(t *testing.T) {
	a := testApp(t)
	w := request(a, "GET", "/api/workspace-config", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"mode":"local"`) {
		t.Fatalf("default config: %s", w.Body.String())
	}
	// 本地路径 + 密码 + 最近路径
	requireStatus(t, request(a, "PUT", "/api/workspace-config", map[string]any{
		"workspace": map[string]any{"mode": "local", "path": "", "host": "", "port": 22, "username": "", "auth": "password"},
		"docs":     map[string]any{"path": "doc"},
		"cache":    map[string]any{"path": ".cache"},
		"password": "secret-pw",
	}), 200)
	w = request(a, "GET", "/api/workspace-config", nil)
	requireStatus(t, w, 200)
	body := w.Body.String()
	if !strings.Contains(body, `"hasPassword":true`) || strings.Contains(body, "secret-pw") {
		t.Fatalf("secret leak or missing flag: %s", body)
	}
	if !strings.Contains(body, `"doc"`) {
		t.Fatalf("recent docs missing: %s", body)
	}
	// 秘钥落在 /data 卷
	if b, err := os.ReadFile(filepath.Join(a.dataPath, wsSecretsFile)); err != nil || !strings.Contains(string(b), "secret-pw") {
		t.Fatalf("secrets file: %s %v", b, err)
	}
	// 工程目录配置文件存在且不含密码
	if b, err := os.ReadFile(filepath.Join(a.workPath, wsConfigFile)); err != nil || strings.Contains(string(b), "secret-pw") {
		t.Fatalf("config must not contain password: %s %v", b, err)
	}
	// 非法值
	for _, bad := range []map[string]any{
		{"workspace": map[string]any{"mode": "ftp"}},
		{"workspace": map[string]any{"mode": "ssh", "port": 99999}},
		{"workspace": map[string]any{"mode": "ssh", "auth": "token"}},
		{"workspace": map[string]any{"mode": "local", "path": "../escape"}},
		{"docs": map[string]any{"path": "../bad"}},
	} {
		requireStatus(t, request(a, "PUT", "/api/workspace-config", bad), 400)
	}
	// 清除密码
	requireStatus(t, request(a, "PUT", "/api/workspace-config", map[string]any{"clearPassword": true}), 200)
	w = request(a, "GET", "/api/workspace-config", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"hasPassword":false`) {
		t.Fatalf("clearPassword failed: %s", w.Body.String())
	}
}

// FR-79：配置自定义本地路径后，AI（附件/工作流）必须从配置根读到内容。
func TestLocalWorkspacePathAIReadFix(t *testing.T) {
	a := testApp(t)
	if err := os.MkdirAll(filepath.Join(a.workPath, "proj"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.workPath, "proj", "hello.txt"), []byte("configured-root-content"), 0644); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, "PUT", "/api/workspace-config", map[string]any{
		"workspace": map[string]any{"mode": "local", "path": "proj"},
	}), 200)
	// 文件面板读配置根下的文件
	w := request(a, "GET", "/api/file?root=workspace&path=hello.txt", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), "configured-root-content") {
		t.Fatalf("file read from configured root failed: %s", w.Body.String())
	}
	// 编辑器保存写配置根
	requireStatus(t, request(a, "PUT", "/api/file", map[string]string{"path": "new.txt", "content": "from-editor"}), 200)
	if b, err := os.ReadFile(filepath.Join(a.workPath, "proj", "new.txt")); err != nil || string(b) != "from-editor" {
		t.Fatalf("write to configured root failed: %s %v", b, err)
	}
	// AI 附件读取（mock provider 收到附件内容）
	var captured string
	provider := newMockProvider(t, func(body map[string]any) string {
		msgs, _ := body["messages"].([]any)
		for _, m := range msgs {
			if mm, ok := m.(map[string]any); ok {
				captured += mm["content"].(string)
			}
		}
		return "ok"
	})
	defer provider.Close()
	a.settings = Settings{BaseURL: provider.URL, Model: "test"}
	s := createSession(t, a)
	w = request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{
		"mode": "chat", "prompt": "读一下文件",
		"attachments": []Attachment{{Root: "workspace", Path: "hello.txt"}},
	})
	requireStatus(t, w, 202)
	waitTaskDone(t, a, s.ID)
	if !strings.Contains(captured, "configured-root-content") {
		t.Fatalf("AI 未读到配置根内容（FR-79 回归）: %s", captured)
	}
	// 系统提示必须注入工作目录快照（路径 + 目录清单）
	if !strings.Contains(captured, "当前工作目录") || !strings.Contains(captured, "hello.txt") {
		t.Fatalf("AI 系统提示缺工作目录快照: %s", captured)
	}
	// 恢复默认根，避免影响其他用例
	requireStatus(t, request(a, "PUT", "/api/workspace-config", map[string]any{
		"workspace": map[string]any{"mode": "local", "path": ""},
	}), 200)
}

// 远程模式分发：mock ssh/sftp 替身。
func TestRemoteWorkspaceDispatch(t *testing.T) {
	a := testApp(t)
	dir := t.TempDir()
	socket := sshControlSocket // 全局常量，mock 用固定 socket
	sshStub := filepath.Join(dir, "ssh")
	sftpStub := filepath.Join(dir, "sftp")
	sshLog := filepath.Join(dir, "ssh.log")
	sftpLog := filepath.Join(dir, "sftp.log")
	marker := filepath.Join(dir, "master-up")
	writeStub(t, sshStub, `#!/bin/sh
echo "$@" >> "`+sshLog+`"
for arg in "$@"; do
  if [ "$arg" = "-O" ]; then mode=1; fi
  if [ "$mode" = "1" ] && [ -z "$op" ] && [ "$arg" != "-O" ]; then op="$arg"; fi
done
case "$op" in
  check) [ -f "`+marker+`" ] && exit 0 || exit 1 ;;
  exit) rm -f "`+marker+`"; exit 0 ;;
  *) ;;
esac
if [ "$1" = "-fNM" ]; then touch "`+marker+`"; exit 0; fi
last=""
for arg in "$@"; do last="$arg"; done
echo "remote:$last"
exit 0
`)
	writeStub(t, sftpStub, `#!/bin/sh
batch=$(cat)
printf '%s\n' "$batch" >> "`+sftpLog+`"
case "$batch" in
  *"ls -l"*)
    echo 'drwxr-xr-x 1 user group 0 Jan 1 00:00 src'
    echo '-rw-r--r-- 1 user group 12 Jan 1 00:00 readme.md'
    ;;
  *"get "*"x.txt"*)
    echo "Couldn't stat remote file: No such file" >&2
    exit 1
    ;;
  *"get "*)
    dest=$(printf '%s\n' "$batch" | sed -n 's/^get ".*" "\(.*\)"/\1/p')
    [ -n "$dest" ] && echo 'remote-content' > "$dest"
    ;;
esac
exit 0
`)
	_ = socket
	a.sshBin = sshStub
	a.sftpBin = sftpStub
	requireStatus(t, request(a, "PUT", "/api/workspace-config", map[string]any{
		"workspace": map[string]any{"mode": "ssh", "path": "/srv/app", "host": "remote.example", "port": 22, "username": "deploy", "auth": "none"},
	}), 200)
	// 远程文件列表
	w := request(a, "GET", "/api/files?root=workspace&path=.", nil)
	requireStatus(t, w, 200)
	body := w.Body.String()
	if !strings.Contains(body, `"name":"src"`) || !strings.Contains(body, `"name":"readme.md"`) {
		t.Fatalf("remote list: %s", body)
	}
	var items []map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &items)
	if len(items) != 2 || items[0]["dir"] != true || items[1]["dir"] != false {
		t.Fatalf("remote list parse: %v", items)
	}
	// 远程读（sftp get 输出到 stdout 的是 ls 假数据，读返回该内容——仅验证走 sftp 通道）
	w = request(a, "GET", "/api/file?root=workspace&path=readme.md", nil)
	requireStatus(t, w, 200)
	// 远程写
	requireStatus(t, request(a, "PUT", "/api/file", map[string]string{"path": "x.txt", "content": "hi", "hash": ""}), 200)
	if b, _ := os.ReadFile(sftpLog); !strings.Contains(string(b), "put -P") || !strings.Contains(string(b), "rename") {
		t.Fatalf("sftp write batch: %s", string(b))
	}
	// 远程命令（单会话）
	w = request(a, "POST", "/api/command", map[string]string{"command": "echo hi"})
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), "remote:echo hi") {
		t.Fatalf("remote command output: %s", w.Body.String())
	}
	// 会话只建立一次（master 复用）
	if b, _ := os.ReadFile(sshLog); strings.Count(string(b), "-fNM") != 1 {
		t.Fatalf("master session must be established exactly once: %s", string(b))
	}
	// 切回本地
	requireStatus(t, request(a, "PUT", "/api/workspace-config", map[string]any{
		"workspace": map[string]any{"mode": "local", "path": ""},
	}), 200)
}

func writeStub(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0700); err != nil {
		t.Fatal(err)
	}
}

func createSession(t *testing.T, a *App) *Session {
	t.Helper()
	w := request(a, "POST", "/api/sessions", map[string]string{})
	requireStatus(t, w, 201)
	var s Session
	_ = json.Unmarshal(w.Body.Bytes(), &s)
	return &s
}

func waitTaskDone(t *testing.T, a *App, sessionID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		w := request(a, "GET", "/api/sessions/"+sessionID, nil)
		var s Session
		_ = json.Unmarshal(w.Body.Bytes(), &s)
		if s.Runs[0].Status != "running" {
			time.Sleep(100 * time.Millisecond) // 等待 execute 协程完成最终 save，避免与 TempDir 清理竞态
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("task did not finish")
}

type providerServer struct{ *httptest.Server }

func newMockProvider(t *testing.T, reply func(body map[string]any) string) *providerServer {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(b, &body)
		content := reply(body)
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: content}}}})
	}))
	return &providerServer{srv}
}
