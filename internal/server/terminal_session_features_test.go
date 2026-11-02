package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"mime/multipart"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// This opt-in check installs the actual candidate ZIP into a real isolated App
// and daemon. The plugin's protocol suite separately covers SSH/X11 and relay.
func TestTerminalSessionFeaturesInstalledCandidate(t *testing.T) {
	bundle := os.Getenv("AIDE_TERMINAL_FEATURE_BUNDLE_PATH")
	if bundle == "" {
		t.Skip("set AIDE_TERMINAL_FEATURE_BUNDLE_PATH to a paired Terminal feature candidate ZIP")
	}
	archive, err := os.Open(bundle)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("bundle", filepath.Base(bundle))
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(part, hash), archive)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	if expected := os.Getenv("AIDE_TERMINAL_FEATURE_BUNDLE_SHA256"); expected != "" && digest != expected {
		t.Fatalf("original bundle SHA256 mismatch: got %s, want %s", digest, expected)
	}
	t.Logf("original multipart bundle: bytes=%d sha256=%s (no ZIP reconstruction)", size, digest)
	a := testApp(t)
	request := httptest.NewRequest("POST", "/api/plugins/bundle", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Authorization", "Bearer "+a.token)
	response := httptest.NewRecorder()
	a.Handler().ServeHTTP(response, request)
	requireStatus(t, response, 201)
	ui := context.WithValue(context.Background(), terminalWorkbenchContextKey{}, true)
	call := func(tool string, args map[string]any) map[string]any {
		t.Helper()
		raw, err := a.callPluginToolContext(ui, "terminal", "terminal_session_"+tool, args)
		if err != nil {
			t.Fatal(tool, err)
		}
		return raw.(map[string]any)
	}
	create := func(args map[string]any) map[string]any {
		t.Helper()
		record := call("create", args)["session"].(map[string]any)
		id := record["id"].(string)
		t.Cleanup(func() { a.callPluginToolContext(ui, "terminal", "terminal_session_close", map[string]any{"id": id}) })
		return record
	}
	read := func(id string) string {
		output := call("poll", map[string]any{"id": id})["output"].(map[string]any)
		raw, err := base64.StdEncoding.DecodeString(output["data"].(string))
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	until := func(id, marker string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if strings.Contains(read(id), marker) {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("session %s did not produce %s", id, marker)
	}
	source := create(map[string]any{"kind": "local", "name": "source"})["id"].(string)
	copy := create(map[string]any{"copyFrom": source, "connect": true, "name": "copy"})
	id := copy["id"].(string)
	if id == source || copy["copiedFrom"] != source {
		t.Fatal("copy did not retain independent identity", copy)
	}
	call("input", map[string]any{"id": source, "data": "printf '\\nSOURCE_ISOLATED\\n'\r"})
	call("input", map[string]any{"id": id, "data": "printf '\\nCOPY_ISOLATED\\n'\r"})
	until(source, "\r\nSOURCE_ISOLATED\r\n")
	until(id, "\r\nCOPY_ISOLATED\r\n")
	if strings.Contains(read(source), "COPY_ISOLATED") || strings.Contains(read(id), "SOURCE_ISOLATED") {
		t.Fatal("copy output mixed with source")
	}
	call("close", map[string]any{"id": source})
	call("input", map[string]any{"id": id, "data": "printf '\\nCOPY_STILL_ALIVE\\n'\r"})
	until(id, "\r\nCOPY_STILL_ALIVE\r\n")
	hidden := create(map[string]any{"kind": "local", "name": "hidden", "visibleToAide": false})["id"].(string)
	if _, err := a.callPluginTool("terminal", "terminal_session_create", map[string]any{"copyFrom": hidden, "workbench": true}); err == nil {
		t.Fatal("model spoofed hidden copy access")
	}
	saved := create(map[string]any{"copyFrom": hidden, "connect": false})
	if saved["visibleToAide"] != false || saved["status"] != "saved" {
		t.Fatal("copy settings changed hidden visibility or connected", saved)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var peers sync.Map
	var received atomic.Int64
	t.Cleanup(func() { listener.Close(); peers.Range(func(key, _ any) bool { key.(net.Conn).Close(); return true }) })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			peers.Store(conn, true)
			go func(conn net.Conn) {
				defer conn.Close()
				defer peers.Delete(conn)
				conn.Write([]byte("HOST_TELNET_READY\r\n"))
				pending := []byte{}
				buf := make([]byte, 32768)
				for {
					n, err := conn.Read(buf)
					if err != nil {
						return
					}
					received.Add(int64(n))
					pending = append(pending, buf[:n]...)
					if bytes.Contains(pending, []byte("HOST_TELNET_PING")) {
						conn.Write([]byte("HOST_TELNET_OK\r\n"))
						pending = nil
					}
					if len(pending) > 65536 {
						return
					}
				}
			}(conn)
		}
	}()
	telnet := create(map[string]any{"kind": "telnet", "host": "127.0.0.1", "port": listener.Addr().(*net.TCPAddr).Port})["id"].(string)
	until(telnet, "HOST_TELNET_READY")
	call("input", map[string]any{"id": telnet, "data": "HOST_TELNET_PING\r"})
	until(telnet, "HOST_TELNET_OK")
	a.mu.Lock()
	a.settings.DisabledTools = append(a.settings.DisabledTools, "terminal_tcp_send")
	a.mu.Unlock()
	before := received.Load()
	if _, err := a.callPluginTool("terminal", "terminal_session_input", map[string]any{"id": telnet, "data": "FORBIDDEN"}); err == nil {
		t.Fatal("Telnet bypassed disabled TCP sending")
	}
	time.Sleep(30 * time.Millisecond)
	if received.Load() != before || strings.Contains(read(telnet), "FORBIDDEN") {
		t.Fatal("disabled Telnet input reached peer or output")
	}
}
