package server

/* P2 #3（backend-core-001）：ask_user 陈旧应答竞态回归测试。
 *
 * 旧实现把 task.AnswerCh（buffered(1)）在多轮澄清间复用，上一轮残留/迟到的应答会被
 * 下一轮的 `<-AnswerCh` 误取，导致错轮应答被应用。修复后每轮 ask_user 新建独立 channel，
 * 应答入口在 a.mu 内投递到「当前仍有效那一轮」的通道。
 */

import (
	"testing"
	"time"
)

// seedClarifySession 直接在内存里塞一个会话+run，用于单测应答投递路径（不走完整 run loop）。
func seedClarifySession(t *testing.T, status string) (*App, *Session, *Task) {
	t.Helper()
	a := testApp(t)
	task := &Task{ID: "run1", Mode: "chat", Status: status}
	s := &Session{ID: "sess1", Runs: []*Task{task}, Messages: []Message{}}
	a.mu.Lock()
	a.sessions["sess1"] = s
	a.mu.Unlock()
	return a, s, task
}

// TestClarifyRoundIsolation：上一轮残留应答 + 紧接着新澄清，断言新澄清只收到本轮应答、旧应答被丢弃。
func TestClarifyRoundIsolation(t *testing.T) {
	a, _, task := seedClarifySession(t, "awaiting_clarification")

	// 轮次 1 的通道 ch1，并模拟上一轮残留一个迟到应答（buffered(1) 里剩的值——旧 bug 的根源）。
	ch1 := make(chan string, 1)
	task.answerRound = 1
	task.AnswerCh = ch1
	ch1 <- "STALE-from-round1"

	// 紧接着提出新澄清（轮次 2）：ask_user 新建 ch2 并替换 task.AnswerCh，等待方只等 ch2。
	ch2 := make(chan string, 1)
	task.answerRound = 2
	task.AnswerCh = ch2
	task.Status = "awaiting_clarification"

	// 用户对当前（第 2 轮）问题作答。
	w := request(a, "POST", "/api/sessions/sess1/runs/run1/answer", map[string]any{"answer": "FRESH-round2"})
	requireStatus(t, w, 200)

	// 本轮应答必须落到 ch2。
	select {
	case got := <-ch2:
		if got != "FRESH-round2" {
			t.Fatalf("ch2 收到 %q，期望本轮应答", got)
		}
	case <-time.After(time.Second):
		t.Fatal("本轮应答未投递到新通道")
	}
	// ch2 不得有多余数据。
	select {
	case v := <-ch2:
		t.Fatalf("ch2 多读了: %q", v)
	default:
	}
	// 旧通道 ch1 的残留原封不动（从未被本轮读取）。
	if len(ch1) != 1 {
		t.Fatalf("旧通道缓冲长度 %d，期望 1（残留未被消费）", len(ch1))
	}
	if v := <-ch1; v != "STALE-from-round1" {
		t.Fatalf("旧通道内容 %q，应为上一轮残留", v)
	}
}

// TestClarifyAnswerToFinishedTask：对已结束/不再 awaiting 的任务应答，必须 409 丢弃、不发送、不 panic、不阻塞。
func TestClarifyAnswerToFinishedTask(t *testing.T) {
	a, _, task := seedClarifySession(t, "done") // 已结束
	ch := make(chan string, 1)
	task.AnswerCh = ch

	w := request(a, "POST", "/api/sessions/sess1/runs/run1/answer", map[string]any{"answer": "late"})
	requireStatus(t, w, 409)
	select {
	case v := <-ch:
		t.Fatalf("已结束任务的应答被投递: %q", v)
	default:
	}
}

// TestClarifyAnswerNonexistentRun：run 不存在时 409，不 panic。
func TestClarifyAnswerNonexistentRun(t *testing.T) {
	a, _, _ := seedClarifySession(t, "awaiting_clarification")
	w := request(a, "POST", "/api/sessions/sess1/runs/NOPE/answer", map[string]any{"answer": "x"})
	requireStatus(t, w, 409)
}

// TestClarifyRoundChannelReplaced：连续两轮 ask_user 后，task.AnswerCh 指向最新一轮，
// 旧通道不再被引用（可被 GC），且对旧通道的直接发送不会串到新等待方。
func TestClarifyRoundChannelReplaced(t *testing.T) {
	a, _, task := seedClarifySession(t, "awaiting_clarification")

	ch1 := make(chan string, 1)
	task.answerRound = 1
	task.AnswerCh = ch1

	// 进入第 2 轮
	ch2 := make(chan string, 1)
	task.answerRound = 2
	task.AnswerCh = ch2
	task.Status = "awaiting_clarification"

	// 模拟「迟到的旧应答」直接落进已被弃用的 ch1（旧实现里它会被下一轮误读）。
	ch1 <- "old-late"

	// 新投递必须只进 ch2。
	w := request(a, "POST", "/api/sessions/sess1/runs/run1/answer", map[string]any{"answer": "new"})
	requireStatus(t, w, 200)
	select {
	case got := <-ch2:
		if got != "new" {
			t.Fatalf("ch2=%q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("ch2 未收到本轮应答")
	}
}
