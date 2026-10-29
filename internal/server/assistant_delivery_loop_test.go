package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestAssistantDeliveryEvidenceGate(t *testing.T) {
	a := testApp(t)
	task := &Task{ID: newID(), Prompt: "生成MP3，内容：测试"}
	registerHarnessFixtureTask(t, a, task)
	if err := a.prepareTaskDeliveryBaseline(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if a.checkTaskDelivery(task, "已完成 [下载](missing.mp3)") == nil || task.Delivery.State != "missing" {
		t.Fatal("missing file accepted")
	}
	if err := putText(a.workspace, "fake.mp3", []byte("not audio")); err != nil {
		t.Fatal(err)
	}
	if a.checkTaskDelivery(task, "[下载](fake.mp3)") == nil {
		t.Fatal("wrong MIME accepted")
	}
	if err := putText(a.workspace, "real.mp3", []byte("ID3fixture audio")); err != nil {
		t.Fatal(err)
	}
	if err := a.checkTaskDelivery(task, "[下载](real.mp3)"); err != nil || task.Delivery.State != "delivered" {
		t.Fatal(err, task.Delivery)
	}
	if err := a.checkTaskDelivery(task, "[下载](../real.mp3)"); err == nil {
		t.Fatal("path traversal accepted")
	}
	task.Files = []Change{{Path: "real.mp3"}}
	if err := a.checkTaskDelivery(task, "提案待审批"); err != nil || task.Delivery.State != "pending_approval" {
		t.Fatal("proposal confused with delivery", err)
	}
	if a.inspectTaskDeliveryLocked(&Task{Prompt: "解释如何制作MP3的代码"}, "示例") != nil {
		t.Fatal("tutorial requires artifact")
	}
}
func TestAssistantDeliveryPromiseReturnsIncomplete(t *testing.T) {
	a := testApp(t)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { parallelReply(w, "我会生成图片。") }))
	defer provider.Close()
	task := &Task{ID: newID(), Prompt: "生成图片", Mode: "chat", Steps: []Step{{Name: "chat"}}, Steer: make(chan string, 1)}
	registerHarnessFixtureTask(t, a, task)
	_, _, err := a.toolLoop(context.Background(), Settings{BaseURL: provider.URL, Model: "fixture"}, []Message{{Role: "user", Content: task.Prompt}}, ProfileParams{}, nil, task, nil, 0)
	if !errors.Is(err, errDeliveryIncomplete) {
		t.Fatalf("promise returned successful completion: %v", err)
	}
}
func TestAssistantMediaIntentContinuity(t *testing.T) {
	for _, p := range []string{"你能不能帮我生成一个三秒视频", "小秘，请帮我生成MP3，内容：你好", "我想让你制作一张图片"} {
		if generationIntent(p) == "" {
			t.Fatal("missed explicit media", p)
		}
	}
	for _, p := range []string{"停止生成视频", "不要生成图片", "你能生成视频吗", "生成视频的教程", "解释图片", "生成图片的API怎么调用"} {
		if generationIntent(p) != "" {
			t.Fatal("accidental billable intent", p)
		}
	}
	prev := &Task{ID: "original", Prompt: "生成三秒视频", MediaGeneration: &MediaGeneration{JobID: "original-job"}}
	s := &Session{Runs: []*Task{prev}}
	p, got := sessionMediaContinuation(s, "继续下载")
	if got != prev || p != prev.Prompt {
		t.Fatal("lost original request")
	}
	p, got = sessionMediaContinuation(s, "总结这份文档")
	if got != nil || p != "总结这份文档" {
		t.Fatal("unrelated request inherited media")
	}
	prev.Status = "stopped"
	if _, got = sessionMediaContinuation(s, "继续"); got != nil {
		t.Fatal("stop reactivated generation")
	}
}
func TestAssistantMediaFailureClassification(t *testing.T) {
	for _, tc := range []struct {
		status     int
		body, kind string
	}{{429, `{"error":{"code":"insufficient_quota"}}`, "quota"}, {429, `{"error":{"code":"limit_requests"}}`, "rate_limit"}, {400, `{"error":{"message":"product is not activated"}}`, "activation"}, {403, `{}`, "auth"}, {400, `{}`, "parameter"}, {503, `{}`, "upstream"}} {
		e := classifyMediaError(tc.status, []byte(tc.body))
		var typed *mediaProviderError
		if !errors.As(e, &typed) || typed.Category != tc.kind {
			t.Fatal(e, tc.kind)
		}
	}
	a := testApp(t)
	cfg := Settings{Model: "fixture"}
	a.recordMediaHealth(cfg, classifyMediaError(429, []byte(`{"error":{"code":"limit_requests"}}`)))
	if !time.Now().Before(a.mediaHealth[mediaIdentity(cfg)].Until) {
		t.Fatal("no cooldown")
	}
	a.recordMediaHealth(cfg, nil)
	if len(a.mediaHealth) != 0 {
		t.Fatal("successful request did not clear cooldown")
	}
}
func TestAssistantParallelProgressBudgetAndSimilarity(t *testing.T) {
	if !similarParallelTitle("设计中文字体加载方案", "给出中文字体加载方案") || similarParallelTitle("architecture", "deployment") {
		t.Fatal("incorrect title equivalence")
	}
	a, s, task := parallelFixture(t, 2)
	task.Parallel = &ParallelProgress{Round: 3, Evidence: "older", Scope: parallelUserScope([]Message{{Role: "user", Content: "do work"}})}
	task.ParallelPrepared = true
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("exhausted planning still called model")
		parallelReply(w, `{"tasks":[]}`)
	}))
	defer provider.Close()
	input := []Message{{Role: "user", Content: "do work"}, {Role: "assistant", ToolCalls: []ToolCall{readCall("read_file", `{"path":"a"}`)}}, {Role: "tool", Content: "another probe result"}}
	_, err := a.prepareParallel(context.Background(), s, task, Settings{BaseURL: provider.URL, Model: "fixture"}, input, ProfileParams{})
	if err != nil || task.Parallel.Phase != "fallback" || task.Parallel.Round != 3 {
		t.Fatal(err, task.Parallel)
	}
	raw, _ := json.Marshal(task)
	var recovered Task
	json.Unmarshal(raw, &recovered)
	if recovered.Parallel.Scope != task.Parallel.Scope {
		t.Fatal("resume lost budget")
	}
}
func TestAssistantHistoryRetrievesOldMatchesAndPages(t *testing.T) {
	a := testApp(t)
	s := &Session{ID: "history-fixture", Number: 90, Title: "history", Updated: "2026-10-23", Messages: []Message{{Role: "user", Content: "old constraint MARKER 1"}, {Role: "assistant", Content: strings.Repeat("中", 800) + "MARKER 2"}}}
	for i := 0; i < 30; i++ {
		s.Messages = append(s.Messages, Message{Role: "user", Content: "unrelated"})
	}
	a.sessions[s.ID] = s
	first := a.searchAssistantHistory(`{"keyword":"MARKER","ref":"#90","limit":1}`)
	var page struct {
		Hits []assistantHistoryHit `json:"hits"`
		Next int                   `json:"nextCursor"`
	}
	json.Unmarshal([]byte(first), &page)
	if len(page.Hits) != 1 || !strings.Contains(page.Hits[0].Text, "MARKER 2") || page.Next != 1 || !utf8.ValidString(page.Hits[0].Text) {
		t.Fatal(first)
	}
	second := a.searchAssistantHistory(`{"keyword":"MARKER","ref":"#90","limit":1,"cursor":1}`)
	if !strings.Contains(second, "MARKER 1") || !strings.Contains(second, "session:history-fixture:message:1") {
		t.Fatal(second)
	}
	if strings.Contains(a.searchAssistantHistory(`{"keyword":"MARKER","ref":"#999"}`), "MARKER 1") {
		t.Fatal("invalid ref escaped search scope")
	}
}
func TestAssistantMemoryNotesSourceAndRevocation(t *testing.T) {
	a := testApp(t)
	note := "prefer source-backed answers"
	id := memoryNoteID(note)
	if !strings.Contains(a.rememberAssistantNote(note), id) {
		t.Fatal("note not recorded")
	}
	listed := a.assistantMemoryNotes(`{"action":"list"}`, "")
	if !strings.Contains(listed, "recordedAt") || !strings.Contains(listed, "assistant-private-memory") {
		t.Fatal(listed)
	}
	cmd := `{"action":"remove","id":"` + id + `"}`
	if strings.Contains(a.assistantMemoryNotes(cmd, "read history"), "已删除") {
		t.Fatal("history became deletion authority")
	}
	if strings.Contains(a.assistantMemoryNotes(cmd, "不要忘记 "+id), "已删除") {
		t.Fatal("negated request became removal authority")
	}
	if !strings.Contains(a.assistantMemoryNotes(cmd, "forget "+id), "已删除") {
		t.Fatal("explicit revoke failed")
	}
	if strings.Contains(a.assistantMemoryNotes(`{"action":"list"}`, ""), note) {
		t.Fatal("revoked note retained")
	}
}
func TestAssistantLedgerIncludesBlockersDeliveryAndSchedules(t *testing.T) {
	a := testApp(t)
	a.sessions["ledger"] = &Session{ID: "ledger", Number: 8, FollowedByAssistant: true, Runs: []*Task{{ID: "run", Status: "paused", Error: "delivery missing", CanResume: true, Delivery: &TaskDelivery{State: "missing", Reason: "no MP4"}}}}
	a.reminders = append(a.reminders, Reminder{ID: "schedule", Title: "authorized followup", Execution: &ReminderExecution{WorkspaceID: a.wsID(), State: "interrupted", Error: "restart boundary"}})
	rows := a.assistantWorkLedgerLocked()
	if len(rows) != 2 {
		t.Fatal(rows)
	}
	for _, r := range rows {
		if r.Priority != 3 || r.Blocker == "" || r.NextAction == "" {
			t.Fatal(r)
		}
	}
	if len(a.sessions) != 2 {
		t.Fatal("ledger started a new task")
	}
}

