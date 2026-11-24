package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestKVCacheCloudPrefixOptInAndStableKey(t *testing.T) {
	manager := newKVCacheManager(t.TempDir(), KVCacheSettings{Enabled: true, Backend: "memory", CapacityMiB: 16, TTLMinutes: 60, CloudPrefix: true})
	if err := manager.Start(); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	messages := []Message{{Role: "system", Content: "private system"}, {Role: "user", Content: "first private question"}}
	cfg := Settings{Model: "gpt-5.6-sol", BaseURL: "https://api.openai.com/v1", KVCache: manager.cfg, kvCache: manager}
	body := map[string]any{"model": cfg.Model, "messages": messages}
	lease, err := prepareKVCacheRequest(context.Background(), cfg, messages, body)
	if err != nil {
		t.Fatal(err)
	}
	first := body["prompt_cache_key"]
	if first == nil || !strings.HasPrefix(first.(string), "aide-") {
		t.Fatalf("missing OpenAI prefix key: %#v", body)
	}
	body["prompt_cache_key"] = nil
	next := append(append([]Message(nil), messages...), Message{Role: "assistant", Content: "answer"}, Message{Role: "user", Content: "next"})
	lease, err = prepareKVCacheRequest(context.Background(), cfg, next, body)
	if err != nil {
		t.Fatal(err)
	}
	if body["prompt_cache_key"] != first {
		t.Fatalf("prefix key changed as conversation grew: %v != %v", body["prompt_cache_key"], first)
	}
	lease.finish(false, "", nil)
	if stats := manager.Stats(); stats.PrefixRequests != 2 || stats.CachedTokens != 0 {
		t.Fatalf("prefix requests must not be counted as cache hits: %+v", stats)
	}
	manager.ObserveUsage(TokenUsage{CacheHit: 123})
	if stats := manager.Stats(); stats.CachedTokens != 123 {
		t.Fatalf("upstream cached tokens not recorded: %+v", stats)
	}
}

func TestKVCacheDisabledAndProviderOffLeaveBodyUntouched(t *testing.T) {
	for _, cfg := range []Settings{
		{BaseURL: "https://api.openai.com/v1"},
		{BaseURL: "https://api.openai.com/v1", KVCache: KVCacheSettings{Enabled: true}, ModelProviders: []ModelProvider{{ID: "p", BaseURL: "https://api.openai.com/v1", KVCacheEngine: "off"}}, Models: []ModelRef{{ID: "m", ProviderID: "p"}}, Model: "m"},
	} {
		manager := newKVCacheManager(t.TempDir(), cfg.KVCache)
		if err := manager.Start(); err != nil {
			t.Fatal(err)
		}
		cfg.kvCache = manager
		cfg.KVCache = manager.cfg
		body := map[string]any{"model": cfg.Model}
		if _, err := prepareKVCacheRequest(context.Background(), cfg, nil, body); err != nil {
			t.Fatal(err)
		}
		if _, exists := body["prompt_cache_key"]; exists {
			t.Fatalf("disabled cache changed request body: %#v", body)
		}
		manager.Close()
	}
}

type llamaContractServer struct {
	t           *testing.T
	disk        string
	saveSize    int
	mu          sync.Mutex
	actions     []string
	cacheFields []bool
	active      atomic.Int32
	maxActive   atomic.Int32
	server      *httptest.Server
}

