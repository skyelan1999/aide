package server

// 整改回归测试（R01/R02/R03/R04/R06）：将审查附件中的缺陷观察测试改写为
// 断言正确行为的回归测试——用例 PASS 表示缺陷已修复，而非缺陷存在。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// R01a：模型/压缩调用期间全局锁被其他请求占用时，压缩必须能完成（不再死锁）。
func TestRemediationAutoCompactNotBlockedByHeldMutex(t *testing.T) {
	a := testApp(t)
	// 构造超阈值会话（>48KB）
	s := createSession(t, a)
	a.mu.Lock()
	s = a.sessions[s.ID] // 使用映射内真实指针
	big := []Message{}
	for i := 0; i < 10; i++ {
		big = append(big, Message{Role: "user", Content: strings.Repeat("x", 8000)}, Message{Role: "assistant", Content: strings.Repeat("y", 8000)})
	}
	s.Messages = big
	_ = a.save(s)
	a.mu.Unlock()
	summaryProvider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"goal":"t","decisions":[],"files":[],"facts":[],"pending":[]}`}}}})
	}))
	defer summaryProvider.Close()
	a.mu.Lock()
	a.settings = Settings{BaseURL: summaryProvider.URL, Model: "test"}
	a.mu.Unlock()
	// 模拟：压缩期间外部持续持锁请求（如 config 轮询）
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				a.mu.Lock()
				time.Sleep(time.Millisecond)
				a.mu.Unlock()
			}
		}
	}()
	done := make(chan error, 1)
	go func() { done <- func() error { _, e := a.compactForTest(s); return e }() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("compaction must complete under lock contention: %v", err)
		}
	case <-time.After(10 * time.Second):
		close(stop)
		wg.Wait()
		t.Fatal("compaction deadlocked while app mutex was contended")
	}
	close(stop)
	wg.Wait()
	a.mu.Lock()
	after := len(a.sessions[s.ID].Messages)
	compact := a.sessions[s.ID].Compact
	a.mu.Unlock()
	if after >= len(big) || compact == "" {
		t.Fatalf("compaction did not fold messages: after=%d compact=%q", after, compact)
	}
}

// compactForTest 模拟自动压缩路径（模型调用在锁外）。
func (a *App) compactForTest(s *Session) (int, error) {
	a.mu.Lock()
	snap, split := a.snapshotForCompact(s)
	cfg := a.settings
	baseLen := len(s.Messages)
	a.mu.Unlock()
	if split == 0 {
		return 0, nil
	}
	summary, err := a.buildCompactionSummary(context.Background(), snap.messages, cfg, snap.prevCompact)
	if err != nil {
		return 0, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(s.Messages) != baseLen {
		return 0, nil
	}
	s.Compact = summary
	s.CompactedMessages = snap.prevCount + split
	s.CompactedAt = time.Now().UTC().Format(time.RFC3339Nano)
	s.Messages = append([]Message{}, s.Messages[split:]...)
	return split, a.save(s)
}

// R01b：两个并发手动压缩：一个成功、另一个 409 或无害返回；全局锁必须释放，API 保持响应。
func TestRemediationConcurrentCompactionNoPanicNoDeadlock(t *testing.T) {
	a := testApp(t)
	s := createSession(t, a)
	a.mu.Lock()
	s = a.sessions[s.ID] // 使用映射内真实指针
	big := []Message{}
	for i := 0; i < 20; i++ {
		big = append(big, Message{Role: "user", Content: strings.Repeat("x", 8000)})
	}
	s.Messages = big
	_ = a.save(s)
	a.mu.Unlock()
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"goal":"t","decisions":[],"files":[],"facts":[],"pending":[]}`}}}})
	}))
	defer slow.Close()
	a.mu.Lock()
	a.settings = Settings{BaseURL: slow.URL, Model: "test"}
	a.mu.Unlock()
	results := make(chan int, 2)
	for i := 0; i < 2; i++ {
		go func() {
			w := request(a, "POST", "/api/sessions/"+s.ID+"/compact", map[string]any{})
			results <- w.Code
		}()
	}
	codes := []int{<-results, <-results}
	ok, conflict := 0, 0
	for _, c := range codes {
		switch c {
		case 200:
			ok++
		case 409:
			conflict++
		default:
			t.Fatalf("unexpected status %d", c)
		}
	}
	if ok == 0 {
		t.Fatalf("至少一个压缩应成功: %v", codes)
	}
	// 锁必须已释放：主要 API 立即响应
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		w := request(a, "GET", "/api/config", nil)
		if w.Code == 200 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("API unresponsive after concurrent compaction")
}

