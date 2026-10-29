package server

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/gorilla/websocket"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMixSoulBuiltInEncryptedLifecycle(t *testing.T) {
	t.Setenv("AIDE_ASSISTANT_ENGINE", "mixsoul")
	a := testApp(t)
	unlockAssistantSessionForTest(t, a)
	var calls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		text := "synthetic-local-reply"
		if n%2 == 0 {
			text = `{"items":[]}`
		}
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": text}}}})
	}))
	defer model.Close()
	cfg := Settings{BaseURL: model.URL, Model: "fixture"}
	got, err := a.runAssistantAgenticLoop(context.Background(), cfg, "记住：synthetic-local-memory", "", "text")
	if err != nil || got.Reply != "synthetic-local-reply" || calls.Load() != 2 {
		t.Fatalf("reply=%q err=%v calls=%d", got.Reply, err, calls.Load())
	}
	b, err := a.selectedMixSoul()
	if err != nil || b == nil {
		t.Fatal(err)
	}
	out, err := a.mixSoulCall(context.Background(), *b, map[string]any{"phase": "native.begin", "now": time.Now().UTC().Format(time.RFC3339Nano)})
	if err != nil || !strings.Contains(out.Instructions, "synthetic-local-memory") {
		t.Fatal("native context did not share memory", err)
	}
	_, err = a.mixSoulCall(context.Background(), *b, map[string]any{"phase": "pulse", "now": time.Now().UTC().Format(time.RFC3339Nano)})
	if err != nil || calls.Load() != 2 {
		t.Fatal("pulse model calls", err)
	}
	files := 0
	err = filepath.WalkDir(filepath.Join(a.dataPath, "assistant", "mixsoul"), func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() {
			files++
			raw, e := os.ReadFile(p)
			if e != nil {
				return e
			}
			if strings.Contains(string(raw), "synthetic-local-memory") {
				t.Error("plaintext persisted")
			}
		}
		return nil
	})
	if err != nil || files < 2 {
		t.Fatal("encrypted state absent", err)
	}
	a.mu.Lock()
	a.wsRevision++
	a.mu.Unlock()
	if a.mixSoulCurrent(*b) {
		t.Fatal("stale workspace accepted")
	}
	b, err = a.selectedMixSoul()
	if err != nil {
		t.Fatal(err)
	}
	a.clearAssistantUnlock()
	if _, err = a.mixSoulCall(context.Background(), *b, map[string]any{"phase": "pulse"}); err == nil {
		t.Fatal("locked memory access accepted")
	}
}

