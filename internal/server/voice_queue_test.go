package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// 本文件覆盖 #41：小秘自行判断发送到 aide 时插队(insert)还是排队(queue)。
// 用 httptest 控制 analyze 返回的 JSON 决策，做到完全确定性；
// 调度本身复用 startTask 既有的 queued/Steer 机制（见 queue_steer_test.go），这里只验证
// analyze 输出 mode、冷却降级、中止豁免，以及 voiceFilter 把 mode 透出给前端。

// voiceDecisionSrv 让 analyze 的模型调用返回固定决策 JSON。
func voiceDecisionSrv(t *testing.T, decision string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonOut(w, 200, map[string]any{
			"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: decision}}},
		})
	}))
}

// newVoiceAgentClock 构造一个可注入时钟的小秘，便于确定性地测插队冷却。
func newVoiceAgentClock(t *testing.T) (*VoiceAgent, *time.Time) {
	t.Helper()
	va := newVoiceAgent(t.TempDir())
	cur := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	va.now = func() time.Time { return cur }
	return va, &cur
}

func dec(action, mode string, stop bool, text string) string {
	b, _ := json.Marshal(map[string]any{
		"action": action, "summarized": text, "mode": mode, "stop": stop,
		"reason": "测试判定",
	})
	return string(b)
}

// TestInsertUrgent 明确紧急措辞 → insert（首次不冷却）。
func TestInsertUrgent(t *testing.T) {
	srv := voiceDecisionSrv(t, dec("send", "insert", false, "赶紧把按钮改成蓝色"))
	defer srv.Close()
	va, _ := newVoiceAgentClock(t)
	cfg := Settings{BaseURL: srv.URL, Model: "test"}
	e, err := va.analyze(context.Background(), cfg, "赶紧把按钮改成蓝色", "")
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if e.Mode != "insert" {
		t.Fatalf("mode = %q, want insert", e.Mode)
	}
	if e.Stop {
		t.Fatal("urgent 不应标记 stop")
	}
}

// TestQueueNewTask 独立新任务 → queue。
func TestQueueNewTask(t *testing.T) {
	srv := voiceDecisionSrv(t, dec("send", "queue", false, "再帮我做个导出报表"))
	defer srv.Close()
	va, _ := newVoiceAgentClock(t)
	cfg := Settings{BaseURL: srv.URL, Model: "test"}
	e, _ := va.analyze(context.Background(), cfg, "再帮我做个导出报表", "")
	if e.Mode != "queue" {
		t.Fatalf("mode = %q, want queue", e.Mode)
	}
}

// TestQueueNormal 普通新需求 → queue。
func TestQueueNormal(t *testing.T) {
	srv := voiceDecisionSrv(t, dec("send", "queue", false, "写个 README"))
	defer srv.Close()
	va, _ := newVoiceAgentClock(t)
	cfg := Settings{BaseURL: srv.URL, Model: "test"}
	e, _ := va.analyze(context.Background(), cfg, "写个 README", "")
	if e.Mode != "queue" {
		t.Fatalf("mode = %q, want queue", e.Mode)
	}
}

// TestQueueFuzzy 模型给了无法识别的 mode（含空）→ 保守默认 queue。
func TestQueueFuzzy(t *testing.T) {
	srv := voiceDecisionSrv(t, dec("send", "maybe", false, "那个东西弄一下"))
	defer srv.Close()
	va, _ := newVoiceAgentClock(t)
	cfg := Settings{BaseURL: srv.URL, Model: "test"} // 默认 VoiceDefaultSendMode=queue
	e, _ := va.analyze(context.Background(), cfg, "那个东西弄一下", "")
	if e.Mode != "queue" {
		t.Fatalf("fuzzy mode = %q, want queue（保守默认）", e.Mode)
	}
}

// TestDefaultModeFallback 配置 VoiceDefaultSendMode=insert 时，模型拿不准（空 mode）才回落 insert。
func TestDefaultModeFallback(t *testing.T) {
	srv := voiceDecisionSrv(t, `{"action":"send","summarized":"随便一句","mode":"","reason":"拿不准"}`)
	defer srv.Close()
	va, _ := newVoiceAgentClock(t)
	cfg := Settings{BaseURL: srv.URL, Model: "test", VoiceDefaultSendMode: "insert"}
	e, _ := va.analyze(context.Background(), cfg, "随便一句", "")
	if e.Mode != "insert" {
		t.Fatalf("default fallback = %q, want insert（配置默认）", e.Mode)
	}
}

// TestModeRecorded 决策记录同时含 mode 与 reason，并落盘历史。
func TestModeRecorded(t *testing.T) {
	srv := voiceDecisionSrv(t, dec("send", "insert", true, "停下来别跑了"))
	defer srv.Close()
	va, _ := newVoiceAgentClock(t)
	cfg := Settings{BaseURL: srv.URL, Model: "test"}
	e, _ := va.analyze(context.Background(), cfg, "停下来别跑了", "")
	if e.Mode != "insert" || e.Reason == "" {
		t.Fatalf("entry 缺 mode/reason: %+v", e)
	}
	hist := va.historyDesc()
	if len(hist) != 1 || hist[0].Mode != "insert" {
		t.Fatalf("history mode = %+v", hist)
	}
}

