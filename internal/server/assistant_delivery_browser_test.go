package server

import (
	"encoding/binary"
	"net"
	"net/http"
	"os"
	"testing"
	"time"
)

// Manual browser harness using the real product handler and entirely synthetic
// state. Opt-in only; no model keys, provider calls, user data or production ports.
func TestAssistantDeliveryBrowserFixture(t *testing.T) {
	address := os.Getenv("AIDE_ASSISTANT_BROWSER_ADDRESS")
	if address == "" {
		t.Skip("opt-in isolated real-workbench browser harness")
	}
	a := testApp(t)
	a.mu.Lock()
	a.token = "assistant-loop-synthetic-token"
	a.settings.VoiceReplyEnabled = false
	a.sessions["delivery-browser"] = &Session{ID: "delivery-browser", Number: 987, Title: "隔离验收 · 视频交付缺口", FollowedByAssistant: true, Created: time.Now().Format(time.RFC3339Nano), Updated: time.Now().Format(time.RFC3339Nano), Messages: []Message{}, Runs: []*Task{{ID: "delivery-run", Mode: "chat", Prompt: "生成三秒视频", Status: "paused", CanResume: true, Error: "服务商已生成，文件下载尚未完成", Delivery: &TaskDelivery{State: "missing", Kind: "video", Reason: "服务商已生成，文件下载尚未完成"}, Steps: []Step{{Name: "chat", Content: "生成记录已保存，续跑只下载原结果，不重复创建任务。"}}}}}
	a.sessions["delivered-browser"] = &Session{ID: "delivered-browser", Number: 988, Title: "隔离验收 · 已交付语音", FollowedByAssistant: true, Created: time.Now().Format(time.RFC3339Nano), Updated: time.Now().Format(time.RFC3339Nano), Messages: []Message{}, Runs: []*Task{{ID: "speech-run", Mode: "chat", Status: "completed", Delivery: &TaskDelivery{State: "delivered", Kind: "speech", Artifacts: []string{"generated/speech/fixture.wav"}}, Steps: []Step{{Name: "chat", Content: "测试语音已保存。"}}}}}
	a.reminders = append(a.reminders, Reminder{ID: "browser-schedule", Title: "隔离验收 · 定时跟进", Execution: &ReminderExecution{WorkspaceID: a.wsID(), SessionID: "delivery-browser", State: "interrupted", Error: "定时执行结果需要核对"}})
	a.mu.Unlock()
	wav := make([]byte, 44+1600)
	copy(wav, "RIFF")
	binary.LittleEndian.PutUint32(wav[4:], uint32(len(wav)-8))
	copy(wav[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(wav[16:], 16)
	binary.LittleEndian.PutUint16(wav[20:], 1)
	binary.LittleEndian.PutUint16(wav[22:], 1)
	binary.LittleEndian.PutUint32(wav[24:], 8000)
	binary.LittleEndian.PutUint32(wav[28:], 16000)
	binary.LittleEndian.PutUint16(wav[32:], 2)
	binary.LittleEndian.PutUint16(wav[34:], 16)
	copy(wav[36:], "data")
	binary.LittleEndian.PutUint32(wav[40:], 1600)
	asset := "generated/speech/qwen-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.wav"
	if err := putText(a.workspace, asset, wav); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	delivered := a.sessions["delivered-browser"].Runs[0]
	delivered.Prompt = "生成WAV，内容：隔离验收"
	delivered.Steps[0].Content = "[播放测试语音](" + asset + ")"
	delivered.Delivery = a.inspectTaskDeliveryLocked(delivered, delivered.Steps[0].Content)
	a.mu.Unlock()
	unlockAssistantSessionForTest(t, a)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: a.Handler(), ReadHeaderTimeout: 5 * time.Second}
	defer server.Close()
	go server.Serve(listener)
	t.Log("Isolated assistant workbench ready; use #token=assistant-loop-synthetic-token")
	for i := 0; i < 900; i++ {
		if _, err := os.Stat("/tmp/assistant-browser-stop"); err == nil {
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatal("browser harness timed out")
}
