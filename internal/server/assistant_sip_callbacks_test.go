package server

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/gorilla/websocket"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const sipFixtureKey = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func sipFixtureRequest(a *App, path string, body any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	r := httptest.NewRequest("POST", path, bytes.NewReader(raw))
	r.Header.Set("Authorization", "Bearer "+a.token)
	r.Header.Set("X-Aide-SIP-Key", sipFixtureKey)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	return w
}
func sipFixture(t *testing.T) (*App, context.Context, *Session, *Task) {
	t.Helper()
	a := testApp(t)
	unlockAssistantSessionForTest(t, a)
	id := assistantSessionID(t, a)
	if err := a.saveSIPJSON("callback-settings.json", assistantSIPCallbackSettings{true}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.dataPath, "sip", "host-key"), []byte(sipFixtureKey), 0600); err != nil {
		t.Fatal(err)
	}
	status := assistantSIPStatus{Server: "10.0.0.1", Phone: "10.0.0.2", Port: 5060, Account: "1001", Extension: "1000", Mode: "normal", State: "waiting", UpdatedAt: time.Now().UTC()}
	if err := a.saveSIPJSON("host-status.json", status); err != nil {
		t.Fatal(err)
	}
	w := sipFixtureRequest(a, "/api/assistant-sip/calls", map[string]any{"action": "open", "assistantId": id, "phone": status.Phone, "account": status.Account})
	requireStatus(t, w, 200)
	var ticket struct {
		CallID string `json:"callId"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &ticket); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Aide-SIP-Key", sipFixtureKey)
	r.Header.Set("X-Aide-SIP-Call", ticket.CallID)
	scope, err := a.bindSIPSocket(r, id, a.wsID())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.bindSIPSocket(r, id, a.wsID()); err == nil {
		t.Fatal("ticket replay accepted")
	}
	ctx := context.WithValue(context.Background(), assistantSIPContextKey{}, scope)
	run := &Task{ID: "callback-run", Status: "running", AssistantManaged: true, WorkspaceID: a.wsID(), Steps: []Step{{Name: "chat", Content: "README.md 存在；没有写入文件。"}}}
	s := &Session{ID: "callback-task", Number: 900, Created: time.Now().UTC().Format(time.RFC3339Nano), Runs: []*Task{run}}
	a.mu.Lock()
	a.sessions[s.ID] = s
	a.mu.Unlock()
	return a, ctx, s, run
}
func sipFixtureQueue(t *testing.T, a *App, ctx context.Context, s *Session, run *Task) assistantSIPCallback {
	t.Helper()
	if got := a.attachSIPTask(ctx, s.ID, run.ID); !strings.Contains(got, "已安排") {
		t.Fatal(got)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	jobs, err := a.sipCallbacksLocked()
	if err != nil || len(jobs) != 1 {
		t.Fatalf("jobs %v %v", jobs, err)
	}
	return jobs[0]
}
func sipFixtureHangup(t *testing.T, a *App, ctx context.Context) {
	t.Helper()
	a.endSIPSocket(sipContext(ctx).CallID)
	a.mu.Lock()
	defer a.mu.Unlock()
	jobs, _ := a.sipCallbacksLocked()
	for i := range jobs {
		jobs[i].OriginClosed = time.Now().Add(-4 * time.Second)
	}
	if err := a.saveSIPCallbacksLocked(jobs); err != nil {
		t.Fatal(err)
	}
}
func sipFixtureClaim(t *testing.T, a *App) string {
	t.Helper()
	w := sipFixtureRequest(a, "/api/assistant-sip/callbacks/claim", map[string]any{})
	requireStatus(t, w, 200)
	var reply struct {
		Callback *struct {
			ID string `json:"id"`
		} `json:"callback"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(w.Body.String(), "README") {
		t.Fatal("result escaped claim")
	}
	if reply.Callback == nil {
		return ""
	}
	return reply.Callback.ID
}
func TestAssistantSIPCallbackWaitsForHangupAndActualCompletion(t *testing.T) {
	a, ctx, s, run := sipFixture(t)
	job := sipFixtureQueue(t, a, ctx, s, run)
	if len(a.sipCallbackTools(context.Background())) != 0 || len(a.sipCallbackTools(ctx)) != 1 {
		t.Fatal("wrong tool surface")
	}
	if got := a.attachSIPTask(ctx, s.ID, run.ID); !strings.Contains(got, "不会重复") {
		t.Fatal(got)
	}
	if sipFixtureClaim(t, a) != "" {
		t.Fatal("dialed active call")
	}
	sipFixtureHangup(t, a, ctx)
	if sipFixtureClaim(t, a) != "" {
		t.Fatal("dialed running task")
	}
	a.mu.Lock()
	run.Status = "completed"
	run.Delivery = &TaskDelivery{State: "pending"}
	a.mu.Unlock()
	if sipFixtureClaim(t, a) != "" {
		t.Fatal("dialed undelivered work")
	}
	a.mu.Lock()
	run.Delivery = nil
	a.mu.Unlock()
	if got := sipFixtureClaim(t, a); got != job.ID {
		t.Fatalf("claim %q", got)
	}
	if sipFixtureClaim(t, a) != "" {
		t.Fatal("duplicate claim")
	}
	// A claimed dial is not replayed even if all in-memory source calls disappear.
	a.mu.Lock()
	a.assistantSIPCalls = nil
	a.mu.Unlock()
	if sipFixtureClaim(t, a) != "" {
		t.Fatal("replayed after restart")
	}
}
func TestAssistantSIPCallbackRejectsOldTasksAndBrowser(t *testing.T) {
	a, ctx, s, run := sipFixture(t)
	oldCreated := time.Now().Add(-time.Hour).Format(time.RFC3339Nano)
	s.Created = oldCreated
	before, _ := json.Marshal(s)
	if got := a.attachSIPTask(ctx, s.ID, run.ID); got != "" {
		t.Fatal("old task attached", got)
	}
	s.Created = time.Now().UTC().Format(time.RFC3339Nano)
	if got := a.attachSIPTask(context.Background(), s.ID, run.ID); got != "" {
		t.Fatal("browser attached", got)
	}
	s.Created = oldCreated
	after, _ := json.Marshal(s)
	if !bytes.Equal(before, after) {
		t.Fatal("task changed")
	}
	w := request(a, "POST", "/api/assistant-sip/callbacks/claim", map[string]any{})
	requireStatus(t, w, 403)
	w = request(a, "POST", "/api/assistant-sip/calls", map[string]any{"action": "open"})
	requireStatus(t, w, 403)
}
func TestAssistantSIPCallbackSingleConcurrentClaimAndRelease(t *testing.T) {
	a, ctx, s, run := sipFixture(t)
	job := sipFixtureQueue(t, a, ctx, s, run)
	sipFixtureHangup(t, a, ctx)
	a.mu.Lock()
	run.Status = "completed"
	a.mu.Unlock()
	var wg sync.WaitGroup
	var mu sync.Mutex
	count := 0
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := sipFixtureRequest(a, "/api/assistant-sip/callbacks/claim", map[string]any{})
			if w.Code != 200 {
				t.Error(w.Body.String())
				return
			}
			if strings.Contains(w.Body.String(), job.ID) {
				mu.Lock()
				count++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if count != 1 {
		t.Fatalf("%d claims", count)
	}
	w := sipFixtureRequest(a, "/api/assistant-sip/callbacks/release", map[string]any{"id": job.ID})
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), "true") {
		t.Fatal(w.Body.String())
	}
	if sipFixtureClaim(t, a) != job.ID {
		t.Fatal("pre-dial release lost job")
	}
	w = sipFixtureRequest(a, "/api/assistant-sip/calls", map[string]any{"action": "open", "assistantId": assistantSessionID(t, a), "phone": job.Phone, "account": job.Account, "callbackId": job.ID})
	requireStatus(t, w, 200)
	var ticket struct {
		CallID string `json:"callId"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &ticket)
	w = sipFixtureRequest(a, "/api/assistant-sip/callbacks/release", map[string]any{"id": job.ID})
	if !strings.Contains(w.Body.String(), "false") {
		t.Fatal("released dialing job")
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Aide-SIP-Key", sipFixtureKey)
	r.Header.Set("X-Aide-SIP-Call", ticket.CallID)
	scope, err := a.bindSIPSocket(r, job.AssistantID, a.wsID())
	if err != nil {
		t.Fatal(err)
	}
	reportCtx := context.WithValue(context.Background(), assistantSIPContextKey{}, scope)
	if len(a.sipCallbackTools(reportCtx)) != 0 {
		t.Fatal("callback exposed tools")
	}
	opening, err := a.sipCallbackOpening(reportCtx)
	if err != nil || !strings.Contains(opening, "README.md") {
		t.Fatal(opening, err)
	}
	if !a.validSIPCallback(reportCtx) {
		t.Fatal("invalid live callback")
	}
	if got := a.execAssistantToolWithContext(reportCtx, ToolCall{}, &assistantDecision{}); !strings.Contains(got, "不执行审批") {
		t.Fatal(got)
	}
	w = sipFixtureRequest(a, "/api/assistant-sip/calls", map[string]any{"action": "close", "callId": ticket.CallID, "outcome": "reported"})
	requireStatus(t, w, 200)
	w = sipFixtureRequest(a, "/api/assistant-sip/calls", map[string]any{"action": "close", "callId": ticket.CallID, "outcome": "failed"})
	requireStatus(t, w, 200)
	a.mu.Lock()
	jobs, _ := a.sipCallbacksLocked()
	a.mu.Unlock()
	if jobs[0].State != "reported" {
		t.Fatal(jobs[0].State)
	}
	if sipFixtureClaim(t, a) != "" {
		t.Fatal("redialed report")
	}
}
func TestAssistantSIPCallbackDisableCancelsWithoutTaskMutation(t *testing.T) {
	a, ctx, s, run := sipFixture(t)
	sipFixtureQueue(t, a, ctx, s, run)
	before, _ := json.Marshal(s)
	w := request(a, "POST", "/api/assistant-sip/callback-settings", map[string]any{"enabled": false})
	requireStatus(t, w, 200)
	after, _ := json.Marshal(s)
	if !bytes.Equal(before, after) {
		t.Fatal("setting changed task")
	}
	a.mu.Lock()
	jobs, _ := a.sipCallbacksLocked()
	a.mu.Unlock()
	if jobs[0].State != "canceled" {
		t.Fatal(jobs)
	}
	if sipFixtureClaim(t, a) != "" {
		t.Fatal("disabled dial")
	}
}
func TestAssistantSIPCallbackPhoneMismatchAndPendingApproval(t *testing.T) {
	a, ctx, s, run := sipFixture(t)
	sipFixtureQueue(t, a, ctx, s, run)
	sipFixtureHangup(t, a, ctx)
	a.mu.Lock()
	run.Status = "awaiting_approval"
	a.mu.Unlock()
	before, _ := json.Marshal(s)
	if sipFixtureClaim(t, a) != "" {
		t.Fatal("called awaiting approval")
	}
	after, _ := json.Marshal(s)
	if !bytes.Equal(before, after) {
		t.Fatal("pending task changed")
	}
	a.mu.Lock()
	run.Status = "completed"
	jobs, _ := a.sipCallbacksLocked()
	jobs[0].Phone = "10.0.0.3"
	_ = a.saveSIPCallbacksLocked(jobs)
	a.mu.Unlock()
	if sipFixtureClaim(t, a) != "" {
		t.Fatal("dialed different phone")
	}
	a.mu.Lock()
	jobs, _ = a.sipCallbacksLocked()
	a.mu.Unlock()
	if jobs[0].State != "canceled" {
		t.Fatal(jobs)
	}
}

func TestAssistantSIPCallbackGatesAndUnansweredNeverRetry(t *testing.T) {
	for _, gate := range []string{"assistant-lock", "disabled-tool", "active-call", "expired"} {
		t.Run(gate, func(t *testing.T) {
			a, ctx, s, run := sipFixture(t)
			sipFixtureQueue(t, a, ctx, s, run)
			sipFixtureHangup(t, a, ctx)
			a.mu.Lock()
			run.Status = "completed"
			switch gate {
			case "assistant-lock":
				delete(a.assistantUnlocked, assistantSessionIDLockedSIP(a))
			case "disabled-tool":
				a.settings.DisabledTools = append(a.settings.DisabledTools, "phone_callback")
			case "active-call":
				if a.assistantRealtimeActive == nil {
					a.assistantRealtimeActive = map[string]bool{}
				}
				a.assistantRealtimeActive[assistantSessionIDLockedSIP(a)] = true
			case "expired":
				jobs, _ := a.sipCallbacksLocked()
				jobs[0].Created = time.Now().Add(-25 * time.Hour)
				_ = a.saveSIPCallbacksLocked(jobs)
			}
			a.mu.Unlock()
			if sipFixtureClaim(t, a) != "" {
				t.Fatal("gate ignored")
			}
		})
	}
	a, ctx, s, run := sipFixture(t)
	job := sipFixtureQueue(t, a, ctx, s, run)
	sipFixtureHangup(t, a, ctx)
	a.mu.Lock()
	run.Status = "completed"
	a.mu.Unlock()
	if sipFixtureClaim(t, a) != job.ID {
		t.Fatal("no claim")
	}
	w := sipFixtureRequest(a, "/api/assistant-sip/calls", map[string]any{"action": "open", "assistantId": job.AssistantID, "phone": job.Phone, "account": job.Account, "callbackId": job.ID})
	requireStatus(t, w, 200)
	var ticket struct {
		CallID string `json:"callId"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &ticket)
	w = sipFixtureRequest(a, "/api/assistant-sip/calls", map[string]any{"action": "close", "callId": ticket.CallID, "outcome": "unanswered"})
	requireStatus(t, w, 200)
	for i := 0; i < 3; i++ {
		if sipFixtureClaim(t, a) != "" {
			t.Fatal("retried unanswered")
		}
	}
	a.mu.Lock()
	jobs, _ := a.sipCallbacksLocked()
	a.mu.Unlock()
	if jobs[0].State != "unanswered" {
		t.Fatal(jobs)
	}
}
func assistantSessionIDLockedSIP(a *App) string { return a.findAssistantSessionLocked().ID }

