package server

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Runs real product HTTP/DOM against a deterministic, unpaid provider. The
// optional exposed listener is only for an owned disposable browser fixture.
func runProjectRecordsBrowser(t *testing.T) {
	t.Helper()
	a := testApp(t)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad fixture JSON", 400)
			return
		}
		const text = "PROJECT_GOAL_FIXTURE: execution finished; operator acceptance remains required."
		if body["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n", text)
		} else {
			jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: text}}}})
		}
	}))
	defer provider.Close()
	a.mu.Lock()
	a.token = "project-records-synthetic-token"
	a.settings = Settings{BaseURL: provider.URL, Model: "project-records-fixture", APIKey: "synthetic-test-key", SandboxMode: "workspace-write"}
	a.storeModelAPIKeyPlaintextLocked("synthetic-test-key")
	for i := 0; i < 8; i++ {
		s := planSession(a, fmt.Sprintf("project-records-%d", i), fmt.Sprintf("Project records fixture %d", i))
		s.Number = 900 + i
		if err := a.save(s); err != nil {
			t.Fatal(err)
		}
	}
	a.mu.Unlock()
	address := os.Getenv("AIDE_PROJECT_BROWSER_ADDRESS")
	if address != "" {
		listener, err := net.Listen("tcp", address)
		if err != nil {
			t.Fatal(err)
		}
		server := &http.Server{Handler: a.Handler(), ReadHeaderTimeout: 5 * time.Second}
		defer server.Close()
		go server.Serve(listener)
		t.Log("Owned project records fixture ready")
		stop := os.Getenv("AIDE_PROJECT_BROWSER_STOP")
		for i := 0; i < 1800; i++ {
			if _, err := os.Stat(stop); err == nil {
				return
			}
			time.Sleep(time.Second)
		}
		t.Fatal("owned project records fixture timed out")
		return
	}
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	script, err := filepath.Abs("../../scripts/browser-project-records.cjs")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("node", script)
	cmd.Env = append(os.Environ(), "AIDE_TEST_URL="+server.URL, "AIDE_TEST_TOKEN="+a.token)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("project records browser: %v\n%s", err, output)
	}
	t.Log(string(output))
}
func TestProjectRecordsBrowser(t *testing.T) {
	if os.Getenv("AIDE_PROJECT_BROWSER") != "1" && os.Getenv("AIDE_PROJECT_BROWSER_ADDRESS") == "" {
		t.Skip("opt-in browser acceptance; Delivery invokes the same harness")
	}
	runProjectRecordsBrowser(t)
}