func TestAssistantScheduleSurfacesPausedDelivery(t *testing.T) {
	a := testApp(t)
	s := &Session{ID: "schedule-pause", Runs: []*Task{{ID: "paused-job", Status: "paused", Error: "delivery incomplete"}}}
	a.sessions[s.ID] = s
	a.reminders = append(a.reminders, Reminder{ID: "pause-follow", Execution: &ReminderExecution{State: "submitted", SessionID: s.ID, RunID: "paused-job", WorkspaceID: a.wsID()}})
	a.dispatchScheduledReminders(time.Now())
	if a.reminders[0].Execution.State != "interrupted" || a.reminders[0].Execution.Error != "delivery incomplete" {
		t.Fatal("paused schedule still appears in flight", a.reminders[0])
	}
}

func TestAssistantProjectMemoryRetrievesBeyondPrefix(t *testing.T) {
	a := testApp(t)
	p := filepath.Join(a.workPath, ".cache/aide/memory.md")
	os.MkdirAll(filepath.Dir(p), 0700)
	os.WriteFile(p, []byte(strings.Repeat("中", 5000)+"重要参数 UNIQUE-LOOKUP"), 0600)
	a.voiceAgent.setAideMemoryPath(p)
	got := a.voiceAgent.readAideMemoryFor("UNIQUE-LOOKUP")
	if !strings.Contains(got, "UNIQUE-LOOKUP") || !utf8.ValidString(got) {
		t.Fatal("lost query hit or invalid UTF-8")
	}
}

