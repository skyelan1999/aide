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
		const text = "USABILITY_FIXTURE: synthetic result for operator review."
		if body["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n", text)
		} else {
			jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: text}}}})
		}
	}))
	defer provider.Close()
	a.token = "usability-synthetic-token"
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
	mux := http.NewServeMux()
	mux.HandleFunc("GET /fixture/summary", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+a.token {
			http.Error(w, "unauthorized", 401)
			return
		}
		jsonOut(w, 200, map[string]any{"scope": "Synthetic workspace and exact pinned public packages; no real models/accounts or user plugins", "packages": rows})
	})
	mux.Handle("/", a.Handler())
	listener, err := net.Listen("tcp", os.Getenv("AIDE_USABILITY_ADDRESS"))
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	defer server.Close()
	go server.Serve(listener)
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