func TestAssistantSIPCallbackNativeSocketHasNoToolsAndStartsReport(t *testing.T) {
	a, ctx, s, run := sipFixture(t)
	job := sipFixtureQueue(t, a, ctx, s, run)
	sipFixtureHangup(t, a, ctx)
	a.mu.Lock()
	run.Status = "completed"
	a.settings.Model = "test"
	a.settings.BaseURL = "https://space.cn-beijing.maas.aliyuncs.com/compatible-mode/v1"
	a.settings.Models = nil
	a.settings.ModelProviders = nil
	a.mu.Unlock()
	if e := a.vault.Put(VaultIDModelAPIKey, VaultTypeModelAPIKey, "fixture", []byte("synthetic-native-key"), ""); e != nil {
		t.Fatal(e)
	}
	if sipFixtureClaim(t, a) != job.ID {
		t.Fatal("no claim")
	}
	w := sipFixtureRequest(a, "/api/assistant-sip/calls", map[string]any{"action": "open", "assistantId": job.AssistantID, "phone": job.Phone, "account": job.Account, "callbackId": job.ID})
	requireStatus(t, w, 200)
	var ticket struct {
		CallID string `json:"callId"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &ticket)
	report := make(chan bool, 1)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, e := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if e != nil {
			return
		}
		defer ws.Close()
		_ = ws.WriteJSON(map[string]any{"type": "session.created"})
		var setup map[string]any
		if ws.ReadJSON(&setup) != nil {
			return
		}
		cfg := setup["session"].(map[string]any)
		if vad, ok := cfg["turn_detection"].(map[string]any); !ok || vad["type"] != "server_vad" || vad["threshold"] != 0.25 {
			t.Error("phone did not use acoustic VAD", cfg["turn_detection"])
		}
		if tools, ok := cfg["tools"].([]any); !ok || len(tools) != 0 {
			t.Error("callback exposed native tools", cfg["tools"])
		}
		if !strings.Contains(cfg["instructions"].(string), "README.md") {
			t.Error("missing verified result")
		}
		_ = ws.WriteJSON(map[string]any{"type": "session.updated"})
		for {
			var in map[string]any
			if ws.ReadJSON(&in) != nil {
				return
			}
			if in["type"] == "response.create" {
				report <- true
			}
		}
	}))
	defer provider.Close()
	dial := func(ctx context.Context, _ string, h http.Header) (*websocket.Conn, *http.Response, error) {
		return websocket.DefaultDialer.DialContext(ctx, "ws"+strings.TrimPrefix(provider.URL, "http"), h)
	}
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.SetPathValue("id", job.AssistantID)
		a.assistantRealtimeSocketWithDial(w, r, dial)
	}))
	defer local.Close()
	h := http.Header{"Origin": {local.URL}, "X-Aide-SIP-Key": {sipFixtureKey}, "X-Aide-SIP-Call": {ticket.CallID}}
	client, _, e := (&websocket.Dialer{Subprotocols: []string{"aide-realtime"}}).Dial("ws"+strings.TrimPrefix(local.URL, "http"), h)
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))
	var ready map[string]any
	if e = client.ReadJSON(&ready); e != nil {
		t.Fatal(e)
	}
	if ready["sipCallback"] != true || ready["type"] != "aide.ready" {
		t.Fatal(ready)
	}
	select {
	case <-report:
	case <-time.After(3 * time.Second):
		t.Fatal("callback waited for user instead of reporting")
	}
	_ = client.WriteJSON(map[string]any{"type": "aide.hangup"})
}

func TestAssistantSIPCallbackDispatchBeforeCallbackManagement(t *testing.T) {
	for _, engine := range []string{"builtin", "mixsoul"} {
		t.Run(engine, func(t *testing.T) {
			t.Setenv("AIDE_ASSISTANT_ENGINE", engine)
			a, ctx, _, _ := sipFixture(t)
			if len(a.sipCallbackTools(ctx)) != 0 {
				t.Fatal("callback tool exposed before a task existed")
			}
			count := 0
			model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count++
				var body struct {
					Tools []struct {
						Function struct {
							Name string `json:"name"`
						} `json:"function"`
					} `json:"tools"`
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				for _, tool := range body.Tools {
					if tool.Function.Name != "dispatch_to_aide" && tool.Function.Name != "push_to_session" {
						t.Error("unrelated tool could impersonate dispatch", tool.Function.Name)
					}
				}
				if count == 1 {
					jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "已经交给 aide 了，完成后回电话。"}}}})
					return
				}
				call := ToolCall{ID: "dispatch-readonly", Type: "function"}
				call.Function.Name = "dispatch_to_aide"
				call.Function.Arguments = `{"summary":"只读检查 README 是否存在，不写文件，不动待审任务，完成后回电话。"}`
				jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "tool_calls": []ToolCall{call}}}}})
			}))
			defer model.Close()
			d, err := a.runAssistantAgenticLoop(ctx, Settings{BaseURL: model.URL, Model: "fixture"}, "交给A的只读检查readme文件是否存在，不写文件，不动待审任务，完成后回电话。", "", "call")
			if err != nil || d.Action != "dispatch" || count != 2 {
				t.Fatalf("action %s calls %d err %v", d.Action, count, err)
			}
		})
	}
}

func TestAssistantSIPCallbackFollowsOnlySameTaskRecovery(t *testing.T) {
	a, ctx, s, run := sipFixture(t)
	job := sipFixtureQueue(t, a, ctx, s, run)
	sipFixtureHangup(t, a, ctx)
	a.mu.Lock()
	run.Status = "failed"
	run.Prompt = "检查README"
	run.Error = "fixture provider unavailable"
	follow := &Task{ID: "continuation", Status: "running", Prompt: "继续", WorkspaceID: a.wsID()}
	s.Runs = append(s.Runs, follow)
	a.mu.Unlock()
	if sipFixtureClaim(t, a) != "" {
		t.Fatal("reported before continuation ended")
	}
	a.mu.Lock()
	follow.Status = "completed"
	follow.Steps = []Step{{Name: "chat", Content: "README exists"}}
	a.mu.Unlock()
	if sipFixtureClaim(t, a) != job.ID {
		t.Fatal("did not follow actual recovery result")
	}
	a.mu.Lock()
	_, actual := a.callbackTaskLocked(job)
	a.mu.Unlock()
	if actual != follow {
		t.Fatal("reported stale failed run")
	}
	a.mu.Lock()
	follow.Prompt = "write unrelated files"
	_, actual = a.callbackTaskLocked(job)
	a.mu.Unlock()
	if actual != nil {
		t.Fatal("followed a different task")
	}
	if !sipTaskReady(run) {
		t.Fatal("terminal failure would leave user waiting forever")
	}
}

func TestAssistantSIPCallbackTrustedSpeechReportAndExplicitRetry(t *testing.T) {
	a, ctx, s, run := sipFixture(t)
	job := sipFixtureQueue(t, a, ctx, s, run)
	sipFixtureHangup(t, a, ctx)
	a.mu.Lock()
	run.Status = "completed"
	a.mu.Unlock()
	if sipFixtureClaim(t, a) != job.ID {
		t.Fatal("no claim")
	}
	w := sipFixtureRequest(a, "/api/assistant-sip/calls", map[string]any{"action": "open", "assistantId": job.AssistantID, "phone": job.Phone, "account": job.Account, "callbackId": job.ID})
	requireStatus(t, w, 200)
	var ticket struct {
		CallID string `json:"callId"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &ticket)
	w = sipFixtureRequest(a, "/api/assistant-sip/callbacks/report", map[string]any{"callId": ticket.CallID})
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), "项目说明文件") {
		t.Fatal("missing actual result")
	}
	w = sipFixtureRequest(a, "/api/assistant-sip/callbacks/report", map[string]any{"callId": ticket.CallID})
	requireStatus(t, w, 409)
	w = sipFixtureRequest(a, "/api/assistant-sip/calls", map[string]any{"action": "close", "callId": ticket.CallID, "outcome": "failed"})
	requireStatus(t, w, 200)
	if sipFixtureClaim(t, a) != "" {
		t.Fatal("automatic redial after failed playback")
	}
	w = sipFixtureRequest(a, "/api/assistant-sip/calls", map[string]any{"action": "retry-callback", "callbackId": job.ID})
	requireStatus(t, w, 200)
	if sipFixtureClaim(t, a) != job.ID {
		t.Fatal("explicit retry lost")
	}
}

func TestAssistantSIPCallbackLostClaimResponseIsIdempotent(t *testing.T) {
	a, ctx, s, run := sipFixture(t)
	job := sipFixtureQueue(t, a, ctx, s, run)
	sipFixtureHangup(t, a, ctx)
	a.mu.Lock()
	run.Status = "completed"
	a.mu.Unlock()
	for i := 0; i < 2; i++ {
		w := sipFixtureRequest(a, "/api/assistant-sip/callbacks/claim", map[string]any{"requestId": "one-local-poll"})
		requireStatus(t, w, 200)
		if !strings.Contains(w.Body.String(), job.ID) {
			t.Fatal("lost undialed claim on response retry")
		}
	}
	w := sipFixtureRequest(a, "/api/assistant-sip/callbacks/claim", map[string]any{"requestId": "different-poll"})
	requireStatus(t, w, 200)
	if strings.Contains(w.Body.String(), job.ID) {
		t.Fatal("different poll stole reservation")
	}
}
