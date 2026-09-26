package server

import (
	"strings"
	"sync"
)

// ── 流式输出桥接（#35）────────────────────────────────────────────────────────
//
// aide 主会话 SSE 产生的增量，按 sessionID 发布到一个轻量内存 broker；
// 小秘据此拉取 aide【正在进行中 / 已被打断】的实时输出。
//
// 与 #30 get_session 的关系（互补，不是替代）：
//   - get_session 读的是【已落库】的会话消息（完成后持久化）；
//   - 本 broker 覆盖【进行中 / 未落库】的内容，以及 run 被用户打断时
//     已经流式产出的那部分（interrupted 状态下仍保留）。
//
// 生命周期纯内存：进程重启后 broker 清空是预期——进行中的输出本就不该跨重启；
// 已完成的输出早已落库，小秘用 get_session 读取。

// run 状态。
const (
	streamRunning     = "running"     // 运行中，缓冲持续累积
	streamDone        = "done"        // 正常完成，缓冲=本次完整输出
	streamInterrupted = "interrupted" // 被用户打断/取消，缓冲保留已产出部分
	streamFailed      = "failed"      // 出错终止，缓冲保留已产出部分
)

// liveOutputMaxBytes 每个会话保留的最近输出上限（约 100KB），超出后截断不再追加。
const liveOutputMaxBytes = 100 << 10

type liveRun struct {
	buf    strings.Builder
	status string
	bytes  int
}

// StreamBroker 按 sessionID 隔离的实时输出缓冲。零值不可用，用 NewStreamBroker 构造。
type StreamBroker struct {
	mu   sync.Mutex
	runs map[string]*liveRun
}

// NewStreamBroker 构造一个空 broker。
func NewStreamBroker() *StreamBroker {
	return &StreamBroker{runs: map[string]*liveRun{}}
}

// Publish 发布一次流式事件。
//
//	status=="running"（chunk 可为空）：开始一轮新运行，重置该会话的缓冲与状态。
//	status==""                  ：把 chunk 追加进进行中的输出缓冲（超上限截断）。
//	status 为 done/interrupted/failed：标记终态；缓冲【保留】（被打断不丢已产出）。
//
// 空 receiver / 空 sessionID 安全 no-op。
func (b *StreamBroker) Publish(sessionID, chunk, status string) {
	if b == nil || sessionID == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	switch status {
	case streamRunning:
		// 新一轮开始：重置缓冲（最近一次运行的输出）。
		b.runs[sessionID] = &liveRun{status: streamRunning}
		return
	case streamDone, streamInterrupted, streamFailed:
		run := b.runs[sessionID]
		if run == nil {
			run = &liveRun{}
			b.runs[sessionID] = run
		}
		run.status = status
		return
	default: // 增量（status==""）
		run := b.runs[sessionID]
		if run == nil || run.status != streamRunning {
			run = &liveRun{status: streamRunning}
			b.runs[sessionID] = run
		}
		if chunk == "" || run.bytes >= liveOutputMaxBytes {
			return
		}
		remaining := liveOutputMaxBytes - run.bytes
		if len(chunk) > remaining {
			chunk = chunk[:remaining]
		}
		run.buf.WriteString(chunk)
		run.bytes += len(chunk)
	}
}

// GetSessionLiveOutput 拉取某会话最近一次运行的实时输出与状态。
// 返回 (text, status, found)：
//   - found=false 表示该会话暂无任何记录；
//   - interrupted/failed 状态下仍返回已产出的部分文本（不丢弃）。
func (b *StreamBroker) GetSessionLiveOutput(sessionID string) (text, status string, found bool) {
	if b == nil {
		return "", "", false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	run, ok := b.runs[sessionID]
	if !ok {
		return "", "", false
	}
	return run.buf.String(), run.status, true
}