func TestMixSoulPluginCannotSelectEngine(t *testing.T) {
	t.Setenv("AIDE_ASSISTANT_ENGINE", "builtin")
	a := testApp(t)
	unlockAssistantSessionForTest(t, a)
	var manifest PluginManifest
	if err := json.Unmarshal([]byte(`{"id":"synthetic-agent","name":"Synthetic","main":"index.js","enabled":true,"assistantAgent":{"protocol":1}}`), &manifest); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(a.pluginsPath, manifest.ID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.js"), []byte(`module.exports={apply(){},assistantAgent(){return {action:'finish',reply:'must-not-run'}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.pluginRegistry.Plugins = append(a.pluginRegistry.Plugins, manifest)
	a.mu.Unlock()
	if b, err := a.selectedMixSoul(); err != nil || b != nil {
		t.Fatal("plugin selected private engine", err)
	}
	var calls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"builtin-only"}}]}`))
	}))
	defer model.Close()
	got, err := a.runAssistantAgenticLoop(context.Background(), Settings{BaseURL: model.URL, Model: "fixture"}, "synthetic", "", "text")
	if err != nil || got.Reply != "builtin-only" || calls.Load() != 1 {
		t.Fatal("builtin changed by plugin", err)
	}
}

// No assistant exchanges are written to chat history here. Recall must come
// from the encrypted store after a completely new App and worker process.
func TestMixSoulRecallAfterRestartWithoutConversationHistory(t *testing.T) {
	t.Setenv("AIDE_ASSISTANT_ENGINE", "mixsoul")
	root := t.TempDir()
	work, ref, data := filepath.Join(root, "work"), filepath.Join(root, "ref"), filepath.Join(root, "data")
	a, err := New(work, ref, data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if a != nil {
			a.Close()
		}
	})
	unlockAssistantSessionForTest(t, a)
	const marker = "amber-bridge-8d572af3"
	var recalled atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Messages []Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
			return
		}
		reply := "已记住"
		summary := false
		system := ""
		query := ""
		for _, m := range input.Messages {
			if m.Role == "system" {
				system += m.Content
				if strings.Contains(m.Content, "从用户原话提取") {
					summary = true
				}
			}
			if m.Role == "user" {
				query = m.Content
			}
			if m.Role == "assistant" {
				t.Error("chat history must be absent")
			}
		}
		if summary {
			reply = `{"items":[]}`
		} else if strings.Contains(query, "识别词是什么") {
			if strings.Contains(query, marker) {
				t.Error("test answer leaked into question")
			}
			reply = "未知"
			if strings.Contains(system, marker) {
				reply = marker
				recalled.Add(1)
			}
		}
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": reply}}}})
	}))
	defer model.Close()
	cfg := Settings{BaseURL: model.URL, Model: "memory-fixture"}
	if _, err = a.runAssistantAgenticLoop(context.Background(), cfg, "记住：合成重启验收项目的识别词是"+marker, "", "text"); err != nil {
		t.Fatal(err)
	}
	if len(a.recentAssistantConversation()) != 0 {
		t.Fatal("remember phase unexpectedly recorded chat")
	}
	a.Close()
	a, err = New(work, ref, data)
	if err != nil {
		t.Fatal(err)
	}
	unlockAssistantSessionForTest(t, a)
	if len(a.recentAssistantConversation()) != 0 {
		t.Fatal("restart loaded chat history")
	}
	query := "不要保存本轮内容。合成重启验收项目的识别词是什么？"
	got, err := a.runAssistantAgenticLoop(context.Background(), cfg, query, "", "text")
	if err != nil || got.Reply != marker || recalled.Load() != 1 {
		t.Fatalf("history-free recall failed reply=%q err=%v", got.Reply, err)
	}
	if _, err = a.runAssistantAgenticLoop(context.Background(), cfg, "忘记 合成重启验收项目", "", "text"); err != nil {
		t.Fatal(err)
	}
	a.Close()
	a, err = New(work, ref, data)
	if err != nil {
		t.Fatal(err)
	}
	unlockAssistantSessionForTest(t, a)
	got, err = a.runAssistantAgenticLoop(context.Background(), cfg, query, "", "text")
	if err != nil || got.Reply != "未知" || recalled.Load() != 1 {
		t.Fatalf("forgotten fact reappeared after restart reply=%q err=%v", got.Reply, err)
	}
}

func TestMixSoulNativeTimeRefreshDoesNotGenerateResponses(t *testing.T) {
	t.Setenv("AIDE_ASSISTANT_ENGINE", "mixsoul")
	t.Setenv("AIDE_ASSISTANT_TIMEZONE", "Asia/Shanghai")
	a := testApp(t)
	unlockAssistantSessionForTest(t, a)
	id := assistantSessionID(t, a)
	a.mu.Lock()
	a.settings.BaseURL = "https://space.cn-beijing.maas.aliyuncs.com/compatible-mode/v1"
	a.settings.Model = "fixture"
	a.settings.Models = nil
	a.settings.ModelProviders = nil
	a.mu.Unlock()
	if e := a.vault.Put(VaultIDModelAPIKey, VaultTypeModelAPIKey, "fixture", []byte("synthetic-native-key"), ""); e != nil {
		t.Fatal(e)
	}
	b, e := a.selectedMixSoul()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = a.mixSoulCall(context.Background(), *b, map[string]any{"phase": "begin", "heard": "不要保存本轮内容", "turnId": "before-call", "now": time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339Nano)}); e != nil {
		t.Fatal(e)
	}
	updates := make(chan string, 8)
	var generated atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		ws.WriteJSON(map[string]any{"type": "session.created"})
		first := true
		for {
			var in map[string]any
			if ws.ReadJSON(&in) != nil {
				return
			}
			if in["type"] == "response.create" {
				generated.Add(1)
			}
			if in["type"] == "session.update" {
				session, _ := in["session"].(map[string]any)
				instructions, _ := session["instructions"].(string)
				updates <- instructions
				ws.WriteJSON(map[string]any{"type": "session.updated"})
				if first {
					first = false
					ws.WriteJSON(map[string]any{"type": "conversation.item.input_audio_transcription.completed", "transcript": "不要保存本轮内容", "item_id": "synthetic-turn"})
				}
			}
		}
	}))
	defer provider.Close()
	dial := func(ctx context.Context, _ string, h http.Header) (*websocket.Conn, *http.Response, error) {
		return websocket.DefaultDialer.DialContext(ctx, "ws"+strings.TrimPrefix(provider.URL, "http"), h)
	}
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.SetPathValue("id", id)
		a.assistantRealtimeSocketWithDial(w, r, dial)
	}))
	defer local.Close()
	client, _, e := (&websocket.Dialer{Subprotocols: []string{"aide-realtime"}}).Dial("ws"+strings.TrimPrefix(local.URL, "http"), http.Header{"Origin": {local.URL}})
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if client.WriteJSON(map[string]any{"type": "aide.ping"}) != nil {
					return
				}
			}
		}
	}()
	go func() {
		for {
			var event map[string]any
			if client.ReadJSON(&event) != nil {
				return
			}
		}
	}()
	for i := 0; i < 3; i++ {
		select {
		case text := <-updates:
			if !strings.Contains(text, "Asia/Shanghai") || !strings.Contains(text, "idleSeconds") || !strings.Contains(text, "elapsedReliable") {
				t.Errorf("time context missing on update %d", i)
			}
		case <-time.After(70 * time.Second):
			t.Fatal("native periodic time update absent")
		}
	}
	if generated.Load() != 0 {
		t.Fatal("time refresh generated paid responses")
	}
}

func TestMixSoulNativeLearningPersistsBoundedImportance(t *testing.T) {
	t.Setenv("AIDE_ASSISTANT_ENGINE", "mixsoul")
	a := testApp(t)
	unlockAssistantSessionForTest(t, a)
	var calls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": `{"items":[{"kind":"preference","text":"synthetic-blue-preference","quote":"喜欢蓝色","importance":0.7,"reason":"用户明确表达的长期偏好"}]}`}}}})
	}))
	defer model.Close()
	a.mu.Lock()
	a.settings.XiaomiModelSource = "custom"
	a.settings.XiaomiBaseURL = model.URL
	a.settings.XiaomiModel = "fixture"
	a.mu.Unlock()
	b, e := a.selectedMixSoul()
	if e != nil {
		t.Fatal(e)
	}
	heard := "我一直喜欢蓝色"
	if _, e = a.mixSoulCall(context.Background(), *b, map[string]any{"phase": "native.observe", "role": "user", "text": heard, "turnId": "native-learning", "now": time.Now().UTC().Format(time.RFC3339Nano)}); e != nil {
		t.Fatal(e)
	}
	a.learnMixSoulNative(context.Background(), *b, heard, "native-learning")
	a.learnMixSoulNative(context.Background(), *b, heard, "native-learning")
	if calls.Load() != 1 {
		t.Fatalf("duplicate native learning requests %d", calls.Load())
	}
	out, e := a.mixSoulCall(context.Background(), *b, map[string]any{"phase": "native.recall", "heard": "蓝色", "now": time.Now().UTC().Format(time.RFC3339Nano)})
	if e != nil || !strings.Contains(out.Instructions, "synthetic-blue-preference") || !strings.Contains(out.Instructions, `"importance":0.7`) {
		t.Fatal("native learning missing", e)
	}
}

func TestMixSoulPrivateAcceptanceMetadata(t *testing.T) {
	t.Setenv("AIDE_ASSISTANT_ENGINE", "mixsoul")
	a := testApp(t)
	unlockAssistantSessionForTest(t, a)
	b, e := a.selectedMixSoul()
	if e != nil || b == nil {
		t.Fatal(e)
	}
	prefix := "MIXPROBE-0123456789abcdef0123456789abcdef"
	dir := filepath.Join(a.dataPath, "assistant", "mixsoul")
	if e = os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	marker := filepath.Join(dir, "acceptance-prefix")
	if e = os.WriteFile(marker, []byte(prefix+"\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if a.mixSoulProbePrefix(*b, "unrelated") != "" {
		t.Fatal("unrelated input selected")
	}
	if a.mixSoulProbePrefix(*b, prefix) != prefix {
		t.Fatal("probe marker not selected")
	}
	if e = os.Chmod(marker, 0644); e != nil {
		t.Fatal(e)
	}
	if a.mixSoulProbePrefix(*b, prefix) != "" {
		t.Fatal("public marker accepted")
	}
	_ = os.Chmod(marker, 0600)
	_, e = a.mixSoulCall(context.Background(), *b, map[string]any{"phase": "begin", "heard": prefix + " secret synthetic statement", "now": time.Now().UTC().Format(time.RFC3339Nano), "turnId": "probe-1", "history": []Message{}})
	if e != nil {
		t.Fatal(e)
	}
	scope := hash([]byte(b.Workspace + "\x00mixsoul-local-v1"))[:32]
	raw, e := os.ReadFile(filepath.Join(dir, scope, "acceptance-audit.jsonl"))
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(raw), "secret synthetic statement") || strings.Contains(string(raw), prefix) {
		t.Fatal("audit leaked statement")
	}
	var audit struct {
		History int `json:"historyMessages"`
		Probe   struct {
			Records []struct {
				Kind       string  `json:"kind"`
				Importance float64 `json:"importance"`
			}
		} `json:"probe"`
	}
	if e = json.Unmarshal(bytes.TrimSpace(raw), &audit); e != nil {
		t.Fatal(e)
	}
	if audit.History != 0 || len(audit.Probe.Records) != 1 || audit.Probe.Records[0].Kind != "episode" {
		t.Fatalf("bad metadata: %s", raw)
	}
	_, e = a.mixSoulCall(context.Background(), *b, map[string]any{"phase": "begin", "heard": prefix + " 我今天很难过", "now": time.Now().UTC().Format(time.RFC3339Nano), "turnId": "probe-affect", "history": []Message{}})
	if e != nil {
		t.Fatal(e)
	}
	more, e := os.ReadFile(filepath.Join(dir, scope, "acceptance-audit.jsonl"))
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(more), "我今天很难过") || strings.Contains(string(more), prefix) || !strings.Contains(string(more), "emotionContext") {
		t.Fatal("affect probe leaked evidence or omitted metadata")
	}
	_ = os.Remove(marker)
	if a.mixSoulProbePrefix(*b, prefix) != "" {
		t.Fatal("disabled marker remained active")
	}
}

// Uses the real embedded worker and host completion loop; only the upstream
// model is synthetic. This verifies appraisal changes the dialogue request,
// that appraisal has no tools, and that optional appraisal failure degrades.
func TestMixSoulEmotionHostDialogue(t *testing.T) {
	for _, failAppraisal := range []bool{false, true} {
		name := "appraisal-success"
		if failAppraisal {
			name = "appraisal-failure"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv("AIDE_ASSISTANT_ENGINE", "mixsoul")
			a := testApp(t)
			unlockAssistantSessionForTest(t, a)
			var calls atomic.Int32
			model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Messages []Message         `json:"messages"`
					Tools    []json.RawMessage `json:"tools"`
				}
				if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
					t.Error(e)
					http.Error(w, "bad request", 400)
					return
				}
				n := calls.Add(1)
				if n == 1 {
					if len(body.Tools) != 0 {
						t.Error("appraisal gained tools")
					}
					if failAppraisal {
						http.Error(w, "synthetic appraisal unavailable", 400)
						return
					}
				}
				if n == 2 {
					var all strings.Builder
					for _, m := range body.Messages {
						all.WriteString(m.Content)
					}
					if !strings.Contains(all.String(), "acknowledge-gently") || !strings.Contains(all.String(), "sadness") {
						t.Error("dialogue did not receive grounded affect")
					}
				}
				text := `{"labels":["sadness"],"basis":"explicit","quote":"我今天很难过","confidence":0.9,"linkedMemoryIds":[]}`
				if n == 2 {
					text = "这件事让你不太好受，我先听你说。"
				}
				if n >= 3 {
					text = `{"items":[]}`
				}
				jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": text}}}})
			}))
			defer model.Close()
			got, e := a.runAssistantAgenticLoop(context.Background(), Settings{BaseURL: model.URL, Model: "synthetic"}, "合成青铜任务我今天很难过", "", "text")
			if e != nil || got.Reply != "这件事让你不太好受，我先听你说。" || calls.Load() != 3 {
				t.Fatalf("reply=%q calls=%d err=%v", got.Reply, calls.Load(), e)
			}
			b, e := a.selectedMixSoul()
			if e != nil || b == nil {
				t.Fatal(e)
			}
			recalled, e := a.mixSoulCall(context.Background(), *b, map[string]any{"phase": "native.recall", "heard": "合成青铜任务", "now": time.Now().UTC().Format(time.RFC3339Nano)})
			if e != nil || !strings.Contains(recalled.Instructions, "explicit-user-affect") {
				t.Fatal("affect event did not persist", e)
			}
		})
	}
}

func TestMixSoulOwnerManagementGatesAndNoModelCalls(t *testing.T) {
	t.Setenv("AIDE_ASSISTANT_ENGINE", "mixsoul")
	a := testApp(t)
	unlockAssistantSessionForTest(t, a)
	b, err := a.selectedMixSoul()
	if err != nil || b == nil {
		t.Fatal(err)
	}
	endpoint := "/api/sessions/" + b.Session + "/assistant-memory"
	requireStatus(t, request(a, "POST", endpoint, map[string]any{"operation": "status"}), 200)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = a.mixSoulCall(context.Background(), *b, map[string]any{"phase": "begin", "turnId": "management-seed", "heard": "记住：synthetic-management-secret", "now": now})
	if err != nil {
		t.Fatal(err)
	}
	w := request(a, "POST", endpoint, map[string]any{"operation": "list"})
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), "synthetic-management-secret") || strings.Contains(w.Body.String(), "vectors") {
		t.Fatal("owner memory view incorrect")
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("sensitive view may cache")
	}
	var page struct {
		Records []struct {
			ID string `json:"id"`
		}
	}
	if err = json.Unmarshal(w.Body.Bytes(), &page); err != nil || len(page.Records) != 1 {
		t.Fatal("invalid list", err)
	}
	requireStatus(t, request(a, "POST", endpoint, map[string]any{"operation": "delete", "recordId": page.Records[0].ID}), 200)
	w = request(a, "POST", endpoint, map[string]any{"operation": "list"})
	if strings.Contains(w.Body.String(), "synthetic-management-secret") {
		t.Fatal("deleted evidence resurrected")
	}
	requireStatus(t, request(a, "POST", endpoint, map[string]any{"operation": "run-shell"}), 400)
	requireStatus(t, request(a, "POST", "/api/sessions/wrong/assistant-memory", map[string]any{"operation": "list"}), 404)

	// Full Chinese memory text must not overflow the bounded worker IPC page,
	// and editing must fetch the untruncated original separately.
	for i := 0; i < 12; i++ {
		_, err = a.mixSoulCall(context.Background(), *b, map[string]any{"phase": "begin", "turnId": "large-page-" + string(rune('a'+i)), "heard": "记住：" + string(rune('a'+i)) + strings.Repeat("合成段落", 400), "now": now})
		if err != nil {
			t.Fatal(err)
		}
	}
	w = request(a, "POST", endpoint, map[string]any{"operation": "list"})
	requireStatus(t, w, 200)
	var large struct {
		PageSize int `json:"pageSize"`
		Records  []struct {
			ID        string `json:"id"`
			Text      string `json:"text"`
			Truncated bool   `json:"truncated"`
		}
	}
	if err = json.Unmarshal(w.Body.Bytes(), &large); err != nil || large.PageSize != 10 || len(large.Records) != 10 || !large.Records[0].Truncated {
		t.Fatal("bounded management page failed", err)
	}
	w = request(a, "POST", endpoint, map[string]any{"operation": "get", "recordId": large.Records[0].ID})
	requireStatus(t, w, 200)
	var detail struct {
		Record struct {
			Text string `json:"text"`
		} `json:"record"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &detail); err != nil || strings.Count(detail.Record.Text, "合成段落") != 400 {
		t.Fatal("full text missing", err)
	}
	a.clearAssistantUnlock()
	requireStatus(t, request(a, "POST", endpoint, map[string]any{"operation": "list"}), 423)
}

