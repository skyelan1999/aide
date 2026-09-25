package server

/* 协议 v1.2：daemon（常驻守护）宿主管理器。
 *
 * 每个 daemon 插件对应一个长驻 `node -e <pluginHostJS> daemon <pluginPath>` 子进程，
 * Go 与子进程之间用 stdio 行分隔 JSON-RPC 通信（见 docs/plugins/daemon-protocol.md）。
 *
 * 安全模型：
 *   - daemon 插件默认全部禁用，仅当 manifest 声明 `"daemon": true` 且注册表显式 enabled 才会被自动拉起；
 *   - 管理器自身不监听网络端口；协议插件如需绑定套接字，必须仅绑 127.0.0.1（通过 AIDE_DAEMON_BIND 传递）；
 *   - 崩溃后指数退避重启（1s→2s→4s→8s，封顶 30s）；连续健康运行 60s 后重置退避基数。
 *
 * 本文件只新增，不改 #30（会话）/#31（数据目录）/#38（密钥库）区域。
 */

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

const (
	daemonEventCap     = 1000             // 每个 daemon 进程事件环形缓存容量
	daemonPingInterval = 30 * time.Second // 心跳周期
	daemonPingTimeout  = 5 * time.Second  // 心跳等待 pong 超时，超时即判崩溃
	daemonCallTimeout  = 70 * time.Second // 单次 tool.call 等待结果超时（与 v1.1 对齐）
	daemonStartTimeout = 15 * time.Second // 启动等待 ready 事件超时
	daemonMaxBackoff   = 30 * time.Second // 崩溃退避上限
	daemonBaseBackoff  = time.Second      // 首次崩溃退避
	daemonResetUptime  = 60 * time.Second // 连续健康运行超过此时长则重置退避基数
	daemonGraceStop    = 3 * time.Second  // 优雅停止宽限，超时后 SIGKILL
)

type daemonStatus string

const (
	dsRunning daemonStatus = "running"
	dsStopped daemonStatus = "stopped"
	dsBackoff daemonStatus = "backoff"
	dsCrashed daemonStatus = "crashed"
)