// TestInsertCooldown 冷却窗口内的非中止类重复插队被降级为 queue；窗口过后恢复 insert。
func TestInsertCooldown(t *testing.T) {
	srv := voiceDecisionSrv(t, dec("send", "insert", false, "现在就要"))
	defer srv.Close()
	va, clk := newVoiceAgentClock(t)
	cfg := Settings{BaseURL: srv.URL, Model: "test", VoiceInsertSensitivity: "conservative"} // 5s
	// 第一次非中止插队 → insert，记录时间戳 T0
	e1, _ := va.analyze(context.Background(), cfg, "现在就要A", "")
	if e1.Mode != "insert" {
		t.Fatalf("e1 mode = %q, want insert", e1.Mode)
	}
	// 2 秒后又一条非中止插队 → 仍在 5s 冷却窗口内 → 降级 queue
	*clk = clk.Add(2 * time.Second)
	e2, _ := va.analyze(context.Background(), cfg, "现在就要B", "")
	if e2.Mode != "queue" {
		t.Fatalf("e2 mode = %q, want queue（冷却降级）", e2.Mode)
	}
	if !strings.Contains(e2.Reason, "冷却") {
		t.Fatalf("e2 reason 应说明冷却降级: %q", e2.Reason)
	}
	// 再 6 秒（距 T0 共 8s > 5s）→ 窗口已过 → 恢复 insert
	*clk = clk.Add(6 * time.Second)
	e3, _ := va.analyze(context.Background(), cfg, "现在就要C", "")
	if e3.Mode != "insert" {
		t.Fatalf("e3 mode = %q, want insert（窗口过后恢复）", e3.Mode)
	}
}

// TestAbortBypassesCooldown 明确中止/止损类（stop=true）始终插队、不受冷却限制。
func TestAbortBypassesCooldown(t *testing.T) {
	srv := voiceDecisionSrv(t, dec("send", "insert", true, "不对，停"))
	defer srv.Close()
	va, clk := newVoiceAgentClock(t)
	cfg := Settings{BaseURL: srv.URL, Model: "test"} // conservative 5s
	// 先制造一次非中止插队占用冷却窗口
	urgent := voiceDecisionSrv(t, dec("send", "insert", false, "马上"))
	defer urgent.Close()
	urgentCfg := Settings{BaseURL: urgent.URL, Model: "test"}
	if e, _ := va.analyze(context.Background(), urgentCfg, "马上", ""); e.Mode != "insert" {
		t.Fatalf("setup urgent mode = %q", e.Mode)
	}
	// 1 秒内连发两条中止指令：都必须立即插队（不被冷却降级）
	*clk = clk.Add(1 * time.Second)
	e1, _ := va.analyze(context.Background(), cfg, "不对，停", "")
	if e1.Mode != "insert" || !e1.Stop {
		t.Fatalf("abort e1 = %+v, want insert+stop", e1)
	}
	*clk = clk.Add(1 * time.Second)
	e2, _ := va.analyze(context.Background(), cfg, "不对，停", "")
	if e2.Mode != "insert" {
		t.Fatalf("abort e2 mode = %q, want insert（中止不受冷却限制）", e2.Mode)
	}
}

// TestInterruptedOutputPreserved 沿用 #35 StreamBroker：打断(interrupted)后已产出内容保留。
func TestInterruptedOutputPreserved(t *testing.T) {
	b := NewStreamBroker()
	b.Publish("s1", "", streamRunning)
	b.Publish("s1", "已经写了一半", "")
	b.Publish("s1", "", streamInterrupted)
	text, status, found := b.GetSessionLiveOutput("s1")
	if !found || status != streamInterrupted {
		t.Fatalf("status = %q, found=%v", status, found)
	}
	if text != "已经写了一半" {
		t.Fatalf("interrupted output lost: %q", text)
	}
}

