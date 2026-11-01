package server

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
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

type releasePublicPlugin struct {
	ID      string `json:"id"`
	Version string `json:"marketVersion"`
	Package string `json:"package"`
	SHA256  string `json:"sha256"`
	Bytes   int    `json:"bytes"`
}

// Opt-in: only published public ZIPs, synthetic workspace/data and loopback
// fixtures. Package bytes are installed unchanged through the real upload API.
func TestReleasePublicPackageFunctions(t *testing.T) {
	root := os.Getenv("AIDE_RELEASE_PUBLIC_MARKET")
	if root == "" {
		t.Skip("explicit frozen public-market packages required")
	}
	lock, err := os.ReadFile("../../scripts/ci/public-plugins.lock.json")
	if err != nil {
		t.Fatal(err)
	}
	var inventory struct {
		Revision string                `json:"marketRevision"`
		Plugins  []releasePublicPlugin `json:"plugins"`
	}
	if json.Unmarshal(lock, &inventory) != nil || len(inventory.Plugins) == 0 {
		t.Fatal("invalid public inventory")
	}
	report := map[string]any{"status": "fail", "marketRevision": inventory.Revision, "plugins": []map[string]any{}}
	var rows []map[string]any
	defer func() {
		report["plugins"] = rows
		if !t.Failed() {
			report["status"] = "pass"
		}
		out := os.Getenv("AIDE_PUBLIC_PLUGIN_EVIDENCE_DIR")
		if out == "" {
			t.Error("missing persistent evidence destination")
			return
		}
		if err := os.MkdirAll(out, 0700); err != nil {
			t.Error(err)
			return
		}
		if err := atomicJSON(filepath.Join(out, "functions.json"), report); err != nil {
			t.Error(err)
		}
	}()
	for _, p := range inventory.Plugins {
		t.Run(p.ID, func(t *testing.T) {
			row := map[string]any{"id": p.ID, "version": p.Version, "sha256": p.SHA256, "inspection": "fail", "installation": "not_run", "function": "not_run"}
			rows = append(rows, row)
			defer func() {
				if t.Failed() && row["installation"] == "pass" {
					row["function"] = "fail"
				}
			}()
			if safePath(p.Package) != nil {
				t.Fatal("unsafe package path")
			}
			raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p.Package)))
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(raw)
			if len(raw) != p.Bytes || hex.EncodeToString(sum[:]) != p.SHA256 {
				t.Fatal("published package identity mismatch")
			}
			m, _, err := parsePluginBundle(raw)
			if err != nil || m.ID != p.ID || m.Version != p.Version {
				t.Fatal("invalid exact package", err)
			}
			row["inspection"] = "pass"
			a := testApp(t)
			var body bytes.Buffer
			form := multipart.NewWriter(&body)
			part, err := form.CreateFormFile("bundle", p.ID+".zip")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = part.Write(raw); err != nil {
				t.Fatal(err)
			}
			if err = form.Close(); err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/api/plugins/bundle", &body)
			req.Header.Set("Content-Type", form.FormDataContentType())
			req.Header.Set("Authorization", "Bearer "+a.token)
			w := httptest.NewRecorder()
			a.Handler().ServeHTTP(w, req)
			requireStatus(t, w, 201)
			a.runPluginHost(context.Background())
			var found bool
			for _, installed := range a.pluginRegistry.Plugins {
				if installed.ID == p.ID && installed.Version == p.Version && installed.Enabled {
					found = true
				}
			}
			if !found {
				t.Fatal("installed package not enabled")
			}
			row["installation"] = "pass"
			cases := releasePublicFunction(t, a, p.ID)
			if len(cases) == 0 {
				t.Fatal("missing meaningful function assertions")
			}
			row["cases"] = cases
			row["function"] = "pass"
		})
	}
}

