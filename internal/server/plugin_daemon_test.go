package server

/* 协议 v1.2 daemon 宿主测试。依赖 plugins/testdata/mock-daemon/ 与系统 node。
 * 若在无 node 的环境（如裸 golang 镜像）运行，相关用例 t.Skip 记 NOT_RUN。
 */

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// installHookedDaemon 在注册表登记一个 daemon 插件，并在管理器里预置一个带 launchHook 的 proc。
// 钩子由调用方控制：进入慢启动时发 enteredReady 前先 close(enteredSlow)，等待 releaseStart 后才算 ready，
// 之后等 p.stopCh 关闭再 close(doneCh)（模拟优雅退出）。
func installHookedDaemon(t *testing.T, a *App, id string, hook func(readyCh, doneCh chan struct{})) *daemonProc {
	t.Helper()
	a.mu.Lock()
	a.pluginRegistry.Plugins = append(a.pluginRegistry.Plugins, PluginManifest{
		ID: id, Name: "Hooked Daemon", Main: "index.js", Daemon: true, Enabled: false,
	})
	a.mu.Unlock()

	a.daemons.mu.Lock()
	p := &daemonProc{
		id:         id,
		pluginPath: "/nonexistent/" + id,
		pending:    map[int64]chan rpcResp{},
		events:     []daemonEvent{},
		stopCh:     make(chan struct{}),
		backoff:    daemonBaseBackoff,
		launchHook: hook,
	}
	a.daemons.procs[id] = p
	a.daemons.mu.Unlock()
	return p
}

// lockFreeWithin 报告在 wait 时长内能否拿到 a.mu（不持有，拿到即释放）。
func lockFreeWithin(a *App, wait time.Duration) bool {
	ch := make(chan struct{})
	go func() { a.mu.Lock(); close(ch); a.mu.Unlock() }()
	select {
	case <-ch:
		return true
	case <-time.After(wait):
		return false
	}
}

// TestDaemonStartOutsideGlobalLock：慢启动（等待 node 宿主 ready）期间，全局 a.mu 必须仍可被其它请求获取，
// 即 Start 不得在持有 a.mu 时同步等待子进程。
func TestDaemonStartOutsideGlobalLock(t *testing.T) {
	a := testApp(t)
	enteredSlow := make(chan struct{})
	releaseStart := make(chan struct{})
	var p *daemonProc
	p = installHookedDaemon(t, a, "slow-start", func(readyCh, doneCh chan struct{}) {
		close(enteredSlow) // 已进入慢进程阶段
		<-releaseStart     // 模拟 node 宿主 10-15s 才 ready
		p.closeReadyOnce()
		p.mu.Lock()
		stopCh := p.stopCh
		p.mu.Unlock()
		<-stopCh
		close(doneCh)
	})

	startDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		startDone <- request(a, "POST", "/api/plugins/daemons/slow-start/start", nil)
	}()

	<-enteredSlow
	// 慢启动正在进行时，a.mu 必须仍可获取；否则说明 handler 仍在锁内等待子进程。
	if !lockFreeWithin(a, 2*time.Second) {
		close(releaseStart)
		t.Fatal("慢启动等待 ready 期间 a.mu 仍被占用：启停未移出全局锁")
	}

	close(releaseStart)
	w := <-startDone
	requireStatus(t, w, 200)
	if got := p.statusSnapshot(); got != dsRunning {
		t.Fatalf("start 后应为 running, got %s", got)
	}
	// 干净停止
	requireStatus(t, request(a, "POST", "/api/plugins/daemons/slow-start/stop", nil), 200)
	if got := p.statusSnapshot(); got != dsStopped {
		t.Fatalf("stop 后应为 stopped, got %s", got)
	}
}

// TestDaemonStopOutsideGlobalLock：慢停止（等待进程退出）期间 a.mu 也必须可获取。
func TestDaemonStopOutsideGlobalLock(t *testing.T) {
	a := testApp(t)
	releaseStart := make(chan struct{})
	enteredSlow := make(chan struct{})
	stopReturned := make(chan struct{})
	var p *daemonProc
	p = installHookedDaemon(t, a, "slow-stop", func(readyCh, doneCh chan struct{}) {
		close(enteredSlow)
		<-releaseStart
		p.closeReadyOnce()
		// Concurrent starts may replace stopCh; capture it under the mutex used
		// by the production supervisor to avoid racing the test hook itself.
		p.mu.Lock()
		stopCh := p.stopCh
		p.mu.Unlock()
		<-stopCh
		// 模拟退出延迟：宽限期内不立刻 close doneCh
		time.Sleep(200 * time.Millisecond)
		close(doneCh)
	})

	// 先触发一次 Start，进入慢启动
	startDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		startDone <- request(a, "POST", "/api/plugins/daemons/slow-stop/start", nil)
	}()
	<-enteredSlow
	close(releaseStart)
	<-startDone // 等其 ready
	if p.statusSnapshot() != dsRunning {
		t.Fatal("前置：daemon 未进入 running")
	}

	go func() {
		requireStatus(t, request(a, "POST", "/api/plugins/daemons/slow-stop/stop", nil), 200)
		close(stopReturned)
	}()

	// 慢停止期间 a.mu 必须仍可获取
	if !lockFreeWithin(a, 2*time.Second) {
		t.Fatal("慢停止期间 a.mu 仍被占用：Stop 未移出全局锁")
	}
	<-stopReturned
}

// TestDaemonConcurrentStartStopConsistency：并发重复 Start/Stop，注册表与进程状态最终一致，无 panic/死锁。
func TestDaemonConcurrentStartStopConsistency(t *testing.T) {
	a := testApp(t)
	enteredSlow := make(chan struct{}, 100)
	releaseStart := make(chan struct{})
	closeOnce := sync.Once{}
	var p *daemonProc
	p = installHookedDaemon(t, a, "race-daemon", func(readyCh, doneCh chan struct{}) {
		// 每次 spawn：短暂信号后立即 ready（不阻塞），模拟快启动；退出等 stopCh。
		select {
		case enteredSlow <- struct{}{}:
		default:
		}
		closeOnce.Do(func() { close(releaseStart) })
		p.closeReadyOnce()
		<-p.stopCh
		close(doneCh)
	})

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				_ = a.daemons.StartWithPath("race-daemon", "/nonexistent/race-daemon")
			} else {
				_ = a.daemons.Stop("race-daemon")
			}
		}(i)
	}
	wg.Wait()

	// 最终：显式停止，状态收敛为 stopped，且 procs 表条目不重复。
	_ = a.daemons.Stop("race-daemon")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if p.statusSnapshot() == dsStopped {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := p.statusSnapshot(); got != dsStopped {
		t.Fatalf("并发启停后最终应收敛 stopped, got %s", got)
	}
	a.daemons.mu.Lock()
	n := len(a.daemons.procs["race-daemon"].events) // 仅断言条目存在、不 panic
	a.daemons.mu.Unlock()
	_ = n
}
