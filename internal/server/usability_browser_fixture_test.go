package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Only an explicitly enabled disposable audit host. Never loads installed user
// plugins or real account settings. Package hashes are supplied by the pinned
// inventory, and installation uses the ordinary bundle endpoint unchanged.
func TestUsabilityBrowserFixture(t *testing.T) {
	if os.Getenv("AIDE_USABILITY_FIXTURE") != "1" {
		t.Skip("opt-in isolated usability host")
	}
	a := testApp(t)
	// Keep installed plugin assets outside the synthetic project index, as a
	// real installation does; the project consists only of the fixture files.
	a.pluginsPath = filepath.Join(t.TempDir(), "plugins")
	if err := os.MkdirAll(a.pluginsPath, 0700); err != nil {
		t.Fatal(err)
	}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			http.Error(w, "bad fixture request", 400)
			return
		}
		// Explicit synthetic prompts exercise actual host dispatch and approval
		// without using an external provider or real account credentials.
		if messages, ok := body["messages"].([]any); ok {
			for i := len(messages) - 1; i >= 0; i-- {
				message, _ := messages[i].(map[string]any)
				if message["role"] == "tool" {
					break
				}
				content, _ := message["content"].(string)
				const marker = "COMMON_WORKFLOW_TOOL:"
				if message["role"] != "user" || !strings.HasPrefix(content, marker) {
					continue
				}
				var call struct {
					Name      string          `json:"name"`
					Arguments json.RawMessage `json:"arguments"`
				}
				input := strings.SplitN(strings.TrimPrefix(content, marker), "\n", 2)[0]
				if json.Unmarshal([]byte(input), &call) != nil || call.Name == "" {
					http.Error(w, "invalid synthetic tool request", 400)
					return
				}
				tool := map[string]any{"index": 0, "id": "common-workflow-call", "type": "function", "function": map[string]any{"name": call.Name, "arguments": string(call.Arguments)}}
				if body["stream"] == true {
					w.Header().Set("Content-Type", "text/event-stream")
					delta, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{tool}}}}})
					fmt.Fprintf(w, "data: %s\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n", delta)
				} else {
					jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "tool_calls": []any{tool}}, "finish_reason": "tool_calls"}}})
				}
				return
			}
		}
		const text = "USABILITY_FIXTURE: synthetic result for operator review."
		if body["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n", text)
		} else {
			jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: text}}}})
		}
	}))
	defer provider.Close()
	a.token = env("AIDE_USABILITY_TOKEN", "usability-synthetic-token")
	if err := os.WriteFile(AccessTokenPath(a.dataPath), []byte(a.token), 0600); err != nil {
		t.Fatal(err)
	}
	a.settings = Settings{BaseURL: provider.URL, Model: "usability-fixture", APIKey: "synthetic-test-key", SandboxMode: "workspace-write"}
	a.storeModelAPIKeyPlaintextLocked("synthetic-test-key")
	root := os.Getenv("AIDE_USABILITY_PACKAGES")
	raw, err := os.ReadFile(filepath.Join(root, "package-inputs.json"))
	if err != nil {
		t.Fatal(err)
	}
	var inputs []struct {
		ID      string `json:"id"`
		Version string `json:"version"`
		SHA256  string `json:"sha256"`
		File    string `json:"file"`
	}
	if json.Unmarshal(raw, &inputs) != nil || len(inputs) == 0 {
		t.Fatal("invalid audit inventory")
	}
	rows := []map[string]any{}
	for _, input := range inputs {
		data, err := os.ReadFile(filepath.Join(root, input.File))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != input.SHA256 {
			t.Fatal("package differs", input.ID)
		}
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		part, err := form.CreateFormFile("bundle", input.ID+".zip")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write(data)
		_ = form.Close()
		req := httptest.NewRequest("POST", "/api/plugins/bundle", &body)
		req.Header.Set("Content-Type", form.FormDataContentType())
		req.Header.Set("Authorization", "Bearer "+a.token)
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, req)
		rows = append(rows, map[string]any{"id": input.ID, "version": input.Version, "sha256": input.SHA256, "installationStatus": w.Code, "installationResponse": json.RawMessage(w.Body.Bytes())})
	}
	if os.Getenv("AIDE_COMMON_WORKFLOW_FIXTURE") == "1" {
		// Match the production registry location used by native history policy,
		// while keeping installed package assets outside the indexed project.
		registry := filepath.Join(a.workPath, pluginsDirName, "registry.json")
		if err := os.MkdirAll(filepath.Dir(registry), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(registry); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(a.pluginsPath, "registry.json"), registry); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(a.workPath, "qa-documents"), 0700); err != nil {
			t.Fatal(err)
		}
		a.wsConfig.Docs.Path = "/workspace/qa-documents"
		if err := a.upsertSystemDocs(); err != nil {
			t.Fatal(err)
		}
	}
	a.runPluginHost(context.Background())
	a.mu.Lock()
	for i := 0; i < 4; i++ {
		s := planSession(a, fmt.Sprintf("usability-%d", i), fmt.Sprintf("Usability fixture %d", i))
		s.Number = 991 + i
		s.Runs = []*Task{}
		s.Messages = []Message{}
		if err := a.save(s); err != nil {
			t.Fatal(err)
		}
	}
	s := planSession(a, "usability-session", "可用性检查 · 合成项目")
	s.Number = 990
	s.Runs = []*Task{}
	s.Messages = []Message{}
	if err := a.save(s); err != nil {
		t.Fatal(err)
	}
	a.mu.Unlock()
	if err := os.WriteFile(filepath.Join(a.workspace.Name(), "spec.md"), []byte("# Synthetic requirements\n[Calculation](calc.md)\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.workspace.Name(), "calc.md"), []byte("# Synthetic calculation\nPressure: 10 MPa\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("AIDE_COMMON_WORKFLOW_FIXTURE") == "1" {
		for name, content := range map[string]string{
			"qa-map.yaml": "image: qa-map.pgm\nresolution: 0.05\norigin: [0.0, 0.0, 0.0]\nnegate: 0\noccupied_thresh: 0.65\nfree_thresh: 0.196\n",
			"qa-map.pgm": "P2\n4 4\n255\n255 255 255 255\n255 0 0 255\n255 0 0 255\n255 255 255 255\n",
			"qa-left.txt": "Synthetic comparison\nValue: 10\n",
			"qa-right.txt": "Synthetic comparison\nValue: 20\n",
		} {
			if err := os.WriteFile(filepath.Join(a.workPath, name), []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /fixture/summary", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+a.token {
			http.Error(w, "unauthorized", 401)
			return
		}
		jsonOut(w, 200, map[string]any{"scope": "Synthetic workspace and pinned packages; no real models/accounts or user data", "packages": rows, "dataPath": a.dataPath, "workspacePath": a.workspace.Name()})
	})
	mux.Handle("/", a.Handler())
	listener, err := net.Listen("tcp", os.Getenv("AIDE_USABILITY_ADDRESS"))
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	defer server.Close()
	go server.Serve(listener)
	if address := os.Getenv("AIDE_USABILITY_TLS_ADDRESS"); address != "" {
		cert, key, err := ensureTLSCert(CertsDir(a.dataPath))
		if err != nil {
			t.Fatal(err)
		}
		tlsListener, err := net.Listen("tcp", address)
		if err != nil {
			t.Fatal(err)
		}
		tlsServer := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
		defer tlsServer.Close()
		go tlsServer.ServeTLS(tlsListener, cert, key)
	}
	t.Log("Owned usability host ready")
	stop := os.Getenv("AIDE_USABILITY_STOP")
	for n := 0; n < 7200; n++ {
		if _, err := os.Stat(stop); err == nil {
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatal("owned usability fixture timed out")
}
