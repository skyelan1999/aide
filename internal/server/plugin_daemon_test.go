package server

/* 协议 v1.2 daemon 宿主测试。依赖 plugins/testdata/mock-daemon/ 与系统 node。
 * 若在无 node 的环境（如裸 golang 镜像）运行，相关用例 t.Skip 记 NOT_RUN。
 */

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// findMockDaemon 从测试工作目录向上查找 plugins/testdata/mock-daemon。
func findMockDaemon(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		cand := filepath.Join(dir, "plugins", "testdata", "mock-daemon")
		if _, err := os.Stat(filepath.Join(cand, "index.js")); err == nil {
			return cand
		}
		dir = filepath.Dir(dir)
	}
	t.Skip("找不到 plugins/testdata/mock-daemon，跳过 daemon 测试")
	return ""
}

// installMockDaemon 把测试插件装入当前 App 的 pluginsPath 与注册表。
func installMockDaemon(t *testing.T, a *App, id string, enabled bool) {
	t.Helper()
	src := findMockDaemon(t)
	b, err := os.ReadFile(filepath.Join(src, "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	dir := filepath.Join(a.pluginsPath, id)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.js"), b, 0644); err != nil {
		t.Fatal(err)
	}
	a.pluginRegistry.Plugins = append(a.pluginRegistry.Plugins, PluginManifest{
		ID: id, Name: "Mock Daemon", Main: "index.js", Daemon: true, Enabled: enabled,
	})
}

func TestDaemonStartStop(t *testing.T) {
	a := testApp(t)
	installMockDaemon(t, a, "mock-daemon", false)

	w := request(a, "GET", "/api/plugins/daemons", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"status":"stopped"`) {
		t.Fatalf("初始应为 stopped: %s", w.Body.String())
	}

	requireStatus(t, request(a, "POST", "/api/plugins/daemons/mock-daemon/start", nil), 200)
	w = request(a, "GET", "/api/plugins/daemons", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"status":"running"`) {
		t.Fatalf("start 后应为 running: %s", w.Body.String())
	}

	requireStatus(t, request(a, "POST", "/api/plugins/daemons/mock-daemon/stop", nil), 200)
	w = request(a, "GET", "/api/plugins/daemons", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"status":"stopped"`) {
		t.Fatalf("stop 后应为 stopped: %s", w.Body.String())
	}
}

func TestDaemonCallViaIPC(t *testing.T) {
	a := testApp(t)
	installMockDaemon(t, a, "mock-daemon", false)
	requireStatus(t, request(a, "POST", "/api/plugins/daemons/mock-daemon/start", nil), 200)

	raw, err := a.callPluginTool("mock-daemon", "echo", map[string]any{"msg": "hello-daemon"})
	if err != nil {
		t.Fatalf("IPC 调用失败: %v", err)
	}
	m, _ := raw.(map[string]any)
	if m["text"] != "hello-daemon" {
		t.Fatalf("返回值不符: %v", raw)
	}
}

func TestDaemonCrashRestart(t *testing.T) {
	a := testApp(t)
	installMockDaemon(t, a, "mock-daemon", false)
	requireStatus(t, request(a, "POST", "/api/plugins/daemons/mock-daemon/start", nil), 200)

	a.daemons.mu.Lock()
	p := a.daemons.procs["mock-daemon"]
	proc := p.cmd.Process
	a.daemons.mu.Unlock()
	if proc == nil {
		t.Fatal("进程尚未启动")
	}
	_ = proc.Kill() // 模拟崩溃

	deadline := time.Now().Add(25 * time.Second)
	ok := false
	for time.Now().Before(deadline) {
		if p.statusSnapshot() == dsRunning {
			if raw, err := a.callPluginTool("mock-daemon", "echo", map[string]any{"msg": "after-crash"}); err == nil {
				if m, _ := raw.(map[string]any); m["text"] == "after-crash" {
					ok = true
					break
				}
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !ok {
		t.Fatal("崩溃后未在退避窗口内恢复并可服务")
	}
	if cc := p.crashCountSnapshot(); cc < 1 {
		t.Fatalf("crashCount 应 >=1, got %d", cc)
	}
}

func TestDaemonEventReporting(t *testing.T) {
	a := testApp(t)
	installMockDaemon(t, a, "mock-daemon", false)
	requireStatus(t, request(a, "POST", "/api/plugins/daemons/mock-daemon/start", nil), 200)

	if _, err := a.callPluginTool("mock-daemon", "emit-event", map[string]any{"note": "hi-from-mock"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)

	w := request(a, "GET", "/api/plugins/daemons/mock-daemon/events", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), "hi-from-mock") {
		t.Fatalf("事件未进入缓存: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"type":"event"`) {
		t.Fatalf("事件类型缺失: %s", w.Body.String())
	}
}

func TestNonDaemonUnchanged(t *testing.T) {
	a := testApp(t)
	code := `'use strict';
module.exports = { name: 'nd', apply(ctx) {
  ctx.tool({ name: 'hi', handler: (args) => ({ text: 'hello ' + (args.name || '') }) });
}};`
	requireStatus(t, request(a, "POST", "/api/plugins", map[string]any{"id": "nd", "name": "nd", "code": code}), 201)

	// 非 daemon 插件仍走 v1.1 短命进程
	raw, err := a.callPluginTool("nd", "hi", map[string]any{"name": "world"})
	if err != nil {
		t.Fatalf("非 daemon 路径回归: %v", err)
	}
	m, _ := raw.(map[string]any)
	if m["text"] != "hello world" {
		t.Fatalf("返回值不符: %v", raw)
	}
	// 非 daemon 插件不出现在 daemons 列表
	w := request(a, "GET", "/api/plugins/daemons", nil)
	requireStatus(t, w, 200)
	if strings.Contains(w.Body.String(), `"id":"nd"`) {
		t.Fatalf("非 daemon 不应出现在 daemons 列表: %s", w.Body.String())
	}
}

func TestDaemonDisabledByDefault(t *testing.T) {
	src := findMockDaemon(t)
	root := t.TempDir()
	work := filepath.Join(root, "work")
	pluginsDir := filepath.Join(work, "plugins")
	if err := os.MkdirAll(filepath.Join(pluginsDir, "mock-daemon"), 0755); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(src, "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginsDir, "mock-daemon", "index.js"), b, 0644); err != nil {
		t.Fatal(err)
	}
	reg := pluginRegistry{Version: 1, Plugins: []PluginManifest{
		{ID: "mock-daemon", Name: "Mock Daemon", Main: "index.js", Daemon: true, Enabled: false},
	}}
	rb, _ := json.Marshal(reg)
	if err := os.WriteFile(filepath.Join(pluginsDir, "registry.json"), rb, 0644); err != nil {
		t.Fatal(err)
	}

	a, err := New(work, filepath.Join(root, "ref"), filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)

	// 默认不自动启动
	w := request(a, "GET", "/api/plugins/daemons", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"status":"stopped"`) {
		t.Fatalf("默认应 stopped: %s", w.Body.String())
	}
	a.daemons.mu.Lock()
	n := len(a.daemons.procs)
	a.daemons.mu.Unlock()
	if n != 0 {
		t.Fatalf("未启用的 daemon 不应被自动拉起, procs=%d", n)
	}
}