func releasePublicCall(t *testing.T, a *App, id, name string, args map[string]any) string {
	t.Helper()
	out, err := a.callPluginToolContext(context.Background(), id, name, args)
	if err != nil {
		t.Fatal(name, err)
	}
	if text, ok := out.(string); ok {
		return text
	}
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func releasePublicNative(t *testing.T, a *App, name string, args map[string]any) string {
	t.Helper()
	task := &Task{ID: "public-function", Mode: "chat", Prompt: "Perform the explicit synthetic release fixture operation", WorkspaceID: a.wsID(), WorkspaceMode: "local", WorkspaceRev: a.wsRevision, Status: "running"}
	raw, _ := json.Marshal(args)
	outcome := &toolDispatchOutcome{}
	out := a.executeToolCall(context.WithValue(context.Background(), toolDispatchOutcomeKey{}, outcome), readCall(name, string(raw)), task, nil)
	if outcome.err != nil {
		t.Fatal(name, out)
	}
	return out
}
func releasePublicFunction(t *testing.T, a *App, id string) []string {
	t.Helper()
	switch id {
	case "goal", "plan", "todo", "feedback", "subagent", "workflow":
		// These published packages are explicitly capability presets, not executable
		// task-management implementations. Verify the real installed disclosure.
		surface := string(a.pluginSurface)
		expected := map[string][]string{"goal": {"goal-create", "goal-status"}, "plan": {"plan-make", "plan-review"}, "todo": {"todo-list", "todo-update"}, "feedback": {"feedback-capture", "feedback-summary"}, "subagent": {"subagent-spawn", "subagent-report"}, "workflow": {"workflow-start", "workflow-status"}}[id]
		for _, name := range expected {
			if !strings.Contains(surface, name) {
				t.Fatal("preset capability missing", name)
			}
		}
		return []string{"published capability preset loads and exposes both declared tools; no executable handler capability claimed"}
	case "environment-guide":
		if err := a.workspace.WriteFile("release-note.txt", []byte("PRIVATE BODY"), 0600); err != nil {
			t.Fatal(err)
		}
		guide := a.environmentGuide()
		if !strings.Contains(guide, "release-note.txt") || strings.Contains(guide, "PRIVATE BODY") {
			t.Fatal("installed guide inventory or privacy failed")
		}
		return []string{"actual installed guide adds current workspace inventory without file bodies"}
	case "current-time":
		got := releasePublicCall(t, a, id, "get_week_number", map[string]any{"date": "2024-01-01"})
		week := releasePublicObject(t, got)
		if week["weekYear"] != float64(2024) || week["weekNumber"] != float64(1) || week["weekStart"] != "2024-01-01" || week["weekEnd"] != "2024-01-07" {
			t.Fatal(got)
		}
		if _, err := a.callPluginToolContext(context.Background(), id, "get_week_number", map[string]any{"date": "2024-02-31"}); err == nil {
			t.Fatal("invalid date accepted")
		}
		return []string{"ISO week calculation on real handler", "invalid calendar date rejected"}
	case "lunar-calendar":
		got := releasePublicCall(t, a, id, "get_lunar_date", map[string]any{"date": "2024-02-10"})
		if !strings.Contains(got, "正月") || !strings.Contains(got, "初一") {
			t.Fatal(got)
		}
		if _, err := a.callPluginToolContext(context.Background(), id, "get_lunar_date", map[string]any{"date": "2024-02-31"}); err == nil {
			t.Fatal("invalid date accepted")
		}
		return []string{"2024 lunar new year from installed calendar data", "invalid date rejected"}
	case "almanac":
		got := releasePublicCall(t, a, id, "almanac_query", map[string]any{"date": "2024-02-10"})
		if !strings.Contains(got, "2024-02-10") {
			t.Fatal(got)
		}
		return []string{"installed offline almanac returns the requested exact date"}
	case "deepseek-supporter", "ali-bailian-supporter", "openai-supporter":
		endpoint, model := "https://api.deepseek.com/v1", "deepseek-flash"
		if id == "ali-bailian-supporter" {
			endpoint, model = "https://dashscope.aliyuncs.com/compatible-mode/v1", "qwen-plus"
		}
		if id == "openai-supporter" {
			endpoint, model = "https://api.openai.com/v1", "gpt-6-luna"
		}
		quote := a.providerQuote(endpoint, model, 100, supporterAt(t, "2026-10-31T09:00:00+08:00"))
		if quote == nil || quote.Source != id {
			t.Fatal("installed catalog did not supply quote", quote)
		}
		if a.providerQuote("https://untrusted.invalid/v1", model, 100, supporterAt(t, "2026-10-31T09:00:00+08:00")) != nil {
			t.Fatal("foreign endpoint matched")
		}
		return []string{"real installed pricing catalog quotes a supported model", "foreign provider endpoint rejected; no live account balance claim"}
	case "auto-ai-model-router":
		got := releasePublicCall(t, a, id, "auto_model_router_catalog", map[string]any{})
		if !strings.Contains(got, "models") {
			t.Fatal("installed router catalog unavailable", got)
		}
		return []string{"actual installed router tool returns its offline model catalog"}
	case "skill":
		got := releasePublicNative(t, a, "skill_library", map[string]any{"action": "installed"})
		if !json.Valid([]byte(got)) {
			t.Fatal("installed skill listing is not structured", got)
		}
		return []string{"real installed native skill inventory callable without fetching or executing remote code"}
	case "plugin-authoring":
		got := releasePublicNative(t, a, "manage_plugins", map[string]any{"action": "guide"})
		if !strings.Contains(got, "apply") {
			t.Fatal("actual plugin authoring guide missing", got)
		}
		return []string{"installed authoring tool dispatches native protocol guide without changing registry"}
	case "office":
		useSourceOfficeScripts(t)
		got := releasePublicNative(t, a, "office_create", map[string]any{"path": "public-release.docx", "format": "docx", "content": map[string]any{"blocks": []any{map[string]any{"type": "paragraph", "text": "published plugin office verification"}}}})
		if !strings.Contains(got, "已生成") {
			t.Fatal(got)
		}
		raw, err := a.workspace.ReadFile("public-release.docx")
		if err != nil || len(raw) < 4 || string(raw[:2]) != "PK" {
			t.Fatal("real DOCX artifact not written", err)
		}
		bundle, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
		if err != nil {
			t.Fatal(err)
		}
		document, err := bundle.Open("word/document.xml")
		if err != nil {
			t.Fatal(err)
		}
		xml, err := io.ReadAll(document)
		document.Close()
		if err != nil || !strings.Contains(string(xml), "published plugin office verification") {
			t.Fatal("DOCX content did not match requested paragraph", err)
		}
		return []string{"actual installed native Office creates a real DOCX in the isolated workspace"}

	case "archive-tools":
		if err := a.workspace.WriteFile("fixture.txt", []byte("public archive round trip"), 0600); err != nil {
			t.Fatal(err)
		}
		got := releasePublicNative(t, a, "archive_compress", map[string]any{"path": "fixture.txt", "format": "zip"})
		if !strings.Contains(got, "fixture.txt.zip") {
			t.Fatal(got)
		}
		releasePublicNative(t, a, "archive_extract", map[string]any{"path": "fixture.txt.zip"})
		matches, err := filepath.Glob(filepath.Join(a.workPath, "fixture*", "fixture.txt"))
		if err != nil || len(matches) != 1 {
			t.Fatal("real extracted payload missing", matches, err)
		}
		raw, err := os.ReadFile(matches[0])
		if err != nil || string(raw) != "public archive round trip" {
			t.Fatal("round trip differs", err)
		}
		return []string{"installed archive native executor compresses and extracts exact bytes"}
	case "markdown-history":
		if err := a.workspace.WriteFile("fixture.md", []byte("# public history"), 0600); err != nil {
			t.Fatal(err)
		}
		w := request(a, "GET", "/api/file/history?path=fixture.md", nil)
		requireStatus(t, w, 200)
		if !strings.Contains(w.Body.String(), `"total":1`) {
			t.Fatal(w.Body.String())
		}
		w = request(a, "GET", "/api/file/history?path=fixture.md", nil)
		requireStatus(t, w, 200)
		if !strings.Contains(w.Body.String(), `"total":1`) {
			t.Fatal("unchanged history duplicated", w.Body.String())
		}
		return []string{"installed history observes real file and deduplicates unchanged version"}
	case "sqlite":
		dir, err := os.MkdirTemp("/workspace", "public-sqlite-")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(dir)
		db := filepath.Join(dir, "fixture.db")
		releasePublicCall(t, a, id, "sqlite_query", map[string]any{"db": db, "sql": "CREATE TABLE release_test(id INTEGER PRIMARY KEY,value TEXT)", "readonly": false})
		releasePublicCall(t, a, id, "sqlite_query", map[string]any{"db": db, "sql": "INSERT INTO release_test(value) VALUES (?)", "params": []any{"actual-published-package"}, "readonly": false})
		got := releasePublicCall(t, a, id, "sqlite_query", map[string]any{"db": db, "sql": "SELECT value FROM release_test"})
		if !strings.Contains(got, "actual-published-package") {
			t.Fatal(got)
		}
		if _, err := a.callPluginToolContext(context.Background(), id, "sqlite_query", map[string]any{"db": db, "sql": "DELETE FROM release_test"}); err == nil {
			t.Fatal("default readonly write accepted")
		}
		t.Run("unscoped-write-confirmation", func(t *testing.T) {
			for _, script := range []string{" DELETE FROM release_test", "SELECT 1; DELETE FROM release_test"} {
				if _, err := a.callPluginToolContext(context.Background(), id, "sqlite_execute", map[string]any{"db": db, "script": script}); err == nil {
					t.Error("unscoped delete accepted without confirmation", script)
				}
			}
		})
		t.Run("attach-outside-root", func(t *testing.T) {
			outside := filepath.Join(t.TempDir(), "must-not-create.db")
			sql := "ATTACH DATABASE '" + outside + "' AS ext; SELECT '" + dir + "/unrelated-safe-string'"
			if _, err := a.callPluginToolContext(context.Background(), id, "sqlite_execute", map[string]any{"db": db, "script": sql, "confirm": true}); err == nil {
				t.Error("unrelated literal authorized outside ATTACH")
			}
			if _, err := os.Stat(outside); !os.IsNotExist(err) {
				t.Error("outside database created", err)
			}
			outside = filepath.Join(t.TempDir(), "must-not-create-transaction.db")
			result, err := a.callPluginToolContext(context.Background(), id, "sqlite_transaction", map[string]any{"db": db, "statements": []any{map[string]any{"sql": "ATTACH DATABASE '" + outside + "' AS ext"}}, "confirm": true})
			if err == nil {
				// Transaction failures are returned as committed:false, not RPC errors.
				var transaction map[string]any
				if text, ok := result.(string); ok {
					_ = json.Unmarshal([]byte(text), &transaction)
				} else {
					transaction, _ = result.(map[string]any)
				}
				if transaction["committed"] != false || transaction["error"] == nil || transaction["error"] == "" {
					t.Error("transaction authorized outside ATTACH", result)
				}
			}
			if _, err := os.Stat(outside); !os.IsNotExist(err) {
				t.Error("outside transaction database created", err)
			}
		})
		t.Run("csv-no-symlink-write", func(t *testing.T) {
			outside := filepath.Join(t.TempDir(), "sentinel")
			if err := os.WriteFile(outside, []byte("unchanged-outside"), 0600); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(dir, "export-link.csv")
			if err := os.Symlink(outside, link); err != nil {
				t.Fatal(err)
			}
			if _, err := a.callPluginToolContext(context.Background(), id, "sqlite_export", map[string]any{"db": db, "format": "csv", "outputPath": link, "query": "SELECT 1 AS value"}); err == nil {
				t.Error("CSV export accepted symlink target")
			}
			if data, err := os.ReadFile(outside); err != nil || string(data) != "unchanged-outside" {
				t.Error("outside sentinel changed", err)
			}
		})
		return []string{"installed SQLite handler creates/inserts/reads exact bound values", "default read-only rejects write", "every unscoped write requires confirmation", "execute and transaction cannot ATTACH outside declared roots", "CSV export cannot overwrite outside symlink sentinel"}
	case "tts-tools":
		got := releasePublicNative(t, a, "tts_voices", map[string]any{})
		if len(got) < 20 {
			t.Fatal("actual voice catalog empty", got)
		}
		if _, err := a.ttsGenerationTool(context.Background(), &Task{Mode: "chat"}, map[string]any{"text": "synthetic public release", "format": "exe"}); err == nil {
			t.Fatal("invalid audio codec accepted")
		}
		return releasePublicSpeech(t, a)
	case "terminal":
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		proxyTest := exec.CommandContext(ctx, "node", "--test", "../../scripts/ci/terminal-proxy-backpressure.test.cjs")
		proxyTest.Env = append(os.Environ(), "AIDE_TERMINAL_RUNTIME="+filepath.Join(a.pluginsPath, id))
		if output, err := proxyTest.CombinedOutput(); err != nil {
			t.Fatalf("installed proxy congestion regression: %v\n%s", err, output)
		} else {
			t.Log(string(output))
		}
		got := releasePublicCall(t, a, id, "terminal_session_create", map[string]any{"kind": "local", "cols": 80, "rows": 24})
		var session map[string]any
		if json.Unmarshal([]byte(got), &session) != nil {
			t.Fatal(got)
		}
		sid, _ := session["id"].(string)
		if sid == "" {
			if v, ok := session["session"].(map[string]any); ok {
				sid, _ = v["id"].(string)
			}
		}
		if sid == "" {
			t.Fatal("created terminal has no real ID", got)
		}
		defer releasePublicCall(t, a, id, "terminal_session_close", map[string]any{"id": sid})
		releasePublicCall(t, a, id, "terminal_session_input", map[string]any{"id": sid, "data": "printf 'published-terminal-ok\\n'\n", "format": "utf8"})
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			got = releasePublicCall(t, a, id, "terminal_session_poll", map[string]any{"id": sid, "offset": 0})
			if strings.Contains(got, "published-terminal-ok") {
				return []string{"published terminal creates a real local process, sends input, polls output and closes", "installed proxy pauses congested sources in both directions and transfers exact 32 MiB payloads"}
			}
			var output map[string]any
			_ = json.Unmarshal([]byte(got), &output)
			if nested, ok := output["output"].(map[string]any); ok {
				output = nested
			}
			for _, key := range []string{"data", "output", "chunk"} {
				if encoded, ok := output[key].(string); ok {
					decoded, _ := base64.StdEncoding.DecodeString(encoded)
					if strings.Contains(string(decoded), "published-terminal-ok") {
						return []string{"published terminal process I/O and explicit cleanup", "installed proxy pauses congested sources in both directions and transfers exact 32 MiB payloads"}
					}
				}
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatal("terminal did not return actual command output", got)
	case "video-preview":
		payload := []byte(`<mlt><playlist id="v"/></mlt>`)
		got := releasePublicCall(t, a, id, "video_preview_io", map[string]any{"action": "begin", "size": len(payload), "name": "fixture.mlt"})
		var opened map[string]any
		if json.Unmarshal([]byte(got), &opened) != nil {
			t.Fatal(got)
		}
		sid, _ := opened["id"].(string)
		if sid == "" {
			t.Fatal(got)
		}
		releasePublicCall(t, a, id, "video_preview_io", map[string]any{"action": "write", "id": sid, "offset": 0, "data": base64.StdEncoding.EncodeToString(payload)})
		got = releasePublicCall(t, a, id, "video_preview_io", map[string]any{"action": "inspect", "id": sid})
		if !strings.Contains(got, "MLT") {
			t.Fatal(got)
		}
		return []string{"installed video package performs opaque upload and real MLT parsing"}
	case "rviz":
		got := releasePublicCall(t, a, id, "rviz_save_config", map[string]any{"config": map[string]any{"version": 1, "fixed": "map", "password": "MUST_NOT_PERSIST", "layers": []any{}}})
		if !strings.Contains(got, "session.json") || strings.Contains(got, "MUST_NOT_PERSIST") {
			t.Fatal("display-only proposal contract failed", got)
		}
		return []string{"installed RViz saves display preferences as a real pending proposal and excludes credentials"}
	case "file-preview":
		return releasePublicFilePreview(t, a)
	case "step-viewer":
		return releasePublicSTEP(t, a)
	case "starlink":
		return releasePublicStarLink(t, a)
	case "codex-bridge":
		return releasePublicCodex(t, a)

	case "browser-control", "computer-control":
		port, tokenEnv, tool := 17779, "AIDE_BROWSER_BRIDGE_TOKEN", "browser_status"
		if id == "computer-control" {
			port, tokenEnv, tool = 17778, "AIDE_COMPUTER_BRIDGE_TOKEN", "computer_status"
		}
		token := strings.Repeat("r", 40)
		t.Setenv(tokenEnv, token)
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			t.Fatal(err)
		}
		server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+token {
				t.Error("bridge token missing")
			}
			jsonOut(w, 200, map[string]any{"ok": true, "fixture": "published-public-plugin", "platform": "synthetic-loopback"})
		}))
		server.Listener = listener
		server.Start()
		defer server.Close()
		for i := range a.pluginRegistry.Plugins {
			if a.pluginRegistry.Plugins[i].ID == id {
				a.pluginRegistry.Plugins[i].Settings = map[string]any{"bridgeUrl": fmt.Sprintf("http://127.0.0.1:%d", port)}
			}
		}
		got := releasePublicCall(t, a, id, tool, map[string]any{})
		if !strings.Contains(got, "published-public-plugin") {
			t.Fatal(got)
		}
		return []string{"installed bridge tool makes an authenticated loopback request and returns actual bridge response; physical UI permissions not claimed"}
	default:
		t.Fatalf("public plugin %s has no implemented package function acceptance case", id)
	}
	return nil
}
