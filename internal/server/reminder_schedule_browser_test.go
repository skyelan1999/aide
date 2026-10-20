package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestReminderScheduleBrowser(t *testing.T) {
	if os.Getenv("AIDE_REMINDER_SCHEDULE_BROWSER") != "1" {
		t.Skip("opt-in isolated workbench browser")
	}
	a := testApp(t)
	a.mu.Lock()
	a.sessions["schedule-browser"] = &Session{ID: "schedule-browser", Number: 987, Title: "定时任务测试会话", Messages: []Message{}, Runs: []*Task{}}
	a.mu.Unlock()
	if err := os.MkdirAll(filepath.Join(a.dataPath, "auth"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.dataPath, "auth/access-token"), []byte(a.token), 0600); err != nil {
		t.Fatal(err)
	}
	product := a.Handler()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/__schedule_test_tick" && r.Method == "POST" {
			a.dispatchScheduledReminders(time.Now().Add(24 * time.Hour))
			w.WriteHeader(204)
			return
		}
		product.ServeHTTP(w, r)
	}))
	defer server.Close()
	script, err := filepath.Abs("../../scripts/browser-reminder-schedules.cjs")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("node", script)
	cmd.Env = append(os.Environ(), "AIDE_TEST_URL="+server.URL, "AIDE_TEST_DATA="+a.dataPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("browser: %v\n%s", err, output)
	}
	t.Log(string(output))
}