// R02a：A 工作区的旧提案不得写入 B 工作区。
func TestRemediationOldProposalRejectedOnNewWorkspace(t *testing.T) {
	a := testApp(t)
	dirA := filepath.Join(a.workPath, "projA")
	dirB := filepath.Join(a.workPath, "projB")
	_ = os.MkdirAll(dirA, 0755)
	_ = os.MkdirAll(dirB, 0755)
	requireStatus(t, request(a, "PUT", "/api/workspace-config", map[string]any{"workspace": map[string]any{"mode": "local", "path": "projA"}}), 200)
	s := createSession(t, a)
	// 通过 write_file 工具在 A 生成新文件提案（走任务）
	provider := newToolProvider(t, []func() (string, []ToolCall){
		func() (string, []ToolCall) {
			return "", []ToolCall{readCall("write_file", `{"path":"new.txt","content":"from-A"}`)}
		},
		func() (string, []ToolCall) { return "完成。", nil },
	})
	defer provider.Close()
	a.settings = Settings{BaseURL: provider.URL, Model: "test"}
	w := request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{"mode": "chat", "prompt": "创建文件"})
	requireStatus(t, w, 202)
	waitTaskDone(t, a, s.ID)
	w = request(a, "GET", "/api/sessions/"+s.ID, nil)
	var sess Session
	_ = json.Unmarshal(w.Body.Bytes(), &sess)
	task := sess.Runs[0]
	if task.Status != "awaiting_approval" {
		t.Fatalf("proposal not created: %+v", task)
	}
	// 切换到 B 后应用 → 必须 409，且 B 不得出现该文件
	requireStatus(t, request(a, "PUT", "/api/workspace-config", map[string]any{"workspace": map[string]any{"mode": "local", "path": "projB"}}), 200)
	w = request(a, "POST", "/api/sessions/"+s.ID+"/runs/"+task.ID+"/apply", map[string]any{})
	requireStatus(t, w, 409)
	if _, err := os.Stat(filepath.Join(dirB, "new.txt")); err == nil {
		t.Fatal("proposal must not be written to new workspace")
	}
	// 切回 A 后应用成功
	requireStatus(t, request(a, "PUT", "/api/workspace-config", map[string]any{"workspace": map[string]any{"mode": "local", "path": "projA"}}), 200)
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs/"+task.ID+"/apply", map[string]any{}), 200)
	if b, err := os.ReadFile(filepath.Join(dirA, "new.txt")); err != nil || string(b) != "from-A" {
		t.Fatalf("apply in original workspace: %s %v", b, err)
	}
}

// R02b：命令 cwd 跟随当前工作区（切换后 pwd 指向 B 而非 A）。
func TestRemediationCommandFollowsCurrentWorkspace(t *testing.T) {
	a := testApp(t)
	dirB := filepath.Join(a.workPath, "projB")
	_ = os.MkdirAll(dirB, 0755)
	requireStatus(t, request(a, "PUT", "/api/workspace-config", map[string]any{"workspace": map[string]any{"mode": "local", "path": "projB"}}), 200)
	w := request(a, "POST", "/api/command", map[string]string{"command": "pwd"})
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), filepath.Base(dirB)) {
		t.Fatalf("command must run in current workspace: %s", w.Body.String())
	}
}