func TestAssistantNativeStopDoesNotQueueOrResubmit(t *testing.T) {
	a := testApp(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	task := &Task{ID: "native-stop", Status: "running", Mode: "chat", Steer: make(chan string, 1), MediaGeneration: &MediaGeneration{Kind: "video", JobID: "existing-remote"}}
	s := &Session{ID: "native-session", Messages: []Message{}, Runs: []*Task{task}}
	a.sessions[s.ID] = s
	a.cancels[task.ID] = cancel
	w := request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{"prompt": "停止", "mode": "chat"})
	requireStatus(t, w, 202)
	if !task.StopRequested || ctx.Err() != context.Canceled || len(task.Queue) != 0 || len(s.Runs) != 1 {
		t.Fatal("stop became a new generation or queued command")
	}
}

func TestAssistantMediaContinuationPinsModelWithoutAuto(t *testing.T) {
	a := testApp(t)
	a.settings = Settings{BaseURL: "http://127.0.0.1:1", Model: "text-model", ActiveModel: "text-model", Models: []ModelRef{{ID: "text-model", ContextWindow: 65536}, {ID: "wan2.7-t2v", ContextWindow: 65536}}}
	w := request(a, "POST", "/api/sessions", map[string]string{})
	requireStatus(t, w, 201)
	var session Session
	json.Unmarshal(w.Body.Bytes(), &session)
	prompt := "生成3秒视频"
	asset := "generated/video/qwen-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.mp4"
	if err := putText(a.workspace, asset, []byte("0000ftypisom")); err != nil {
		t.Fatal(err)
	}
	cfg, err := settingsForSelectedModel(a.settings, "wan2.7-t2v")
	if err != nil {
		t.Fatal(err)
	}
	previous := &Task{ID: newID(), Prompt: prompt, Status: "completed", Model: cfg.Model, MediaGeneration: &MediaGeneration{Kind: "video", Model: cfg.Model, Identity: mediaIdentity(cfg), State: "succeeded", JobID: "existing-job", Asset: asset, Format: "mp4", RequestHash: hash([]byte(prompt + "\n\nmp3"))}}
	a.mu.Lock()
	a.sessions[session.ID].Runs = append(a.sessions[session.ID].Runs, previous)
	a.mu.Unlock()
	w = request(a, "POST", "/api/sessions/"+session.ID+"/runs", map[string]string{"mode": "chat", "prompt": "继续下载"})
	requireStatus(t, w, 202)
	var accepted Task
	json.Unmarshal(w.Body.Bytes(), &accepted)
	if accepted.Model != "wan2.7-t2v" || accepted.MediaGeneration == nil || accepted.MediaGeneration.JobID != "existing-job" {
		t.Fatal("continuation followed current text model instead of existing media", accepted.Model)
	}
	waitDispatchedRunTerminal(t, a, session.ID)
	a.mu.Lock()
	last := a.sessions[session.ID].Runs[len(a.sessions[session.ID].Runs)-1]
	status := last.Status
	delivery := last.Delivery
	a.mu.Unlock()
	if status != "completed" || delivery == nil || delivery.State != "delivered" {
		t.Fatal(status, delivery)
	}
}