// daemonEvent 是守护进程上报的一条事件（traffic/event/log），Go 侧环形缓存最近 N 条。
type daemonEvent struct {
	Type             string `json:"type"`
	Time             string `json:"time,omitempty"`
	Direction        string `json:"direction,omitempty"`
	Length           int    `json:"length,omitempty"`
	PayloadTruncated bool   `json:"payloadTruncated,omitempty"`
	Subtype          string `json:"subtype,omitempty"`
	Message          string `json:"message,omitempty"`
	Level            string `json:"level,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResp struct {
	result json.RawMessage
	err    *rpcError
}

// daemonProc 单个 daemon 插件的运行态。
type daemonProc struct {
	id         string
	pluginPath string
	cmd        *exec.Cmd
	stdin      io.WriteCloser

	mu      sync.Mutex // 保护 cmd/stdin/pending/nextID
	nextID  int64
	pending map[int64]chan rpcResp
	readyCh chan struct{}
	doneCh  chan struct{}
	stopCh  chan struct{}

	statusMu   sync.RWMutex
	status     daemonStatus
	since      time.Time
	crashCount int

	eventsMu sync.Mutex
	events   []daemonEvent

	startedAt time.Time
	backoff   time.Duration
}

// DaemonManager 管理所有 daemon 插件子进程的生命周期。
type DaemonManager struct {
	app         *App
	pluginsPath string
	mu          sync.Mutex
	procs       map[string]*daemonProc
}

func newDaemonManager(a *App) *DaemonManager {
	return &DaemonManager{app: a, pluginsPath: a.pluginsPath, procs: map[string]*daemonProc{}}
}

func (p *daemonProc) setStatus(s daemonStatus) {
	p.statusMu.Lock()
	p.status = s
	p.since = time.Now()
	p.statusMu.Unlock()
}

func (p *daemonProc) statusSnapshot() daemonStatus {
	p.statusMu.RLock()
	defer p.statusMu.RUnlock()
	return p.status
}

func (p *daemonProc) crashCountSnapshot() int {
	p.statusMu.RLock()
	defer p.statusMu.RUnlock()
	return p.crashCount
}

// isDaemonPlugin 报告某 id 是否在注册表中声明为 daemon 插件。
func (a *App) isDaemonPlugin(id string) bool {
	for _, p := range a.pluginRegistry.Plugins {
		if p.ID == id {
			return p.Daemon
		}
	}
	return false
}

func (a *App) daemonPluginMain(id string) string {
	for _, p := range a.pluginRegistry.Plugins {
		if p.ID == id {
			main := p.Main
			if main == "" {
				main = "index.js"
			}
			return filepath.Join(a.pluginsPath, id, main)
		}
	}
	return filepath.Join(a.pluginsPath, id, "index.js")
}

// restore 容器/进程重启后按注册表期望状态自动恢复：声明 daemon 且 enabled 的插件自动拉起。
func (m *DaemonManager) restore() {
	for _, p := range m.app.pluginRegistry.Plugins {
		if p.Daemon && p.Enabled {
			if err := m.Start(p.ID); err != nil {
				log.Printf("[daemon] 自动恢复 %s 失败: %v", p.ID, err)
			}
		}
	}
}

// Start 启动（或复用）一个 daemon 插件；幂等。
func (m *DaemonManager) Start(id string) error {
	m.mu.Lock()
	p, exists := m.procs[id]
	if !exists {
		p = &daemonProc{
			id:         id,
			pluginPath: m.app.daemonPluginMain(id),
			pending:    map[int64]chan rpcResp{},
			events:     []daemonEvent{},
			stopCh:     make(chan struct{}),
			backoff:    daemonBaseBackoff,
		}
		m.procs[id] = p
	}
	st := p.statusSnapshot()
	already := st == dsRunning || st == dsBackoff
	if already {
		m.mu.Unlock()
		return nil
	}
	// 重新进入运行：重置 stopCh 与退避
	p.mu.Lock()
	p.stopCh = make(chan struct{})
	readyCh := make(chan struct{})
	p.readyCh = readyCh
	p.mu.Unlock()
	m.mu.Unlock()

	go p.supervise()

	// 等待 ready（supervise 内首次 spawn 成功后会关闭 readyCh）
	deadline := time.Now().Add(daemonStartTimeout)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		rc := p.readyCh
		p.mu.Unlock()
		if rc != nil {
			select {
			case <-rc:
				return nil
			case <-time.After(100 * time.Millisecond):
			}
		} else {
			time.Sleep(30 * time.Millisecond)
		}
		if st2 := p.statusSnapshot(); st2 == dsCrashed {
			return fmt.Errorf("daemon %s 启动失败", id)
		}
	}
	return fmt.Errorf("daemon %s 启动超时（%v 内未就绪）", id, daemonStartTimeout)
}

// supervise 是单个 daemon 插件的重启循环：spawn → 等待退出 → 退避 → 再 spawn。
func (p *daemonProc) supervise() {
	for {
		p.spawn()
		p.mu.Lock()
		done := p.doneCh
		stop := p.stopCh
		p.mu.Unlock()
		<-done // 等待本次进程退出

		select {
		case <-stop:
			p.setStatus(dsStopped)
			return
		default:
		}

		// 异常退出：退避重启
		p.statusMu.Lock()
		p.crashCount++
		uptime := time.Since(p.startedAt)
		if uptime > daemonResetUptime {
			p.backoff = daemonBaseBackoff // 长时间健康后崩溃，重置退避基数
		}
		delay := p.backoff
		p.statusMu.Unlock()
		p.setStatus(dsBackoff)
		log.Printf("[daemon] %s 异常退出（已运行 %v），%v 后第 %d 次重启", p.id, uptime.Truncate(time.Second), delay, p.crashCount)

		select {
		case <-time.After(delay):
		case <-stop:
			p.setStatus(dsStopped)
			return
		}
		p.statusMu.Lock()
		p.backoff *= 2
		if p.backoff > daemonMaxBackoff {
			p.backoff = daemonMaxBackoff
		}
		p.statusMu.Unlock()
	}
}

// spawn 拉起一次子进程，建立 stdio JSON-RPC。
func (p *daemonProc) spawn() {
	p.mu.Lock()
	cmd := exec.Command("node", "-e", pluginHostJS, "daemon", p.pluginPath)
	// 与 v1.1 短命进程同一受限环境；AIDE_DAEMON_BIND 告知协议插件仅绑回环。
	cmd.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=/home/aide", "AIDE_DAEMON=1", "AIDE_DAEMON_BIND=127.0.0.1"}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		p.mu.Unlock()
		log.Printf("[daemon] %s stdin 管道失败: %v", p.id, err)
		p.setStatus(dsCrashed)
		p.mu.Lock()
		p.doneCh = make(chan struct{})
		close(p.doneCh)
		p.mu.Unlock()
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		p.mu.Unlock()
		p.setStatus(dsCrashed)
		p.mu.Lock()
		p.doneCh = make(chan struct{})
		close(p.doneCh)
		p.mu.Unlock()
		return
	}
	doneCh := make(chan struct{})
	p.stdin = stdin
	p.pending = map[int64]chan rpcResp{}
	p.doneCh = doneCh
	p.mu.Unlock()

	if err := cmd.Start(); err != nil {
		log.Printf("[daemon] %s 启动失败: %v", p.id, err)
		p.setStatus(dsCrashed)
		close(doneCh)
		return
	}
	p.cmd = cmd
	p.startedAt = time.Now()
	p.setStatus(dsRunning)
	go p.readLoop(stdout, doneCh)
	go p.heartbeat(doneCh)
	go func() { _ = cmd.Wait() }() // 回收进程；stdout EOF 由 readLoop 处理
}

// readLoop 逐行解析子进程 stdout：响应路由到 pending，事件入缓存，pong 更新心跳。
func (p *daemonProc) readLoop(stdout io.ReadCloser, doneCh chan struct{}) {
	defer close(doneCh)
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 1<<20), 4<<20)
	for sc.Scan() {
		line := sc.Bytes()
		var msg struct {
			ID     *int64          `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
			Error  *rpcError       `json:"error"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}
		if msg.Method == "event" {
			var ev daemonEvent
			_ = json.Unmarshal(msg.Params, &ev)
			p.appendEvent(ev)
			if ev.Subtype == "ready" {
				p.mu.Lock()
				select {
				case <-p.readyCh:
				default:
					close(p.readyCh)
				}
				p.mu.Unlock()
			}
			continue
		}
		if msg.Method == "pong" {
			continue
		}
		if msg.ID != nil {
			p.mu.Lock()
			ch, ok := p.pending[*msg.ID]
			if ok {
				delete(p.pending, *msg.ID)
			}
			p.mu.Unlock()
			if ok {
				ch <- rpcResp{result: msg.Result, err: msg.Error}
			}
		}
	}
}

// heartbeat 周期 ping；超时未 pong 视为崩溃，杀掉进程触发重启。
func (p *daemonProc) heartbeat(doneCh chan struct{}) {
	p.mu.Lock()
	stopCh := p.stopCh
	p.mu.Unlock()
	t := time.NewTicker(daemonPingInterval)
	defer t.Stop()
	for {
		select {
		case <-doneCh:
			return
		case <-stopCh:
			return
		case <-t.C:
		}
		id := atomic.AddInt64(&p.nextID, 1)
		ch := make(chan rpcResp, 1)
		p.mu.Lock()
		p.pending[id] = ch
		p.mu.Unlock()
		p.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": "ping"})
		select {
		case <-ch:
		case <-time.After(daemonPingTimeout):
			log.Printf("[daemon] %s 心跳超时，判定崩溃", p.id)
			p.kill()
			return
		case <-doneCh:
			return
		}
	}
}

func (p *daemonProc) kill() {
	p.mu.Lock()
	cmd := p.cmd
	p.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

func (p *daemonProc) send(obj any) {
	b, err := json.Marshal(obj)
	if err != nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stdin == nil {
		return
	}
	_, _ = p.stdin.Write(b)
	_, _ = p.stdin.Write([]byte("\n"))
}

func (p *daemonProc) appendEvent(ev daemonEvent) {
	p.eventsMu.Lock()
	defer p.eventsMu.Unlock()
	p.events = append(p.events, ev)
	if len(p.events) > daemonEventCap {
		p.events = p.events[len(p.events)-daemonEventCap:]
	}
}

// Stop 终止 daemon 插件并释放其占用的端口（协议插件 stop() 钩子负责关套接字）。
func (m *DaemonManager) Stop(id string) error {
	m.mu.Lock()
	p := m.procs[id]
	m.mu.Unlock()
	if p == nil {
		return nil
	}
	p.stop()
	return nil
}

func (p *daemonProc) stop() {
	p.mu.Lock()
	select {
	case <-p.stopCh: // 已关闭
	default:
		close(p.stopCh)
	}
	stdin := p.stdin
	cmd := p.cmd
	p.mu.Unlock()

	if stdin != nil {
		_ = stdin.Close() // stdin EOF → 宿主进入 stop() → 退出
	}
	p.mu.Lock()
	done := p.doneCh
	p.mu.Unlock()
	if done != nil {
		select {
		case <-done:
		case <-time.After(daemonGraceStop):
			if cmd != nil && cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			<-done
		}
	}
	p.setStatus(dsStopped)
}

// Restart 先 Stop 再 Start。
func (m *DaemonManager) Restart(id string) error {
	_ = m.Stop(id)
	return m.Start(id)
}

// StopAll 应用关闭时调用。
func (m *DaemonManager) StopAll() {
	m.mu.Lock()
	procs := make([]*daemonProc, 0, len(m.procs))
	for _, p := range m.procs {
		procs = append(procs, p)
	}
	m.mu.Unlock()
	for _, p := range procs {
		p.stop()
	}
}

// Call 经 IPC 转发一次工具调用，等待结果。
func (m *DaemonManager) Call(id, tool string, args map[string]any) (any, error) {
	m.mu.Lock()
	p := m.procs[id]
	m.mu.Unlock()
	if p == nil {
		return nil, fmt.Errorf("daemon %s 不存在", id)
	}
	if st := p.statusSnapshot(); st != dsRunning {
		return nil, fmt.Errorf("daemon %s 未运行（状态 %s）", id, st)
	}
	return p.call(tool, args)
}

func (p *daemonProc) call(tool string, args map[string]any) (any, error) {
	id := atomic.AddInt64(&p.nextID, 1)
	ch := make(chan rpcResp, 1)
	p.mu.Lock()
	p.pending[id] = ch
	doneCh := p.doneCh
	p.mu.Unlock()
	p.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": "tool.call", "params": map[string]any{"tool": tool, "args": args}})
	select {
	case resp := <-ch:
		if resp.err != nil {
			return nil, errors.New(resp.err.Message)
		}
		var v any
		if len(resp.result) > 0 {
			if err := json.Unmarshal(resp.result, &v); err != nil {
				return nil, fmt.Errorf("daemon 结果解析失败: %w", err)
			}
		}
		return v, nil
	case <-time.After(daemonCallTimeout):
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
		return nil, errors.New("daemon 工具调用超时")
	case <-doneCh:
		return nil, errors.New("daemon 进程已退出")
	}
}

// daemonStatusInfo 对外暴露的 daemon 状态行。
type daemonStatusInfo struct {
	ID         string `json:"id"`
	Daemon     bool   `json:"daemon"`
	Enabled    bool   `json:"enabled"`
	Status     string `json:"status"`
	Since      string `json:"since,omitempty"`
	CrashCount int    `json:"crashCount,omitempty"`
}

// List 返回所有 daemon 插件的状态（含从未启动者，默认 stopped）。
func (m *DaemonManager) List() []daemonStatusInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []daemonStatusInfo{}
	for _, reg := range m.app.pluginRegistry.Plugins {
		if !reg.Daemon {
			continue
		}
		info := daemonStatusInfo{ID: reg.ID, Daemon: true, Enabled: reg.Enabled, Status: string(dsStopped), CrashCount: 0}
		if p, ok := m.procs[reg.ID]; ok {
			info.Status = string(p.statusSnapshot())
			info.CrashCount = p.crashCountSnapshot()
			p.statusMu.RLock()
			if !p.since.IsZero() {
				info.Since = p.since.UTC().Format(time.RFC3339)
			}
			p.statusMu.RUnlock()
		}
		out = append(out, info)
	}
	return out
}

// Events 返回某 daemon 进程缓存的最近事件。
func (m *DaemonManager) Events(id string) []daemonEvent {
	m.mu.Lock()
	p := m.procs[id]
	m.mu.Unlock()
	if p == nil {
		return []daemonEvent{}
	}
	p.eventsMu.Lock()
	defer p.eventsMu.Unlock()
	out := make([]daemonEvent, len(p.events))
	copy(out, p.events)
	return out
}

// ── HTTP 路由处理器 ──

func (a *App) listDaemons(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	jsonOut(w, 200, map[string]any{"daemons": a.daemons.List()})
}

func (a *App) daemonStartHandler(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	id := r.PathValue("id")
	if !a.isDaemonPlugin(id) {
		fail(w, 404, errors.New("非 daemon 插件"))
		return
	}
	if err := a.daemons.Start(id); err != nil {
		fail(w, 500, err)
		return
	}
	jsonOut(w, 200, map[string]any{"id": id, "status": string(dsRunning)})
}

func (a *App) daemonStopHandler(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	id := r.PathValue("id")
	if !a.isDaemonPlugin(id) {
		fail(w, 404, errors.New("非 daemon 插件"))
		return
	}
	_ = a.daemons.Stop(id)
	jsonOut(w, 200, map[string]any{"id": id, "status": string(dsStopped)})
}

func (a *App) daemonRestartHandler(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	id := r.PathValue("id")
	if !a.isDaemonPlugin(id) {
		fail(w, 404, errors.New("非 daemon 插件"))
		return
	}
	if err := a.daemons.Restart(id); err != nil {
		fail(w, 500, err)
		return
	}
	jsonOut(w, 200, map[string]any{"id": id, "status": string(dsRunning)})
}

func (a *App) daemonEventsHandler(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	id := r.PathValue("id")
	events := a.daemons.Events(id)
	jsonOut(w, 200, map[string]any{"id": id, "events": events})
}
