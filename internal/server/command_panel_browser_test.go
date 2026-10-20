package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Isolated Aide page with disposable workspace/plugin data; no live user data.
func TestCommandPanelBrowser(t *testing.T) {
	if os.Getenv("AIDE_COMMAND_PANEL_BROWSER") != "1" {
		t.Skip("opt-in browser regression")
	}
	out := os.Getenv("AIDE_EVIDENCE_DIR")
	if out == "" {
		t.Fatal("set AIDE_EVIDENCE_DIR")
	}
	a := testApp(t)
	a.commandInputs = make(map[string]*io.PipeWriter)
	if err := os.MkdirAll(filepath.Join(a.dataPath, "auth"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.dataPath, "auth/access-token"), []byte(a.token), 0600); err != nil {
		t.Fatal(err)
	}
	handler := a.Handler()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Exercise real HTTP streaming + production input forwarding without a live SSH device.
		if r.URL.Path != "/api/command" || r.Method != "POST" {
			handler.ServeHTTP(w, r)
			return
		}
		a.mu.Lock()
		full := a.settings.SandboxMode == "danger-full-access"
		a.mu.Unlock()
		if !full {
			handler.ServeHTTP(w, r)
			return
		}
		reader, writer := io.Pipe()
		defer reader.Close()
		defer writer.Close()
		a.commandInputMu.Lock()
		a.commandInputs["fixture-pty"] = writer
		a.commandInputMu.Unlock()
		defer func() { a.commandInputMu.Lock(); delete(a.commandInputs, "fixture-pty"); a.commandInputMu.Unlock() }()
		done := make(chan struct{})
		defer close(done)
		go func() {
			select {
			case <-r.Context().Done():
				reader.Close()
			case <-done:
			}
		}()
		w.Header().Set("Content-Type", "application/x-ndjson")
		json.NewEncoder(w).Encode(map[string]string{"type": "session", "id": "fixture-pty"})
		w.(http.Flusher).Flush()
		buf := make([]byte, 4096)
		for {
			n, err := reader.Read(buf)
			if n > 0 {
				json.NewEncoder(w).Encode(map[string]string{"type": "output", "text": "fixture input: " + string(buf[:n])})
				w.(http.Flusher).Flush()
			}
			if err != nil {
				return
			}
		}
	}))
	defer server.Close()
	script, err := filepath.Abs("../../scripts/browser-command-panel.cjs")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("node", script)
	command.Env = append(os.Environ(), "AIDE_TEST_URL="+server.URL, "AIDE_TEST_DATA="+a.dataPath)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("command panel browser: %v\n%s", err, output)
	}
	t.Log(string(output))
}

func TestCommandPanelRestrictedSSHRejectsBeforeConnection(t *testing.T) {
	a := testApp(t)
	a.wsConfig.Workspace.Mode = "ssh"
	a.wsConfig.Workspace.Host = "fixture.invalid"
	for _, mode := range []string{"read-only", "workspace-write"} {
		a.settings.SandboxMode = mode
		for _, interactive := range []bool{false, true} {
			w := request(a, "POST", "/api/command", map[string]any{"command": "pwd", "interactive": interactive})
			requireStatus(t, w, 403)
			if !strings.Contains(w.Body.String(), "远程SSH尚未接入内核沙箱") {
				t.Fatal(w.Body.String())
			}
		}
	}
}
