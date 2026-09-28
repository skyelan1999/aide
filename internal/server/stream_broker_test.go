package server

import (
	"strings"
	"testing"
)

// 流式输出桥接（#35）测试：小秘可按会话 ID 拉取 aide 主会话的实时输出，
// 含进行中、以及被打断后已产出的部分。

// TestStreamBrokerPublish：发布增量 → 缓冲累积。
func TestStreamBrokerPublish(t *testing.T) {
	b := NewStreamBroker()
	b.Publish("s1", "", streamRunning)
	b.Publish("s1", "你好", "")
	b.Publish("s1", "，世界", "")
	text, status, found := b.GetSessionLiveOutput("s1")
	if !found {
		t.Fatal("应找到会话 s1")
	}
	if status != streamRunning {
		t.Fatalf("运行中状态应为 running，got=%q", status)
	}
	if text != "你好，世界" {
		t.Fatalf("缓冲应累积为「你好，世界」，got=%q", text)
	}
}

// TestStreamBrokerGetLiveOutput：拉取最新输出正确；无记录会话返回 found=false。
func TestStreamBrokerGetLiveOutput(t *testing.T) {
	b := NewStreamBroker()
	if _, _, found := b.GetSessionLiveOutput("nope"); found {
		t.Fatal("不存在的会话应返回 found=false")
	}
	b.Publish("s9", "", streamRunning)
	b.Publish("s9", "partial answer", "")
	text, _, found := b.GetSessionLiveOutput("s9")
	if !found || text != "partial answer" {
		t.Fatalf("拉取最新输出错误: text=%q found=%v", text, found)
	}
}

// TestStreamBrokerInterrupted：被打断 → 保留已产出部分，status=interrupted。
func TestStreamBrokerInterrupted(t *testing.T) {
	b := NewStreamBroker()
	b.Publish("s2", "", streamRunning)
	b.Publish("s2", "正在写代码第一段……", "")
	b.Publish("s2", "写到一半第二段……", "")
	// 用户打断
	b.Publish("s2", "", streamInterrupted)
	text, status, found := b.GetSessionLiveOutput("s2")
	if !found {
		t.Fatal("被打断会话应仍存在")
	}
	if status != streamInterrupted {
		t.Fatalf("状态应为 interrupted，got=%q", status)
	}
	if !strings.Contains(text, "正在写代码第一段") || !strings.Contains(text, "写到一半第二段") {
		t.Fatalf("打断后必须保留已产出部分，got=%q", text)
	}
}

// TestStreamBrokerDone：正常完成 → status=done，缓冲=完整输出。
func TestStreamBrokerDone(t *testing.T) {
	b := NewStreamBroker()
	b.Publish("s3", "", streamRunning)
	b.Publish("s3", "完整的最终回答。", "")
	b.Publish("s3", "", streamDone)
	text, status, _ := b.GetSessionLiveOutput("s3")
	if status != streamDone {
		t.Fatalf("状态应为 done，got=%q", status)
	}
	if text != "完整的最终回答。" {
		t.Fatalf("完成缓冲应为完整输出，got=%q", text)
	}
}

// TestStreamBrokerMultipleSessions：多会话缓冲隔离。
func TestStreamBrokerMultipleSessions(t *testing.T) {
	b := NewStreamBroker()
	b.Publish("a", "", streamRunning)
	b.Publish("a", "会话A的内容", "")
	b.Publish("b", "", streamRunning)
	b.Publish("b", "会话B的内容", "")

	ta, _, _ := b.GetSessionLiveOutput("a")
	tb, _, _ := b.GetSessionLiveOutput("b")
	if ta != "会话A的内容" || tb != "会话B的内容" {
		t.Fatalf("多会话缓冲应隔离，a=%q b=%q", ta, tb)
	}
}

// TestStreamBrokerResetOnNewRun：同会话新一轮开始 → 重置为最近一次运行的输出。
func TestStreamBrokerResetOnNewRun(t *testing.T) {
	b := NewStreamBroker()
	b.Publish("s", "", streamRunning)
	b.Publish("s", "上一轮输出", "")
	b.Publish("s", "", streamDone)
	// 新一轮开始
	b.Publish("s", "", streamRunning)
	if text, _, _ := b.GetSessionLiveOutput("s"); text != "" {
		t.Fatalf("新一轮应清空旧缓冲，got=%q", text)
	}
	b.Publish("s", "这一轮的新输出", "")
	if text, _, _ := b.GetSessionLiveOutput("s"); text != "这一轮的新输出" {
		t.Fatalf("新一轮缓冲错误，got=%q", text)
	}
}

// TestStreamBrokerCap：超 100KB 后截断，不再无限增长。
func TestStreamBrokerCap(t *testing.T) {
	b := NewStreamBroker()
	b.Publish("big", "", streamRunning)
	chunk := strings.Repeat("x", 10000)
	for i := 0; i < 20; i++ { // 200KB 远超上限
		b.Publish("big", chunk, "")
	}
	text, _, _ := b.GetSessionLiveOutput("big")
	if len(text) > liveOutputMaxBytes+10000 {
		t.Fatalf("缓冲应被限制在约 %d，got len=%d", liveOutputMaxBytes, len(text))
	}
}

// TestLiveRunHelpersWired：App 级 begin/finish 辅助（与 execute 接线一致）。
// 模拟一次：开始→流式产出→用户取消，小秘拉到 interrupted 已产出。
func TestLiveRunHelpersWired(t *testing.T) {
	a := &App{liveBroker: NewStreamBroker(), liveTaskSess: map[string]string{}}
	a.beginLiveRun("sessX", "run1")
	if sid := a.liveSessionID("run1"); sid != "sessX" {
		t.Fatalf("taskID→sessionID 映射错误: %q", sid)
	}
	// 模拟 onDelta 发布两段
	if sid := a.liveSessionID("run1"); sid != "" {
		a.liveBroker.Publish(sid, "第一段正文", "")
		a.liveBroker.Publish(sid, "第二段正文", "")
	}
	// 用户取消 → finishLiveRun(cancelled)
	a.finishLiveRun("sessX", "run1", "cancelled")
	text, status, found := a.liveBroker.GetSessionLiveOutput("sessX")
	if !found {
		t.Fatal("应找到 sessX")
	}
	if status != streamInterrupted {
		t.Fatalf("取消应为 interrupted，got=%q", status)
	}
	if text != "第一段正文第二段正文" {
		t.Fatalf("取消后应保留已产出，got=%q", text)
	}
	// run1 映射已清理
	if sid := a.liveSessionID("run1"); sid != "" {
		t.Fatal("run 结束后应清理 taskID→sessionID 映射")
	}
}