func TestMixSoulLocalEmbeddingOperatorConfigAndGates(t *testing.T) {
	t.Setenv("AIDE_ASSISTANT_ENGINE", "mixsoul")
	var calls atomic.Int32
	embedding := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var in struct {
			Input []string `json:"input"`
			Model string   `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
		}
		if in.Model != "local-embedding-fixture" || len(in.Input) > 16 {
			t.Error("invalid local embedding request")
		}
		rows := []any{}
		for i := range in.Input {
			rows = append(rows, map[string]any{"index": i, "embedding": []float64{1, 0, 0}})
		}
		jsonOut(w, 200, map[string]any{"data": rows})
	}))
	defer embedding.Close()
	raw, _ := json.Marshal(map[string]any{"endpoint": embedding.URL + "/v1/embeddings", "model": "local-embedding-fixture", "revision": "fixture-v1", "dimensions": 3})
	t.Setenv("AIDE_MIXSOUL_LOCAL_EMBEDDING", string(raw))
	a := testApp(t)
	unlockAssistantSessionForTest(t, a)
	b, err := a.selectedMixSoul()
	if err != nil || b == nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, phase := range []string{"native.observe", "native.recall"} {
		out, e := a.mixSoulCall(context.Background(), *b, map[string]any{"phase": phase, "role": "user", "turnId": "local-vector-seed", "heard": "合成向量查询", "text": "记住：合成向量事实", "now": now})
		if e != nil || !strings.Contains(out.Instructions, `"status":"ready"`) {
			t.Fatal("local semantic context absent", e)
		}
	}
	before := calls.Load()
	_, err = a.mixSoulCall(context.Background(), *b, map[string]any{"phase": "pulse", "now": now})
	if err != nil || calls.Load() != before {
		t.Fatal("idle called embedding model", err)
	}
	a.clearAssistantUnlock()
	_, err = a.mixSoulCall(context.Background(), *b, map[string]any{"phase": "native.recall", "heard": "合成向量查询", "now": now})
	if err == nil || calls.Load() != before {
		t.Fatal("locked embedding access", err)
	}
}
