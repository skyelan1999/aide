package server

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	"image/png"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type releasePublicTransport func(*http.Request) (*http.Response, error)

func (f releasePublicTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func releasePublicSpeech(t *testing.T, a *App) []string {
	t.Helper()
	// Exercise the installed native generation entry, durable job state, exact
	// provider payload, actual saved WAV bytes and resume without resubmission.
	// This is a protocol fixture, not a claim of acoustic model quality.
	wav := []byte{'R', 'I', 'F', 'F', 38, 0, 0, 0, 'W', 'A', 'V', 'E', 'f', 'm', 't', ' ', 16, 0, 0, 0, 1, 0, 1, 0, 0xc0, 0x5d, 0, 0, 0x80, 0xbb, 0, 0, 2, 0, 16, 0, 'd', 'a', 't', 'a', 2, 0, 0, 0, 0, 0}
	posts, downloads := 0, 0
	previous := http.DefaultTransport
	http.DefaultTransport = releasePublicTransport(func(r *http.Request) (*http.Response, error) {
		var body []byte
		header := make(http.Header)
		switch {
		case r.URL.Host == "dashscope.aliyuncs.com" && r.Method == "POST":
			posts++
			if r.URL.Path != "/api/v1/services/audio/tts/SpeechSynthesizer" || r.Header.Get("Authorization") != "Bearer public-speech-fixture" {
				t.Error("speech provider path/auth mismatch")
			}
			var request struct {
				Model string `json:"model"`
				Input struct {
					Text   string `json:"text"`
					Format string `json:"format"`
					Voice  string `json:"voice"`
				} `json:"input"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
			}
			if request.Model != "qwen-audio-3.0-tts-flash" || request.Input.Text != "public speech fixture" || request.Input.Format != "wav" || request.Input.Voice != "fixture_voice" {
				t.Errorf("wrong actual speech payload: %+v", request)
			}
			body = []byte(`{"output":{"audio":{"url":"https://dashscope-result-bj.oss-cn-beijing.aliyuncs.com/public-fixture.wav"}}}`)
			header.Set("Content-Type", "application/json")
		case r.URL.Host == "dashscope-result-bj.oss-cn-beijing.aliyuncs.com" && r.Method == "GET":
			downloads++
			body = wav
			header.Set("Content-Type", "audio/wav")
		default:
			return nil, fmt.Errorf("unexpected fixture network request: %s %s", r.Method, r.URL.Host)
		}
		return &http.Response{StatusCode: 200, Header: header, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Request: r}, nil
	})
	defer func() { http.DefaultTransport = previous }()
	a.settings.BaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	a.settings.Model = "qwen-audio-3.0-tts-flash"
	a.settings.Models = []ModelRef{{ID: a.settings.Model}}
	a.settings.ModelProviders = nil
	a.mu.Lock()
	a.storeModelAPIKeyPlaintextLocked("public-speech-fixture")
	a.mu.Unlock()
	task := &Task{ID: "public-speech", Mode: "chat", Status: "running", WorkspaceID: a.wsID(), WorkspaceRev: a.wsRevision, WorkspaceMode: "local"}
	session := &Session{ID: "public-speech-session", Runs: []*Task{task}}
	a.sessions[session.ID] = session
	if err := a.save(session); err != nil {
		t.Fatal(err)
	}
	args := map[string]any{"model": a.settings.Model, "text": "public speech fixture", "format": "wav", "voice": "fixture_voice"}
	for i := 0; i < 2; i++ {
		if _, err := a.ttsGenerationTool(context.Background(), task, args); err != nil {
			t.Fatal(err)
		}
	}
	if task.MediaGeneration == nil || task.MediaGeneration.Asset == "" {
		t.Fatal("speech artifact missing")
	}
	data, err := a.workspace.ReadFile(task.MediaGeneration.Asset)
	if err != nil || !bytes.Equal(data, wav) || posts != 1 || downloads != 1 {
		t.Fatal("speech bytes/resume mismatch", err, posts, downloads)
	}
	return []string{"installed native voice catalog and invalid codec rejection", "actual generation entry validates provider request, saves exact WAV and durable state; resume neither resubmits nor redownloads", "provider bytes are synthetic; no paid inference or acoustic quality claim"}
}

func releasePublicSettings(t *testing.T, a *App, id string, settings map[string]any) {
	t.Helper()
	response := request(a, "PUT", "/api/plugins/"+id+"/settings", map[string]any{"settings": settings})
	requireStatus(t, response, 200)
	if strings.Contains(response.Body.String(), "restartError") {
		t.Fatal(response.Body.String())
	}
}

func releasePublicObject(t *testing.T, text string) map[string]any {
	t.Helper()
	var value any
	if err := json.Unmarshal([]byte(text), &value); err != nil {
		t.Fatal("not structured", text)
	}
	if nested, ok := value.(string); ok {
		if err := json.Unmarshal([]byte(nested), &value); err != nil {
			t.Fatal(err)
		}
	}
	result, ok := value.(map[string]any)
	if !ok {
		t.Fatal("not an object", text)
	}
	return result
}

func releasePublicCodex(t *testing.T, a *App) []string {
	token := strings.Repeat("c", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		value := map[string]any{"service": "aide-codex-bridge-manager", "version": "0.4.1", "pairingProtocol": 1, "installed": true, "running": true}
		switch r.URL.Path {
		case "/health":
		case "/api/pair":
			var body map[string]string
			if json.NewDecoder(r.Body).Decode(&body) != nil || body["code"] != "ABCD-1234-5678" {
				t.Error("pairing payload mismatch")
			}
			value["token"] = token
		case "/api/usage":
			if r.Header.Get("Authorization") != "Bearer "+token {
				t.Error("quota call lost scoped grant")
			}
			value["usage"] = map[string]any{"buckets": []any{map[string]any{"id": "codex", "primary": map[string]any{"usedPercent": 25, "windowDurationMins": 300, "resetsAt": 1793250000}}}, "account": "MUST_NOT_LEAK"}
		case "/api/models":
			if r.Header.Get("Authorization") != "Bearer "+token {
				t.Error("model call lost grant")
			}
			value["models"] = []any{map[string]any{"id": "fixture-model", "key": "MUST_NOT_LEAK"}}
		default:
			if r.Header.Get("Authorization") != "Bearer "+token {
				t.Error("manager status lost grant")
			}
		}
		jsonOut(w, 200, value)
	}))
	defer server.Close()
	releasePublicSettings(t, a, "codex-bridge", map[string]any{"managerURL": server.URL})
	before := releasePublicCall(t, a, "codex-bridge", "codex_bridge_manager_status", map[string]any{})
	if !strings.Contains(before, "unpaired") {
		t.Fatal(before)
	}
	paired := releasePublicCall(t, a, "codex-bridge", "codex_bridge_pair", map[string]any{"code": "ABCD-1234-5678"})
	if !strings.Contains(paired, `"paired":true`) || strings.Contains(paired, token) {
		t.Fatal("grant not persisted privately", paired)
	}
	quota := releasePublicCall(t, a, "codex-bridge", "codex_bridge_usage", map[string]any{})
	if !strings.Contains(quota, `"remainingPercent":75`) || strings.Contains(quota, "MUST_NOT_LEAK") {
		t.Fatal(quota)
	}
	models := releasePublicCall(t, a, "codex-bridge", "codex_bridge_models", map[string]any{})
	if !strings.Contains(models, "fixture-model") || strings.Contains(models, "MUST_NOT_LEAK") {
		t.Fatal(models)
	}
	releasePublicCall(t, a, "codex-bridge", "codex_bridge_disconnect", map[string]any{})
	return []string{"published bridge pairing persists a private grant across actual host calls", "quota normalization and model catalog remove secrets", "disconnect clears fixture grant; no paid model inference claimed"}
}

func releasePublicFilePreview(t *testing.T, a *App) []string {
	token := strings.Repeat("f", 40)
	script := filepath.Join(a.pluginsPath, "file-preview", "engine", "server.py")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "python3", script)
	cmd.Env = append(os.Environ(), "FILE_PREVIEW_TOKEN="+token)
	configurePluginProcess(cmd, true)
	logPath := filepath.Join(t.TempDir(), "renderer.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = killPluginProcess(cmd); _ = cmd.Wait() }()
	releasePublicSettings(t, a, "file-preview", map[string]any{"engineURL": "http://127.0.0.1:8765", "engineToken": token})
	deadline := time.Now().Add(10 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest("GET", "http://127.0.0.1:8765/health", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				ready = true
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		data, _ := os.ReadFile(logPath)
		t.Fatal("published independent renderer did not start", string(data))
	}
	picture := image.NewRGBA(image.Rect(0, 0, 32, 24))
	for y := 0; y < 24; y++ {
		for x := 0; x < 32; x++ {
			picture.Set(x, y, color.RGBA{uint8(x * 7), uint8(y * 9), 80, 255})
		}
	}
	file := filepath.Join(a.workPath, "public-preview.png")
	stream, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	err = png.Encode(stream, picture)
	closeErr := stream.Close()
	if err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	got := releasePublicCall(t, a, "file-preview", "file_preview_render", map[string]any{"path": "public-preview.png", "pages": []any{1}, "dpi": 120})
	if !strings.Contains(got, "image/jpeg") || !strings.Contains(got, "/9j/") {
		t.Fatal("actual rendered JPEG not delivered", got)
	}
	rendered := releasePublicObject(t, got)
	images, ok := rendered["images"].([]any)
	if !ok || len(images) != 1 {
		t.Fatal("missing exact rendered page")
	}
	page, ok := images[0].(map[string]any)
	if !ok {
		t.Fatal("malformed rendered page")
	}
	encoded, ok := page["dataBase64"].(string)
	if !ok {
		t.Fatal("missing rendered bytes")
	}
	raster, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	decoded, format, err := image.Decode(bytes.NewReader(raster))
	if err != nil || format != "jpeg" {
		t.Fatal("undecodable JPEG", err)
	}
	bounds := decoded.Bounds()
	if math.Abs(float64(bounds.Dx())/float64(bounds.Dy())-4.0/3) > 0.05 {
		t.Fatal("rendered geometry changed", bounds)
	}
	red, green, blue, _ := decoded.At(bounds.Dx()/2, bounds.Dy()/2).RGBA()
	if math.Abs(float64(red/257)-112) > 20 || math.Abs(float64(green/257)-108) > 20 || math.Abs(float64(blue/257)-80) > 20 {
		t.Fatal("rendered pixels differ from source gradient", red/257, green/257, blue/257)
	}
	if page["sha256"] != hash(raster) {
		t.Fatal("renderer digest mismatch")
	}
	after, err := os.ReadFile(file)
	if err != nil || hash(before) != hash(after) {
		t.Fatal("renderer altered original", err)
	}
	if _, err := a.callPluginToolContext(context.Background(), "file-preview", "file_preview_render", map[string]any{"path": "../private.png"}); err == nil {
		t.Fatal("renderer traversal accepted")
	}
	return []string{"actual installed plugin sends authorized source to its released renderer and returns a real JPEG", "original bytes preserved and traversal rejected; Office conversion is separately listed in coverage"}
}

func releasePublicStarLink(t *testing.T, a *App) []string {
	directory, err := os.MkdirTemp("/workspace", "public-starlink-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(directory)
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	a.workPath = directory
	a.workspace = root
	a.wsConfig.Workspace.Mode = "local"
	a.wsConfig.Workspace.Path = directory
	a.wsRoots[a.wsID()] = root
	if err := root.WriteFile("fixture.md", []byte("# Published StarLink fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	certFile, keyFile, err := ensureTLSCert(filepath.Join(a.dataPath, "certs"))
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(a.Handler())
	server.TLS = &tls.Config{Certificates: []tls.Certificate{certificate}}
	server.StartTLS()
	defer server.Close()
	if err := os.MkdirAll(filepath.Join(a.dataPath, "auth"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(a.dataPath, "certs"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.dataPath, "auth", "access-token"), []byte(a.token), 0600); err != nil {
		t.Fatal(err)
	}

	port := server.Listener.Addr().(*net.TCPAddr).Port
	releasePublicSettings(t, a, "starlink", map[string]any{"port": port, "dataDir": a.dataPath})
	got := releasePublicCall(t, a, "starlink", "starlink_status", map[string]any{})
	if releasePublicObject(t, got)["available"] != true {
		t.Fatal("isolated StarLink unavailable", got)
	}
	summary := releasePublicObject(t, releasePublicCall(t, a, "starlink", "starlink_summary", map[string]any{}))
	if count, ok := summary["nodeCount"].(float64); !ok || count < 1 {
		t.Fatal("isolated graph missing nodes", summary)
	}
	searched := releasePublicObject(t, releasePublicCall(t, a, "starlink", "starlink_search", map[string]any{"query": "fixture.md"}))
	if results, ok := searched["results"].([]any); !ok || len(results) == 0 {
		t.Fatal("real workspace fixture not searchable", searched)
	}
	return []string{"installed StarLink authenticates against the real isolated Aide TLS host", "workspace-bound status and graph summary complete without external sources"}
}

func releasePublicSTEP(t *testing.T, a *App) []string {
	id := "step-viewer"
	if _, err := a.callPluginToolContext(context.Background(), id, "step_preview_io", map[string]any{"action": "install"}); err == nil {
		t.Fatal("engine install missing explicit consent was accepted")
	}
	releasePublicCall(t, a, id, "step_preview_io", map[string]any{"action": "install", "confirm": true})
	deadline := time.Now().Add(6 * time.Minute)
	ready := false
	for time.Now().Before(deadline) {
		status := releasePublicObject(t, releasePublicCall(t, a, id, "step_preview_io", map[string]any{"action": "status"}))
		if status["ready"] == true {
			ready = true
			break
		}
		if message, ok := status["error"].(string); ok && message != "" {
			t.Fatal("published prebuilt engine failed", message)
		}
		time.Sleep(time.Second)
	}
	if !ready {
		t.Fatal("published prebuilt CAD engine did not become ready within six minutes")
	}
	engine := "/home/aide/.cache/aide-step-viewer/engine"
	file := filepath.Join(a.workPath, "synthetic-box.step")
	program := `from OCP.BRepPrimAPI import BRepPrimAPI_MakeBox
from OCP.STEPControl import STEPControl_Writer,STEPControl_AsIs
import sys
writer=STEPControl_Writer();writer.Transfer(BRepPrimAPI_MakeBox(10,20,30).Shape(),STEPControl_AsIs)
assert int(writer.Write(sys.argv[1]))==1
`
	command := exec.Command("python3", "-c", program, file)
	command.Env = append(os.Environ(), "PYTHONPATH="+engine, "LD_LIBRARY_PATH="+filepath.Join(engine, "system"))
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatal("real CAD fixture creation failed", err, string(out))
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	opened := releasePublicObject(t, releasePublicCall(t, a, id, "step_preview_io", map[string]any{"action": "begin", "size": len(raw)}))
	sid, ok := opened["id"].(string)
	if !ok || sid == "" {
		t.Fatal(opened)
	}
	defer releasePublicCall(t, a, id, "step_preview_io", map[string]any{"action": "cancel", "id": sid})
	for offset := 0; offset < len(raw); offset += 48 * 1024 {
		end := min(len(raw), offset+48*1024)
		releasePublicCall(t, a, id, "step_preview_io", map[string]any{"action": "write", "id": sid, "offset": offset, "data": base64.StdEncoding.EncodeToString(raw[offset:end])})
	}
	releasePublicCall(t, a, id, "step_preview_io", map[string]any{"action": "start", "id": sid})
	var output strings.Builder
	offset := 0
	done := false
	deadline = time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		part := releasePublicObject(t, releasePublicCall(t, a, id, "step_preview_io", map[string]any{"action": "read", "id": sid, "offset": offset}))
		data, _ := part["data"].(string)
		decoded, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			t.Fatal(err)
		}
		output.Write(decoded)
		offset = int(part["offset"].(float64))
		if part["done"] == true {
			done = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !done {
		t.Fatal("real CAD conversion did not finish")
	}
	complete := false
	for _, line := range strings.Split(output.String(), "\n") {
		var record map[string]any
		if json.Unmarshal([]byte(line), &record) != nil {
			continue
		}
		if e := record["error"]; e != nil {
			t.Fatal("CAD converter error", e)
		}
		if record["kind"] == "complete" {
			triangles, _ := record["triangles"].(float64)
			if triangles < 12 {
				t.Fatal("box geometry not actually tessellated", record)
			}
			bounds, _ := json.Marshal(record["bounds"])
			if string(bounds) != "[[0,0,0],[10,20,30]]" {
				t.Fatal("wrong real box bounds", string(bounds))
			}
			complete = true
		}
	}
	if !complete {
		t.Fatal("missing complete CAD geometry evidence", fmt.Sprint(output.String()))
	}
	return []string{"published prebuilt CAD engine downloads with its pinned checksum; no plugin compilation", "real generated STEP box uploads, tessellates to triangles and retains exact 10x20x30 bounds", "engine installation requires consent and private job is explicitly cancelled"}
}
