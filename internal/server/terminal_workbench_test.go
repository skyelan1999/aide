package server

import (
	"archive/zip"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
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
	source := filepath.Join("..", "..", "bundled-plugins", "terminal")
	files := map[string]string{}
	if bundle := os.Getenv("AIDE_TERMINAL_BUNDLE_PATH"); bundle != "" {
		reader, err := zip.OpenReader(bundle)
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Close()
		for _, entry := range reader.File {
			f, err := entry.Open()
			if err != nil {
				t.Fatal(err)
			}
			raw, err := io.ReadAll(f)
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
			files[entry.Name] = string(raw)
		}
	} else {
		names := []string{"manifest.json", "index.js", "sessions.js", "pty-worker.py", "workbench.html", "LICENSE"}
		modules, err := filepath.Glob(filepath.Join(source, "communications", "*.js"))
		if err != nil {
			t.Fatal(err)
		}
		for _, filename := range modules {
			relative, _ := filepath.Rel(source, filename)
			names = append(names, relative)
		}
		for _, name := range names {
			raw, err := os.ReadFile(filepath.Join(source, name))
			if err != nil {
				t.Fatal(err)
			}
			files[filepath.ToSlash(name)] = string(raw)
		}
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
	listing, err := a.callPluginTool("terminal", "terminal_session_files", map[string]any{"path": "."})
	if err != nil {
		t.Fatal("Terminal daemon did not receive scoped file listing", err)
	}
	if !strings.Contains(gotText(listing), "directory-fixture.txt") {
		t.Fatal("missing scoped file metadata", listing)
	}
	history, err := a.callPluginTool("terminal", "terminal-history", map[string]any{"path": "."})
	if err != nil || !strings.Contains(gotText(history), "directory-fixture.txt") {
		t.Fatal(history, err)
	}
	// Exercise a namespaced protocol tool and shared session input through the
	// actual installed daemon, including the per-call disabled-tool snapshot.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		connection, err := listener.Accept()
		if err == nil {
			defer connection.Close()
			io.Copy(connection, connection)
		}
	}()
	port := listener.Addr().(*net.TCPAddr).Port
	connected, err := a.callPluginTool("terminal", "terminal_tcp_connect", map[string]any{"host": "127.0.0.1", "port": port})
	if err != nil {
		t.Fatal(err)
	}
	connection := connected.(map[string]any)
	sessionID := connection["sessionId"].(string)
	defer a.callPluginTool("terminal", "terminal_session_close", map[string]any{"id": sessionID})
	if _, err = a.callPluginTool("terminal", "terminal_tcp_send", map[string]any{"connectionId": connection["connectionId"], "data": "BUNDLE_TCP_OK"}); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		response, err := a.callPluginTool("terminal", "terminal_session_poll", map[string]any{"id": sessionID})
		if err != nil {
			t.Fatal(err)
		}
		output := response.(map[string]any)["output"].(map[string]any)
		body, _ := base64.StdEncoding.DecodeString(output["data"].(string))
		if strings.Contains(string(body), "RX") && strings.Count(string(body), "BUNDLE_TCP_OK") >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no actual TCP echo", fmt.Sprint(output))
		}
		time.Sleep(20 * time.Millisecond)
	}
	a.mu.Lock()
	a.settings.DisabledTools = append(a.settings.DisabledTools, "terminal_tcp_send")
	a.mu.Unlock()
	if _, err = a.callPluginTool("terminal", "terminal_session_input", map[string]any{"id": sessionID, "data": "must not send"}); err == nil {
		t.Fatal("shared session input bypassed a disabled protocol tool")
	}

	trusted := context.WithValue(context.Background(), terminalWorkbenchContextKey{}, true)
	hidden, err := a.callPluginToolContext(trusted, "terminal", "terminal_session_create", map[string]any{"kind": "local", "visibleToAide": false, "name": "private fixture"})
	if err != nil {
		t.Fatal(err)
	}
	hiddenID := hidden.(map[string]any)["session"].(map[string]any)["id"].(string)
	for _, tool := range []string{"terminal_session_poll", "terminal_session_logs", "terminal_session_close", "terminal_session_visibility", "terminal_session_relay_script"} {
		if _, err := a.callPluginTool("terminal", tool, map[string]any{"id": hiddenID, "visible": true, "workbench": true}); err == nil {
			t.Fatal("model bypassed hidden/UI gate", tool)
		}
	}
	privateList, err := a.callPluginTool("terminal", "terminal_session_list", nil)
	if err != nil || strings.Contains(gotText(privateList), hiddenID) {
		t.Fatal("hidden session exposed", err)
	}
	if _, err := a.callPluginToolContext(trusted, "terminal", "terminal_session_visibility", map[string]any{"id": hiddenID, "visible": true}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.callPluginTool("terminal", "terminal_session_poll", map[string]any{"id": hiddenID}); err != nil {
		t.Fatal("visible session inaccessible", err)
	}
	if _, err := a.callPluginToolContext(trusted, "terminal", "terminal_session_close", map[string]any{"id": hiddenID}); err != nil {
		t.Fatal(err)
	}

}