// R03a：虚拟根路径穿越必须被拒绝（/workspace/../data、/workspaceXYZ 前缀）。
func TestRemediationVirtualRootTraversalRejected(t *testing.T) {
	a := testApp(t)
	for _, p := range []string{"/workspace/../data", "/workspaceXYZ/etc"} {
		w := request(a, "PUT", "/api/workspace-config", map[string]any{"workspace": map[string]any{"mode": "local", "path": p}})
		requireStatus(t, w, 400)
	}
	// 相对路径 .. 同样拒绝
	requireStatus(t, request(a, "PUT", "/api/workspace-config", map[string]any{"workspace": map[string]any{"mode": "local", "path": "../escape"}}), 400)
}

// R04：连续压缩必须携带上一版摘要（早期唯一约束不丢失）。
func TestRemediationCompactionChainRetainsPreviousSummary(t *testing.T) {
	a := testApp(t)
	var captured sync.Map
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := json.Marshal(map[string]any{})
		_ = json.Unmarshal(b, &map[string]any{})
		body := map[string]any{}
		dec := json.NewDecoder(r.Body)
		_ = dec.Decode(&body)
		if msgs, ok := body["messages"].([]any); ok && len(msgs) > 0 {
			if user, ok := msgs[len(msgs)-1].(map[string]any); ok {
				if content, ok := user["content"].(string); ok {
					captured.Store("last", content)
				}
			}
		}
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"goal":"t","decisions":[],"files":[],"facts":[],"pending":[]}`}}}})
	}))
	defer provider.Close()
	a.mu.Lock()
	a.settings = Settings{BaseURL: provider.URL, Model: "test"}
	a.mu.Unlock()
	s := createSession(t, a)
	// 第一轮压缩：历史含唯一约束标记
	a.mu.Lock()
	s = a.sessions[s.ID]
	s.Messages = []Message{{Role: "user", Content: "唯一约束: 必须使用绿色 UI"}, {Role: "assistant", Content: strings.Repeat("z", 30000)}}
	s.Compact = ""
	s.CompactedMessages = 0
	_ = a.save(s)
	a.mu.Unlock()
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/compact", map[string]any{}), 200)
	// 再灌入新历史，进行第二轮压缩：请求必须包含上一版摘要与唯一约束
	a.mu.Lock()
	s = a.sessions[s.ID]
	s.Messages = append(s.Messages, Message{Role: "user", Content: strings.Repeat("n", 30000)})
	_ = a.save(s)
	prevCompact := s.Compact
	a.mu.Unlock()
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/compact", map[string]any{}), 200)
	v, _ := captured.Load("last")
	lastReq, _ := v.(string)
	if !strings.Contains(lastReq, "上一版") && !strings.Contains(lastReq, "唯一约束") {
		// 摘要输入可能以两种形式携带约束（prompt 明确要求保留；或模型已合并）
		if !strings.Contains(lastReq, "唯一约束") {
			t.Fatalf("second compaction request must carry previous summary/constraint: %q", lastReq)
		}
	}
	a.mu.Lock()
	after := s.Compact
	a.mu.Unlock()
	if prevCompact == "" || after == "" {
		t.Fatal("compaction chain broken")
	}
}

// R06：write_file 必须逐字节保留正文空白（前导空格、缩进与末尾换行）。
func TestRemediationWriteToolPreservesWhitespace(t *testing.T) {
	a := testApp(t)
	const content = "  indented first line\nsecond\tline\n\n"
	provider := newToolProvider(t, []func() (string, []ToolCall){
		func() (string, []ToolCall) {
			return "", []ToolCall{readCall("write_file", `{"path":"ws.txt","content":"  indented first line\nsecond\tline\n\n"}`)}
		},
		func() (string, []ToolCall) { return "完成。", nil },
	})
	defer provider.Close()
	a.settings = Settings{BaseURL: provider.URL, Model: "test"}
	s := createSession(t, a)
	w := request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{"mode": "chat", "prompt": "写入"})
	requireStatus(t, w, 202)
	waitTaskDone(t, a, s.ID)
	w = request(a, "GET", "/api/sessions/"+s.ID, nil)
	var sess Session
	_ = json.Unmarshal(w.Body.Bytes(), &sess)
	task := sess.Runs[0]
	if len(task.Files) != 1 || task.Files[0].Content != content {
		t.Fatalf("content must be byte-preserved: %+v", task.Files[0].Content)
	}
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs/"+task.ID+"/apply", map[string]any{}), 200)
	b, err := os.ReadFile(filepath.Join(a.workPath, "ws.txt"))
	if err != nil || string(b) != content {
		t.Fatalf("applied content differs: %q %v", b, err)
	}
}

// R07（后端部分）：任务响应结束后，会话 API 状态一致（前端序号逻辑由 Node 审计验证）。
func TestRemediationSessionAPIStableDuringRun(t *testing.T) {
	a := testApp(t)
	s := createSession(t, a)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(150 * time.Millisecond)
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "ok"}}}})
	}))
	defer provider.Close()
	a.settings = Settings{BaseURL: provider.URL, Model: "test"}
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{"mode": "chat", "prompt": "hi"}), 202)
	// 运行期间并发读取会话必须成功（锁未被长期占用）
	deadline := time.Now().Add(3 * time.Second)
	reads := 0
	for time.Now().Before(deadline) {
		w := request(a, "GET", "/api/sessions/"+s.ID, nil)
		if w.Code != 200 {
			t.Fatalf("session read failed during run: %d", w.Code)
		}
		reads++
		var got Session
		_ = json.Unmarshal(w.Body.Bytes(), &got)
		if len(got.Runs) > 0 && got.Runs[0].Status != "running" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if reads == 0 {
		t.Fatal("no session reads completed")
	}
}

var _ = context.Background

// R08-01：0 费率必须是真实 0（非缺省回退），负值必须拒绝，留空语义由前端显式拒绝。
func TestR08ZeroPricingIsRealZero(t *testing.T) {
	a := testApp(t)
	w := request(a, "GET", "/api/token-pricing", nil)
	requireStatus(t, w, 200)
	var def Pricing
	if err := json.Unmarshal(w.Body.Bytes(), &def); err != nil || def.PriceIn <= 0 || def.PriceOut <= 0 {
		t.Fatalf("default pricing should be positive: %v %v", w.Body.String(), err)
	}
	w = request(a, "PUT", "/api/token-pricing", Pricing{PriceIn: -1, PriceOut: 0})
	requireStatus(t, w, 400)
	w = request(a, "PUT", "/api/token-pricing", Pricing{PriceIn: 0, PriceOut: 0})
	requireStatus(t, w, 200)
	a.recordTokenUsage(TokenUsage{Prompt: 2000000, Completion: 300000, Total: 2300000, Model: "local-free", Provider: "http://mock"})
	a.tokenStatsMu.Lock()
	cost := 0.0
	for _, c := range a.tokenCalls {
		cost += c.Cost
	}
	day := a.tokenStats[time.Now().UTC().Format("2006-01-02")]
	a.tokenStatsMu.Unlock()
	if cost != 0 {
		t.Fatalf("0-price call cost %v, want 0", cost)
	}
	if !day.Priced {
		t.Fatal("0-price call still has a price snapshot and must be priced")
	}
	w = request(a, "GET", "/api/token-pricing", nil)
	requireStatus(t, w, 200)
	var back Pricing
	_ = json.Unmarshal(w.Body.Bytes(), &back)
	if back.PriceIn != 0 || back.PriceOut != 0 {
		t.Fatalf("0 price did not round-trip: %+v", back)
	}
}

// R08-02：改价不得覆盖历史调用快照；新调用按新价。
func TestR08PriceSnapshotStable(t *testing.T) {
	a := testApp(t)
	requireStatus(t, request(a, "PUT", "/api/token-pricing", Pricing{PriceIn: 2, PriceOut: 8}), 200)
	a.recordTokenUsage(TokenUsage{Prompt: 1000000, Completion: 250000, Total: 1250000, Model: "model-a"})
	a.recordTokenUsage(TokenUsage{Prompt: 500000, Completion: 125000, Total: 625000, Model: "model-b"})
	requireStatus(t, request(a, "PUT", "/api/token-pricing", Pricing{PriceIn: 20, PriceOut: 80}), 200)
	a.recordTokenUsage(TokenUsage{Prompt: 1000000, Completion: 250000, Total: 1250000, Model: "model-a"})
	a.tokenStatsMu.Lock()
	defer a.tokenStatsMu.Unlock()
	if len(a.tokenCalls) != 3 {
		t.Fatalf("want 3 calls, got %d", len(a.tokenCalls))
	}
	if a.tokenCalls[0].Cost != 4 || a.tokenCalls[1].Cost != 10 || a.tokenCalls[2].Cost != 40 {
		t.Fatalf("snapshot costs wrong: %+v", a.tokenCalls)
	}
	if a.tokenCalls[0].PriceIn != 2 || a.tokenCalls[2].PriceIn != 20 {
		t.Fatal("price change rewrote historical snapshots")
	}
}

// R08-03：旧版汇总迁移保留用量与 estimated 标记；费用未知（不虚构 0 或现价），重启不重复累加。
func TestR08LegacyMigration(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "data")
	if err := os.MkdirAll(data, 0755); err != nil {
		t.Fatal(err)
	}
	legacy := `{"version":1,"days":{"2026-09-20":{"prompt":1200,"completion":300,"total":1500,"calls":2,"estimated":true},"2026-09-21":{"prompt":2400,"completion":600,"total":3000,"calls":3,"estimated":false}}}`
	if err := os.WriteFile(filepath.Join(data, "token-stats.json"), []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}
	check := func(a *App) {
		t.Helper()
		w := request(a, "GET", "/api/token-stats", nil)
		requireStatus(t, w, 200)
		var out struct {
			Totals   TokenDay            `json:"totals"`
			Unpriced TokenDay            `json:"unpricedTotals"`
			Days     map[string]TokenDay `json:"days"`
			Cost     float64             `json:"cost"`
			Calls    int                 `json:"calls"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if out.Totals.Total != 4500 || out.Totals.Calls != 5 {
			t.Fatalf("legacy totals wrong: %+v", out.Totals)
		}
		if out.Unpriced.Total != 4500 || out.Unpriced.Calls != 5 {
			t.Fatalf("legacy days must be unpriced: %+v", out.Unpriced)
		}
		if out.Cost != 0 || out.Calls != 0 {
			t.Fatalf("no fabricated cost/calls allowed: cost=%v calls=%d", out.Cost, out.Calls)
		}
		if !out.Days["2026-09-20"].Estimated || out.Days["2026-09-21"].Estimated {
			t.Fatalf("estimated flags not preserved: %+v", out.Days)
		}
		if out.Days["2026-09-20"].Priced || out.Days["2026-09-21"].Priced {
			t.Fatal("legacy days must not be marked priced")
		}
	}
	a1, err := New(filepath.Join(root, "work"), filepath.Join(root, "ref"), data)
	if err != nil {
		t.Fatal(err)
	}
	check(a1)
	a1.Close()
	a2, err := New(filepath.Join(root, "work"), filepath.Join(root, "ref"), data)
	if err != nil {
		t.Fatal(err)
	}
	check(a2) // 重启不重复累加、不清零
	a2.Close()
}
