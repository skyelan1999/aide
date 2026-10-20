package server

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestTerminalWorkbenchBundleInstallsStartsAndRetainsLegacyTools(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("native Windows PTY is not implemented; use the Linux container")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("Python PTY runtime missing")
	}
	source := filepath.Join("..", "..", "plugins", "terminal")
	files := map[string]string{}
	for _, name := range []string{"manifest.json", "index.js", "sessions.js", "pty-worker.py", "workbench.html", "LICENSE"} {
		raw, err := os.ReadFile(filepath.Join(source, name))
		if err != nil {
			t.Fatal(err)
		}
		files[name] = string(raw)
	}
	a := testApp(t)
	requireStatus(t, pluginBundleRequest(t, a, files), 201)
	listed := request(a, "GET", "/api/plugins", nil)
	requireStatus(t, listed, 200)
	if !strings.Contains(listed.Body.String(), `"daemon":true`) {
		t.Fatal("parent cannot apply detach behavior", listed.Body.String())
	}
	// This uses the daemon started by installation, without a manual start or restart.
	raw, err := a.callPluginTool("terminal", "terminal_session_create", map[string]any{"name": "bundle device"})
	if err != nil {
		t.Fatal(err)
	}
	id := raw.(map[string]any)["session"].(map[string]any)["id"].(string)
	defer a.callPluginTool("terminal", "terminal_session_close", map[string]any{"id": id})
	if _, err = a.callPluginTool("terminal", "terminal_session_input", map[string]any{"id": id, "data": "printf '\\nBUNDLE_DEVICE_OK\\n'\r"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		raw, err = a.callPluginTool("terminal", "terminal_session_poll", map[string]any{"id": id})
		if err != nil {
			t.Fatal(err)
		}
		output := raw.(map[string]any)["output"].(map[string]any)
		data, err := base64.StdEncoding.DecodeString(output["data"].(string))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "\r\nBUNDLE_DEVICE_OK\r\n") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no actual command output: %q", data)
		}
		time.Sleep(20 * time.Millisecond)
	}
	proposal, err := a.callPluginTool("terminal", "terminal-run", map[string]any{"command": "printf legacy-proposal"})
	if err != nil {
		t.Fatal(err)
	}
	_, proposals := normalizePluginResult(proposal)
	if len(proposals) != 1 || proposals[0]["command"] != "printf legacy-proposal" {
		t.Fatal("legacy tool stopped proposing", proposal)
	}
	if err = os.WriteFile(filepath.Join(a.workPath, "directory-fixture.txt"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	history, err := a.callPluginTool("terminal", "terminal-history", map[string]any{"path": "."})
	if err != nil || !strings.Contains(gotText(history), "directory-fixture.txt") {
		t.Fatal(history, err)
	}
}