// TestVoiceFilterSurfacesMode voiceFilter 把小秘 agentic 决策（dispatch_to_aide + urgent）
// 的 mode=insert/stop 原样返回给前端（语音据此刻插队打断当前 run）。
func TestVoiceFilterSurfacesMode(t *testing.T) {
	idx := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := idx
		idx++
		if i == 0 {
			// 小秘第一轮：调用 dispatch_to_aide（紧急中止→insert）
			calls := []ToolCall{{ID: "c1", Type: "function"}}
			calls[0].Function.Name = "dispatch_to_aide"
			calls[0].Function.Arguments = `{"summary":"立刻停","mode":"insert","urgent":true}`
			jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{
				"role": "assistant", "content": "", "tool_calls": calls,
			}}}})
			return
		}
		// 第二轮：工具结果回来后小秘收尾
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{
			"role": "assistant", "content": "好，已通知停止。",
		}}}})
	}))
	defer srv.Close()
	a := testApp(t)
	a.settings = Settings{BaseURL: srv.URL, Model: "test"}
	a.voiceAgent = newVoiceAgent(t.TempDir())
	unlockAssistantSessionForTest(t, a) // #62：voice-filter 需先过小秘密码门
	w := request(a, "POST", "/api/voice-filter", map[string]any{"text": "立刻停", "context": ""})
	requireStatus(t, w, 200)
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("body not json: %v", err)
	}
	if got["mode"] != "insert" {
		t.Fatalf("voice-filter mode = %v, want insert (body=%s)", got["mode"], w.Body.String())
	}
	if got["action"] != "send" {
		t.Fatalf("action = %v", got["action"])
	}
	if got["text"] != "立刻停" {
		t.Fatalf("dispatch text = %v", got["text"])
	}
}

// TestQueueEnqueuedDispatch mode=queue → queued=true：进入队列，不打断当前 run。
func TestQueueEnqueuedDispatch(t *testing.T) {
	a := testApp(t)
	a.settings = Settings{BaseURL: "http://unused", Model: "test"}
	s := &Session{ID: newID(), Title: "s", Messages: []Message{{Role: "user", Content: "第一个问题"}}}
	task := &Task{ID: newID(), Mode: "chat", Status: "running", Steer: make(chan string, 4), Steps: []Step{{Name: "chat", Status: "running"}}}
	s.Runs = append(s.Runs, task)
	a.sessions[s.ID] = s
	url := "/api/sessions/" + s.ID + "/runs"
	// 前端据 mode=queue 传 queued=true
	w := request(a, "POST", url, map[string]any{"prompt": "排队的新任务", "mode": "chat", "queued": true})
	requireStatus(t, w, 202)
	if len(task.Queue) != 1 || task.Queue[0] != "排队的新任务" {
		t.Fatalf("queue = %v", task.Queue)
	}
	// 当前 run 不被打断：Steer 通道为空
	select {
	case st := <-task.Steer:
		t.Fatalf("queue 不应打断当前 run，却收到 steer %q", st)
	default:
	}
}

// TestInsertInterruptsDispatch mode=insert → queued=false：立即送上 Steer 通道打断当前 run。
func TestInsertInterruptsDispatch(t *testing.T) {
	a := testApp(t)
	a.settings = Settings{BaseURL: "http://unused", Model: "test"}
	s := &Session{ID: newID(), Title: "s", Messages: []Message{{Role: "user", Content: "第一个问题"}}}
	task := &Task{ID: newID(), Mode: "chat", Status: "running", Steer: make(chan string, 4), Steps: []Step{{Name: "chat", Status: "running"}}}
	s.Runs = append(s.Runs, task)
	a.sessions[s.ID] = s
	url := "/api/sessions/" + s.ID + "/runs"
	// 前端据 mode=insert 传 queued=false
	w := request(a, "POST", url, map[string]any{"prompt": "马上停", "mode": "chat", "queued": false})
	requireStatus(t, w, 202)
	select {
	case st := <-task.Steer:
		if st != "马上停" {
			t.Fatalf("steer = %q", st)
		}
	case <-time.After(time.Second):
		t.Fatal("insert 应立即送上 Steer 通道")
	}
	if len(task.Queue) != 0 {
		t.Fatalf("insert 不应入队: %v", task.Queue)
	}
}

// TestDefaultQueuePersistent 重启后默认 queue 与敏感度配置被持久化并回退。
func TestDefaultQueuePersistent(t *testing.T) {
	s := &Settings{}
	normalizeLoadedSettings(s)
	if s.VoiceDefaultSendMode != "queue" {
		t.Fatalf("default VoiceDefaultSendMode = %q, want queue", s.VoiceDefaultSendMode)
	}
	if s.VoiceInsertSensitivity != "conservative" {
		t.Fatalf("default sensitivity = %q, want conservative", s.VoiceInsertSensitivity)
	}
	// 非法值回退
	s.VoiceDefaultSendMode = "nonsense"
	s.VoiceInsertSensitivity = "fast"
	validateSettings(s)
	if s.VoiceDefaultSendMode != "queue" || s.VoiceInsertSensitivity != "conservative" {
		t.Fatalf("invalid not reset: mode=%q sens=%q", s.VoiceDefaultSendMode, s.VoiceInsertSensitivity)
	}
	// insert/normal/aggressive 合法保留
	s.VoiceDefaultSendMode = "insert"
	s.VoiceInsertSensitivity = "aggressive"
	validateSettings(s)
	if s.VoiceDefaultSendMode != "insert" || s.VoiceInsertSensitivity != "aggressive" {
		t.Fatalf("valid values not kept: mode=%q sens=%q", s.VoiceDefaultSendMode, s.VoiceInsertSensitivity)
	}
}
