package server

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAssistantResultTriggerPersistentAndScoped(t *testing.T) {
	a := testApp(t)
	id := assistantSessionID(t, a)
	unlockAssistantSessionForTest(t, a)
	a.mu.Lock()
	a.sessions["weather-result"] = &Session{ID: "weather-result", Number: 963, Title: "Synthetic weather", FollowedByAssistant: true, Runs: []*Task{{ID: "weather-run", Status: "running"}}}
	a.sessions["other-result"] = &Session{ID: "other-result", Title: "Unfollowed", Runs: []*Task{{ID: "other-run", Status: "completed", Steps: []Step{{Name: "chat", Content: "UNFOLLOWED_SECRET"}}}}}
	a.mu.Unlock()
	a.publishAssistantResults()
	a.mu.Lock()
	before := len(a.sessions[id].Messages)
	run := a.sessions["weather-result"].Runs[0]
	run.Status = "completed"
	run.Steps = []Step{{Name: "plan", Content: "INTERNAL_PLAN"}, {Name: "chat", Content: "合成结果：西青晴，20℃，来源：隔离天气服务。", Reasoning: "PRIVATE_REASONING"}}
	a.sessions["weather-result"].Runs = append(a.sessions["weather-result"].Runs, &Task{ID: "next-running", Status: "running"})
	_ = a.save(a.sessions["weather-result"])
	a.mu.Unlock()
	// Observe automatic save -> wake -> durable message, without polling watch.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		n := len(a.sessions[id].Messages)
		a.mu.Unlock()
		if n > before {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	a.publishAssistantResults()
	a.publishAssistantResults()
	a.mu.Lock()
	s := a.sessions[id]
	if len(s.Messages) != before+1 {
		t.Fatalf("count %d expected %d", len(s.Messages), before+1)
	}
	message := s.Messages[len(s.Messages)-1]
	if message.Role != "assistant" || message.Type != "task-result" || message.SourceRunID != "weather-run" || !strings.Contains(message.Content, "20℃") || strings.Contains(message.Content, "PRIVATE") || strings.Contains(message.Content, "INTERNAL") || strings.Contains(message.Content, "UNFOLLOWED") {
		t.Fatalf("unsafe result: %+v", message)
	}
	raw, err := json.Marshal(s)
	a.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	var restored Session
	if err = json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.sessions[id] = &restored
	a.mu.Unlock()
	a.publishAssistantResults()
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.sessions[id].Messages) != before+1 {
		t.Fatal("persisted receipt replayed")
	}
}

func TestAssistantResultTriggerLockAndDelivery(t *testing.T) {
	a := testApp(t)
	id := assistantSessionID(t, a)
	a.mu.Lock()
	a.sessions["result"] = &Session{ID: "result", FollowedByAssistant: true, Runs: []*Task{{ID: "r", Status: "completed", Delivery: &TaskDelivery{State: "pending", Reason: "需审批"}, Steps: []Step{{Name: "chat", Content: "文件已生成，尚未应用。"}}}}}
	before := len(a.sessions[id].Messages)
	a.mu.Unlock()
	a.publishAssistantResults()
	a.mu.Lock()
	if len(a.sessions[id].Messages) != before {
		t.Fatal("locked assistant notified")
	}
	a.mu.Unlock()
	unlockAssistantSessionForTest(t, a)
	a.publishAssistantResults()
	a.mu.Lock()
	s := a.sessions[id]
	if len(s.Messages) != before+1 || !strings.Contains(s.Messages[len(s.Messages)-1].Content, "不能视为已交付") {
		t.Fatal("delivery claim incorrect")
	}
	a.sessions["result"].Runs[0].Delivery.State = "delivered"
	a.mu.Unlock()
	a.publishAssistantResults()
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.sessions[id].Messages) != before+2 {
		t.Fatal("delivery transition missing")
	}
}

func TestAssistantResultTriggerRealHTTP(t *testing.T) {
	a := testApp(t)
	id := assistantSessionID(t, a)
	unlockAssistantSessionForTest(t, a)
	srv := httptest.NewServer(a.Handler())
	defer srv.Close()
	// A real TCP client receives SSE notification and the persisted final body.
	req, _ := http.NewRequest("GET", srv.URL+"/api/events", nil)
	req.Header.Set("Authorization", "Bearer "+a.token)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req = req.WithContext(ctx)
	done := make(chan string, 1)
	go func() {
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			done <- ""
			return
		}
		defer res.Body.Close()
		// SSE is a stream: the connection comment may be its own TCP read.
		// Keep consuming complete frames until this session's notification arrives.
		scanner := bufio.NewScanner(res.Body)
		var frame strings.Builder
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				if strings.Contains(frame.String(), id) {
					done <- frame.String()
					return
				}
				frame.Reset()
			} else {
				frame.WriteString(line)
				frame.WriteByte('\n')
			}
		}
		done <- frame.String()
	}()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		a.eventMu.Lock()
		n := len(a.globalSubs)
		a.eventMu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	a.mu.Lock()
	a.sessions["http-result"] = &Session{ID: "http-result", Number: 964, Title: "Synthetic IP", FollowedByAssistant: true, Runs: []*Task{{ID: "http-run", Status: "completed", Steps: []Step{{Name: "chat", Content: "合成结果：203.0.113.9，测试城市。"}}}}}
	_ = a.save(a.sessions["http-result"])
	a.mu.Unlock()
	if event := <-done; !strings.Contains(event, id) {
		t.Fatalf("missing assistant SSE notification: %s", event)
	}
	get, _ := http.NewRequest("GET", srv.URL+"/api/sessions/"+id, nil)
	get.Header.Set("Authorization", "Bearer "+a.token)
	res, err := http.DefaultClient.Do(get)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 || !strings.Contains(string(body), "203.0.113.9") {
		t.Fatalf("result not returned over HTTP: %d", res.StatusCode)
	}
}

func TestAssistantResultTriggerFullRestart(t *testing.T) {
	a := testApp(t)
	id := assistantSessionID(t, a)
	unlockAssistantSessionForTest(t, a)
	a.mu.Lock()
	a.sessions["restart-result"] = &Session{ID: "restart-result", FollowedByAssistant: true, Runs: []*Task{{ID: "restart-run", Status: "completed", Steps: []Step{{Name: "chat", Content: "合成重启结果"}}}}}
	_ = a.save(a.sessions["restart-result"])
	a.mu.Unlock()
	a.publishAssistantResults()
	a.mu.Lock()
	count := len(a.sessions[id].Messages)
	a.mu.Unlock()
	work, ref, data := a.workPath, a.refPath, a.dataPath
	a.Close()
	restarted, err := New(work, ref, data)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	unlockAssistantSessionForTest(t, restarted)
	restarted.publishAssistantResults()
	restarted.mu.Lock()
	defer restarted.mu.Unlock()
	if len(restarted.sessions[id].Messages) != count {
		t.Fatal("notification duplicated after full App restart")
	}
}