func newLlamaContractServer(t *testing.T, disk string, saveSize int) *llamaContractServer {
	disk = filepath.Join(disk, "llamacpp")
	if err := os.MkdirAll(disk, 0o700); err != nil {
		t.Fatal(err)
	}
	s := &llamaContractServer{t: t, disk: disk, saveSize: saveSize}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slots/0" {
			action := r.URL.Query().Get("action")
			s.mu.Lock()
			s.actions = append(s.actions, action)
			s.mu.Unlock()
			switch action {
			case "save":
				filename := r.URL.Query().Get("filename")
				if !validKVCacheFilename(filename) {
					t.Errorf("invalid save filename %q", filename)
					w.WriteHeader(400)
					return
				}
				if err := os.WriteFile(filepath.Join(disk, filename), make([]byte, saveSize), 0o600); err != nil {
					t.Errorf("save slot: %v", err)
					w.WriteHeader(500)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"n_tokens": 128})
				return
			case "restore":
				if !validKVCacheFilename(r.URL.Query().Get("filename")) {
					t.Errorf("invalid restore filename")
					w.WriteHeader(400)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"n_tokens": 96})
				return
			case "erase":
				_ = json.NewEncoder(w).Encode(map[string]any{"n_tokens": 0})
				return
			default:
				w.WriteHeader(400)
				return
			}
		}
		if r.URL.Path != "/v1/chat/completions" {
			w.WriteHeader(404)
			return
		}
		active := s.active.Add(1)
		defer s.active.Add(-1)
		for {
			previous := s.maxActive.Load()
			if active <= previous || s.maxActive.CompareAndSwap(previous, active) {
				break
			}
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode chat: %v", err)
		}
		s.mu.Lock()
		s.cacheFields = append(s.cacheFields, body["id_slot"] == float64(0) && body["cache_prompt"] == true)
		s.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`)
	}))
	t.Cleanup(s.server.Close)
	return s
}

func (s *llamaContractServer) recordedActions() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.actions...)
}

func (s *llamaContractServer) recordedCacheFields() []bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]bool(nil), s.cacheFields...)
}

func llamaTestConfig(serverURL string, manager *kvCacheManager) Settings {
	return Settings{
		Model: "local-model", BaseURL: serverURL + "/v1", APIKey: "secret-key",
		Models:         []ModelRef{{ID: "local-model", ProviderID: "local"}},
		ModelProviders: []ModelProvider{{ID: "local", BaseURL: serverURL + "/v1", KVCacheEngine: "llamacpp"}},
		KVCache:        manager.cfg, kvCache: manager,
	}
}

func TestKVCacheLlamaDiskRestoreAndPrefixChain(t *testing.T) {
	disk := t.TempDir()
	engine := newLlamaContractServer(t, disk, 4096)
	manager := newKVCacheManager(t.TempDir(), KVCacheSettings{Enabled: true, Backend: "disk", CapacityMiB: 16, TTLMinutes: 60, DiskPath: disk, LlamaCpp: true})
	if err := manager.Start(); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	cfg := llamaTestConfig(engine.server.URL, manager)
	messages := []Message{{Role: "system", Content: "secret prompt"}, {Role: "user", Content: "private question"}}
	text, _, _, err := complete(context.Background(), cfg, messages, ProfileParams{}, nil, nil)
	if err != nil || text != "hello" {
		t.Fatalf("first complete: %q %v", text, err)
	}
	next := append(append([]Message(nil), messages...), Message{Role: "assistant", Content: text}, Message{Role: "user", Content: "continue"})
	if _, _, _, err := complete(context.Background(), cfg, next, ProfileParams{}, nil, nil); err != nil {
		t.Fatal(err)
	}
	actions := engine.recordedActions()
	if len(actions) != 6 || actions[0] != "erase" || actions[1] != "save" || actions[2] != "erase" || actions[3] != "restore" || actions[4] != "save" || actions[5] != "erase" {
		t.Fatalf("unexpected slot lifecycle: %v", actions)
	}
	if fields := engine.recordedCacheFields(); len(fields) != 2 || !fields[0] || !fields[1] {
		t.Fatalf("main conversation lost llama cache fields: %v", fields)
	}
	stats := manager.Stats()
	if stats.Restores != 1 || stats.RestoredTokens != 96 || stats.Saves != 2 || stats.SavedTokens != 256 || stats.UsedBytes > stats.CapacityBytes {
		t.Fatalf("unexpected cache stats: %+v", stats)
	}
	manifest, err := os.ReadFile(filepath.Join(disk, kvCacheManifestName))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"secret prompt", "private question", "secret-key", engine.server.URL} {
		if strings.Contains(string(manifest), secret) {
			t.Fatalf("manifest leaked %q: %s", secret, manifest)
		}
	}
}

func TestKVCacheLlamaTopicSummaryDoesNotEvictConversation(t *testing.T) {
	engine := newLlamaContractServer(t, t.TempDir(), 1024)
	manager := newKVCacheManager(t.TempDir(), KVCacheSettings{Enabled: true, Backend: "memory", CapacityMiB: 16, TTLMinutes: 60, LlamaCpp: true})
	if err := manager.Start(); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	a := testApp(t)
	cfg := llamaTestConfig(engine.server.URL, manager)
	messages := []Message{{Role: "system", Content: "main conversation"}, {Role: "user", Content: "first question\n\nruntime-only instruction"}}
	text, _, _, err := complete(context.Background(), cfg, messages, ProfileParams{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := &Session{ID: "topic-session", Title: "old title"}
	task := &Task{ID: "topic-task", Prompt: "summarize the main task"}
	a.mu.Lock()
	a.sessions[s.ID] = s
	a.mu.Unlock()
	a.summarizeTopic(context.Background(), s, task, cfg, ProfileParams{})
	// The next request persists the user's original text, not the runtime-only
	// suffix. Memory mode must still leave prefix matching to llama.cpp.
	next := []Message{messages[0], {Role: "user", Content: "first question"}, {Role: "assistant", Content: text}, {Role: "user", Content: "continue"}}
	if _, _, _, err := complete(context.Background(), cfg, next, ProfileParams{}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if fields := engine.recordedCacheFields(); len(fields) != 3 || !fields[0] || !fields[1] || !fields[2] {
		t.Fatalf("main and auxiliary requests must retain the engine cache: %v", fields)
	}
	if actions := engine.recordedActions(); len(actions) != 0 {
		t.Fatalf("memory-mode requests must preserve llama.cpp RAM cache: %v", actions)
	}
	if stats := manager.Stats(); stats.Saves != 2 || stats.EntryCount != 1 || stats.ActiveLeases != 0 {
		t.Fatalf("topic summary replaced the main cache entry: %+v", stats)
	}
}

func TestKVCacheLlamaOverflowRollsBackWithoutFailingAnswer(t *testing.T) {
	disk := t.TempDir()
	engine := newLlamaContractServer(t, disk, 17<<20)
	manager := newKVCacheManager(t.TempDir(), KVCacheSettings{Enabled: true, Backend: "disk", CapacityMiB: 16, TTLMinutes: 60, DiskPath: disk, LlamaCpp: true})
	if err := manager.Start(); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	cfg := llamaTestConfig(engine.server.URL, manager)
	text, _, _, err := complete(context.Background(), cfg, []Message{{Role: "user", Content: "large"}}, ProfileParams{}, nil, nil)
	if err != nil || text != "hello" {
		t.Fatalf("cache overflow must not fail model answer: %q %v", text, err)
	}
	stats := manager.Stats()
	if stats.OverflowRollbacks != 1 || stats.EntryCount != 0 || stats.ManagedBytes != 0 || stats.UsedBytes > stats.CapacityBytes {
		t.Fatalf("overflow was not rolled back: %+v", stats)
	}
	matches, err := filepath.Glob(filepath.Join(disk, "llamacpp", "aide-*.bin"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("oversized slot file remained: %v %v", matches, err)
	}
}

func TestKVCacheLlamaSlotSerializesConcurrentCalls(t *testing.T) {
	disk := t.TempDir()
	engine := newLlamaContractServer(t, disk, 1024)
	manager := newKVCacheManager(t.TempDir(), KVCacheSettings{Enabled: true, Backend: "memory", CapacityMiB: 16, TTLMinutes: 60, LlamaCpp: true})
	if err := manager.Start(); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	cfg := llamaTestConfig(engine.server.URL, manager)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, _, err := complete(context.Background(), cfg, []Message{{Role: "user", Content: "same"}}, ProfileParams{}, nil, nil); err != nil {
				t.Errorf("concurrent complete: %v", err)
			}
		}()
	}
	wg.Wait()
	if got := engine.maxActive.Load(); got != 1 {
		t.Fatalf("same llama slot served concurrent requests: %d", got)
	}
	if stats := manager.Stats(); stats.ActiveLeases != 0 || stats.EntryCount != 1 {
		t.Fatalf("leases not released: %+v", stats)
	}
}

func TestKVCacheReclaimExpiresEntryAndDeletesOwnedFile(t *testing.T) {
	disk := t.TempDir()
	engine := newLlamaContractServer(t, disk, 2048)
	manager := newKVCacheManager(t.TempDir(), KVCacheSettings{Enabled: true, Backend: "disk", CapacityMiB: 16, TTLMinutes: 1, DiskPath: disk, LlamaCpp: true})
	now := time.Now()
	manager.now = func() time.Time { return now }
	if err := manager.Start(); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	cfg := llamaTestConfig(engine.server.URL, manager)
	if _, _, _, err := complete(context.Background(), cfg, []Message{{Role: "user", Content: "ttl"}}, ProfileParams{}, nil, nil); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	if err := manager.Reclaim(context.Background()); err != nil {
		t.Fatal(err)
	}
	stats := manager.Stats()
	if stats.Expirations != 1 || stats.EntryCount != 0 || stats.ManagedBytes != 0 {
		t.Fatalf("TTL did not reclaim entry: %+v", stats)
	}
	matches, _ := filepath.Glob(filepath.Join(disk, "llamacpp", "aide-*.bin"))
	if len(matches) != 0 {
		t.Fatalf("expired slot file remained: %v", matches)
	}
}

func TestLMCacheConfigUsesFixedBudgetAndSafeDefaults(t *testing.T) {
	data := t.TempDir()
	if err := os.MkdirAll(ConfigDir(data), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := KVCacheSettings{Enabled: true, Backend: "disk", CapacityMiB: 2048, TTLMinutes: 60, DiskPath: filepath.Join(data, "fixed-cache"), VLLM: true}
	if err := writeLMCacheConfig(data, cfg); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(ConfigDir(data), "lmcache.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	for _, want := range []string{"cache_policy: LRU", "enable_lazy_memory_allocator: false", "run_script_api_enabled: false", "max_local_disk_size: 2.000000", `"diskPath"`} {
		if want == `"diskPath"` {
			continue
		}
		if !strings.Contains(text, want) {
			t.Fatalf("LMCache config missing %q:\n%s", want, text)
		}
	}
	if !strings.Contains(text, cfg.DiskPath) || strings.Contains(text, "enable_lazy_memory_allocator: true") {
		t.Fatalf("unsafe or unfixed LMCache config:\n%s", text)
	}
}

func TestKVCacheSplitsOneFixedBudgetAcrossRealEngines(t *testing.T) {
	cfg := KVCacheSettings{Enabled: true, Backend: "memory", CapacityMiB: 16, TTLMinutes: 60, LlamaCpp: true, VLLM: true}
	if got := kvCacheEngineBudgetMiB(cfg, "llamacpp"); got != 8 {
		t.Fatalf("llama.cpp budget = %d, want 8", got)
	}
	if got := llamaLaunchArgs(cfg); len(got) != 2 || got[1] != "8" {
		t.Fatalf("llama.cpp did not receive its fixed share: %v", got)
	}
	data := t.TempDir()
	if err := writeLMCacheConfig(data, cfg); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(ConfigDir(data), "lmcache.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "max_local_cpu_size: 0.007812") {
		t.Fatalf("vLLM did not receive the other 8 MiB: %s", b)
	}
	cfg.Enabled = false
	if err := writeLMCacheConfig(data, cfg); err != nil {
		t.Fatal(err)
	}
	b, err = os.ReadFile(filepath.Join(ConfigDir(data), "lmcache.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "local_cpu: false\nlocal_disk: null\n") {
		t.Fatalf("disabled cache left an active LMCache backend: %s", b)
	}
}

func TestKVCacheCancelledLeaseDoesNotRetainSlot(t *testing.T) {
	manager := newKVCacheManager(t.TempDir(), KVCacheSettings{Enabled: true, Backend: "memory", CapacityMiB: 16, TTLMinutes: 60, LlamaCpp: true})
	if err := manager.Start(); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	cfg := llamaTestConfig("http://127.0.0.1:1", manager)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	body := map[string]any{"model": cfg.Model}
	if _, err := prepareKVCacheRequest(ctx, cfg, []Message{{Role: "user", Content: "cancel"}}, body); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled prepare error = %v", err)
	}
	if _, exists := body["id_slot"]; exists {
		t.Fatal("cancelled request acquired an engine slot")
	}
	if stats := manager.Stats(); stats.ActiveLeases != 0 || stats.EntryCount != 0 {
		t.Fatalf("cancelled lease was retained: %+v", stats)
	}
}

func TestKVCacheSlotLimitFallsBackToNormalRequest(t *testing.T) {
	manager := newKVCacheManager(t.TempDir(), KVCacheSettings{Enabled: true, Backend: "memory", CapacityMiB: 16, TTLMinutes: 60, LlamaCpp: true})
	if err := manager.Start(); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	for i := 0; i < kvCacheMaxEntries; i++ {
		manager.slots[fmt.Sprintf("occupied-%d", i)] = newKVCacheSlot()
	}
	cfg := llamaTestConfig("http://127.0.0.1:1", manager)
	body := map[string]any{"model": cfg.Model}
	lease, err := prepareKVCacheRequest(context.Background(), cfg, []Message{{Role: "user", Content: "fallback"}}, body)
	if err != nil {
		t.Fatalf("cache capacity blocked normal request: %v", err)
	}
	lease.finish(false, "", nil)
	if _, exists := body["id_slot"]; exists {
		t.Fatal("over-capacity cache acquired an engine slot")
	}
	if manager.Stats().LastError == "" {
		t.Fatal("slot limit was not surfaced in diagnostics")
	}
}

func TestKVCacheAdapterSignatureTracksOnlyAdapterChanges(t *testing.T) {
	providers := []ModelProvider{{ID: "a", KVCacheEngine: "llamacpp"}, {ID: "b", KVCacheEngine: "vllm"}}
	before := kvCacheAdapterSignature(providers)
	providers[0].BaseURL = "http://localhost:8000/v1"
	if kvCacheAdapterSignature(providers) != before {
		t.Fatal("ordinary provider endpoint edit must not masquerade as an adapter change")
	}
	providers[0].KVCacheEngine = "off"
	if kvCacheAdapterSignature(providers) == before {
		t.Fatal("adapter disable did not change its generation signature")
	}
}

func TestKVCacheLRUEvictsIdleEntryWithinSharedBudget(t *testing.T) {
	disk := t.TempDir()
	engine := newLlamaContractServer(t, disk, 6<<20)
	manager := newKVCacheManager(t.TempDir(), KVCacheSettings{Enabled: true, Backend: "disk", CapacityMiB: 16, TTLMinutes: 60, DiskPath: disk, LlamaCpp: true, VLLM: true})
	if err := manager.Start(); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	cfg := llamaTestConfig(engine.server.URL, manager)
	for _, model := range []string{"local-model", "local-model-2"} {
		cfg.Model = model
		if _, _, _, err := complete(context.Background(), cfg, []Message{{Role: "user", Content: model}}, ProfileParams{}, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	stats := manager.Stats()
	if stats.Evictions != 1 || stats.EntryCount != 1 || stats.Saves != 2 || stats.UsedBytes > stats.CapacityBytes {
		t.Fatalf("shared fixed budget did not evict LRU: %+v", stats)
	}
}

func TestKVCacheCloseCancelsAndJoinsActiveSave(t *testing.T) {
	saveStarted := make(chan struct{})
	var once sync.Once
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("action") == "save" {
			once.Do(func() { close(saveStarted) })
			<-r.Context().Done()
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"n_tokens": 0})
	}))
	defer engine.Close()
	manager := newKVCacheManager(t.TempDir(), KVCacheSettings{Enabled: true, Backend: "disk", CapacityMiB: 16, TTLMinutes: 60, DiskPath: t.TempDir(), LlamaCpp: true})
	if err := manager.Start(); err != nil {
		t.Fatal(err)
	}
	cfg := llamaTestConfig(engine.URL, manager)
	lease, err := prepareKVCacheRequest(context.Background(), cfg, []Message{{Role: "user", Content: "close"}}, map[string]any{"model": cfg.Model})
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan struct{})
	go func() {
		lease.finish(true, "answer", nil)
		close(finished)
	}()
	select {
	case <-saveStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("save did not start")
	}
	closed := make(chan struct{})
	go func() {
		manager.Close()
		close(closed)
	}()
	for _, done := range []chan struct{}{finished, closed} {
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("close did not cancel and join active cache finalization")
		}
	}
	if stats := manager.Stats(); stats.ActiveLeases != 0 || stats.Saves != 0 || stats.EntryCount != 0 {
		t.Fatalf("closed manager retained a completing save: %+v", stats)
	}
}

func TestKVCacheClearRequiresStoppedVLLMAndRemovesOwnedDisk(t *testing.T) {
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{}`)
	}))
	disk := t.TempDir()
	manager := newKVCacheManager(t.TempDir(), KVCacheSettings{Enabled: true, Backend: "disk", CapacityMiB: 16, TTLMinutes: 60, DiskPath: disk, VLLM: true})
	if err := manager.Start(); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	cfg := llamaTestConfig(engine.URL, manager)
	cfg.ModelProviders[0].KVCacheEngine = "vllm"
	owned := filepath.Join(kvCacheVLLMDir(manager.cfg), "lmcache-owned.bin")
	if err := os.WriteFile(owned, []byte("cache"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := manager.Clear(context.Background(), []Settings{cfg}); err == nil || !strings.Contains(err.Error(), "仍在运行") {
		t.Fatalf("running vLLM clear error = %v", err)
	}
	if _, err := os.Stat(owned); err != nil {
		t.Fatalf("running engine's file was removed: %v", err)
	}
	engine.Close()
	if err := manager.Clear(context.Background(), []Settings{cfg}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(owned); !os.IsNotExist(err) {
		t.Fatalf("stopped engine's Aide-owned disk cache remained: %v", err)
	}
}
