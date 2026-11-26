package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	kvCacheDefaultCapacityMiB int64 = 1024
	kvCacheMinCapacityMiB     int64 = 16
	kvCacheMaxCapacityMiB     int64 = 1024 * 1024
	kvCacheDefaultTTLMinutes        = 24 * 60
	kvCacheMaxTTLMinutes            = 30 * 24 * 60
	kvCacheMaxEntries               = 64
	kvCacheMaxChainMessages         = 256
	kvCacheMetadataEntryBytes int64 = 32 << 10
	kvCacheManifestName             = "aide-kv-manifest.json"
)

// KVCacheSettings is an experimental, fixed-budget control plane. Cloud prefix
// caching never stores prompts or answers locally. Real KV tensors remain in the
// selected inference engine and its explicitly configured local cache backend.
type KVCacheSettings struct {
	Enabled     bool   `json:"enabled"`
	Backend     string `json:"backend"` // memory | disk
	CapacityMiB int64  `json:"capacityMiB"`
	DiskPath    string `json:"diskPath,omitempty"`
	TTLMinutes  int    `json:"ttlMinutes"`
	CloudPrefix bool   `json:"cloudPrefix"`
	LlamaCpp    bool   `json:"llamaCpp"`
	VLLM        bool   `json:"vllm"`
}

func defaultKVCacheSettings() KVCacheSettings {
	return KVCacheSettings{
		Backend: "memory", CapacityMiB: kvCacheDefaultCapacityMiB,
		TTLMinutes: kvCacheDefaultTTLMinutes, CloudPrefix: true, LlamaCpp: true, VLLM: true,
	}
}

// Real-KV engines share one fixed total budget rather than each allocating the
// full amount. Cloud prefix caching stores no tensors and takes no local share.
func kvCacheEngineBudgetMiB(cfg KVCacheSettings, engine string) int64 {
	parts := int64(0)
	if cfg.LlamaCpp {
		parts++
	}
	if cfg.VLLM {
		parts++
	}
	if parts == 0 || engine != "llamacpp" && engine != "vllm" {
		return cfg.CapacityMiB
	}
	return cfg.CapacityMiB / parts
}

func kvCacheLlamaDir(cfg KVCacheSettings) string { return filepath.Join(cfg.DiskPath, "llamacpp") }
func kvCacheVLLMDir(cfg KVCacheSettings) string  { return filepath.Join(cfg.DiskPath, "vllm") }

func (m *kvCacheManager) llamaBudgetBytes() int64 {
	return kvCacheEngineBudgetMiB(m.cfg, "llamacpp") << 20
}

func kvCacheObservedDiskUsage(path string) (int64, error) {
	var total int64
	err := filepath.WalkDir(path, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("KV Cache 目录禁止符号链接")
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if info.Size() > int64(^uint64(0)>>1)-total {
			return errors.New("KV Cache 用量统计溢出")
		}
		total += info.Size()
		return nil
	})
	return total, err
}

func normalizeKVCacheDefaults(cfg *KVCacheSettings) {
	legacyZero := *cfg == (KVCacheSettings{})
	if cfg.Backend == "" {
		cfg.Backend = "memory"
	}
	if cfg.CapacityMiB == 0 {
		cfg.CapacityMiB = kvCacheDefaultCapacityMiB
	}
	if cfg.TTLMinutes == 0 {
		cfg.TTLMinutes = kvCacheDefaultTTLMinutes
	}
	// A completely absent legacy object must retain the opt-in feature defaults.
	// Explicit false values are preserved once any cache field is present.
	if legacyZero {
		defaults := defaultKVCacheSettings()
		cfg.CloudPrefix, cfg.LlamaCpp, cfg.VLLM = defaults.CloudPrefix, defaults.LlamaCpp, defaults.VLLM
	}
}

func normalizeKVCacheSettings(data string, in KVCacheSettings) (KVCacheSettings, error) {
	normalizeKVCacheDefaults(&in)
	if in.Backend != "memory" && in.Backend != "disk" {
		return KVCacheSettings{}, errors.New("KV Cache 存储类型仅支持 memory 或 disk")
	}
	if in.CapacityMiB < kvCacheMinCapacityMiB || in.CapacityMiB > kvCacheMaxCapacityMiB {
		return KVCacheSettings{}, fmt.Errorf("KV Cache 固定容量须在 %d–%d MiB 之间", kvCacheMinCapacityMiB, kvCacheMaxCapacityMiB)
	}
	if in.TTLMinutes < 1 || in.TTLMinutes > kvCacheMaxTTLMinutes {
		return KVCacheSettings{}, fmt.Errorf("KV Cache 生命周期须在 1–%d 分钟之间", kvCacheMaxTTLMinutes)
	}
	if in.Backend == "disk" {
		if strings.TrimSpace(in.DiskPath) == "" {
			in.DiskPath = filepath.Join(data, "kv-cache")
		}
		if !filepath.IsAbs(in.DiskPath) {
			return KVCacheSettings{}, errors.New("KV Cache 磁盘路径必须是固定的绝对路径")
		}
		in.DiskPath = filepath.Clean(in.DiskPath)
		volume := filepath.VolumeName(in.DiskPath) + string(os.PathSeparator)
		if in.DiskPath == string(os.PathSeparator) || (filepath.VolumeName(in.DiskPath) != "" && in.DiskPath == volume) {
			return KVCacheSettings{}, errors.New("KV Cache 不能直接使用文件系统根目录")
		}
	} else {
		in.DiskPath = ""
	}
	return in, nil
}

type kvCacheEntry struct {
	Key       string    `json:"key"`
	Provider  string    `json:"provider"`
	Model     string    `json:"model"`
	Slot      int       `json:"slot"`
	File      string    `json:"file,omitempty"`
	Size      int64     `json:"size"`
	Chain     []string  `json:"chain"`
	UpdatedAt time.Time `json:"updatedAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type kvCacheManifest struct {
	Version    int            `json:"version"`
	Generation uint64         `json:"generation"`
	Entries    []kvCacheEntry `json:"entries"`
}

type kvCacheStats struct {
	CapacityBytes     int64  `json:"capacityBytes"`
	UsedBytes         int64  `json:"usedBytes"`
	ManagedBytes      int64  `json:"managedBytes"`
	EntryCount        int    `json:"entryCount"`
	ActiveLeases      int    `json:"activeLeases"`
	PrefixRequests    uint64 `json:"prefixRequests"`
	Restores          uint64 `json:"restores"`
	RestoredTokens    uint64 `json:"restoredTokens"`
	Saves             uint64 `json:"saves"`
	SavedTokens       uint64 `json:"savedTokens"`
	Evictions         uint64 `json:"evictions"`
	Expirations       uint64 `json:"expirations"`
	OverflowRollbacks uint64 `json:"overflowRollbacks"`
	CachedTokens      uint64 `json:"cachedTokens"`
	Generation        uint64 `json:"generation"`
	LastError         string `json:"lastError,omitempty"`
}

type kvCacheSlot struct {
	token chan struct{}
}

func newKVCacheSlot() *kvCacheSlot {
	s := &kvCacheSlot{token: make(chan struct{}, 1)}
	s.token <- struct{}{}
	return s
}

func (s *kvCacheSlot) acquire(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.token:
		if err := ctx.Err(); err != nil {
			s.release()
			return err
		}
		return nil
	}
}

func (s *kvCacheSlot) release() { s.token <- struct{}{} }

type kvCacheManager struct {
	mu         sync.Mutex
	dataPath   string
	cfg        KVCacheSettings
	entries    map[string]*kvCacheEntry
	slots      map[string]*kvCacheSlot
	stats      kvCacheStats
	generation uint64
	closed     bool
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	leaseWG    sync.WaitGroup
	now        func() time.Time
}

func newKVCacheManager(data string, cfg KVCacheSettings) *kvCacheManager {
	ctx, cancel := context.WithCancel(context.Background())
	m := &kvCacheManager{
		dataPath: data, cfg: cfg, entries: map[string]*kvCacheEntry{}, slots: map[string]*kvCacheSlot{},
		generation: 1, ctx: ctx, cancel: cancel, now: time.Now,
	}
	m.stats.Generation = 1
	return m
}

func (m *kvCacheManager) Start() error {
	cfg, err := normalizeKVCacheSettings(m.dataPath, m.cfg)
	if err != nil {
		return err
	}
	m.cfg = cfg
	if cfg.Enabled && cfg.Backend == "disk" {
		if err := prepareKVCacheDiskPath(cfg.DiskPath); err != nil {
			return err
		}
		if cfg.LlamaCpp {
			if err := prepareKVCacheDiskPath(kvCacheLlamaDir(cfg)); err != nil {
				return err
			}
		}
		if cfg.VLLM {
			if err := prepareKVCacheDiskPath(kvCacheVLLMDir(cfg)); err != nil {
				return err
			}
		}
		if err := m.loadManifest(); err != nil {
			return err
		}
	}
	if err := writeLMCacheConfig(m.dataPath, cfg); err != nil {
		return err
	}
	if cfg.Enabled {
		m.wg.Add(1)
		go m.reclaimLoop()
	}
	return nil
}

func (m *kvCacheManager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	m.generation++
	m.stats.Generation = m.generation
	m.mu.Unlock()
	m.cancel()
	m.leaseWG.Wait()
	m.wg.Wait()
}

func (m *kvCacheManager) capacityBytes() int64 { return m.cfg.CapacityMiB << 20 }

func (m *kvCacheManager) managedBytesLocked() int64 {
	used := int64(0)
	for _, entry := range m.entries {
		used += entry.Size + kvCacheMetadataEntryBytes
	}
	return used
}

func (m *kvCacheManager) setErrorLocked(err error) {
	if err != nil {
		m.stats.LastError = err.Error()
	}
}

func (m *kvCacheManager) slotLocked(key string) (*kvCacheSlot, error) {
	if slot := m.slots[key]; slot != nil {
		return slot, nil
	}
	if len(m.slots) >= kvCacheMaxEntries {
		return nil, errors.New("KV Cache 槽位数量已达到固定上限")
	}
	slot := newKVCacheSlot()
	m.slots[key] = slot
	return slot, nil
}

func (m *kvCacheManager) manifestPath() string {
	return filepath.Join(m.cfg.DiskPath, kvCacheManifestName)
}

func (m *kvCacheManager) loadManifest() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = map[string]*kvCacheEntry{}
	b, err := os.ReadFile(m.manifestPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取 KV Cache 清单失败: %w", err)
	}
	if len(b) > int(kvCacheMaxEntries*kvCacheMetadataEntryBytes) {
		return errors.New("KV Cache 清单超过固定元数据上限")
	}
	var manifest kvCacheManifest
	if err := json.Unmarshal(b, &manifest); err != nil {
		return fmt.Errorf("解析 KV Cache 清单失败: %w", err)
	}
	if manifest.Version != 1 || len(manifest.Entries) > kvCacheMaxEntries {
		return errors.New("KV Cache 清单版本或条目数无效")
	}
	for _, item := range manifest.Entries {
		entry := item
		if entry.Key == "" || entry.Slot < 0 || len(entry.Chain) == 0 || len(entry.Chain) > kvCacheMaxChainMessages || entry.Size < 0 || !validKVCacheFilename(entry.File) {
			continue
		}
		path := filepath.Join(kvCacheLlamaDir(m.cfg), entry.File)
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		entry.Size = info.Size()
		m.entries[entry.Key] = &entry
		if _, err := m.slotLocked(entry.Key); err != nil {
			return err
		}
	}
	m.generation = manifest.Generation + 1
	m.stats.Generation = m.generation
	m.evictOverflowLocked(nil)
	return m.saveManifestLocked()
}

func (m *kvCacheManager) saveManifestLocked() error {
	if m.cfg.Backend != "disk" {
		return nil
	}
	manifest := kvCacheManifest{Version: 1, Generation: m.generation, Entries: make([]kvCacheEntry, 0, len(m.entries))}
	for _, entry := range m.entries {
		manifest.Entries = append(manifest.Entries, *entry)
	}
	sort.Slice(manifest.Entries, func(i, j int) bool { return manifest.Entries[i].Key < manifest.Entries[j].Key })
	b, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	if len(b) > int(kvCacheMaxEntries*kvCacheMetadataEntryBytes) {
		return errors.New("KV Cache 清单超过固定元数据上限")
	}
	tmp, err := os.CreateTemp(m.cfg.DiskPath, ".aide-kv-manifest-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, m.manifestPath())
}

func validKVCacheFilename(name string) bool {
	return len(name) == 73 && strings.HasPrefix(name, "aide-") && strings.HasSuffix(name, ".bin") && filepath.Base(name) == name
}

func prepareKVCacheDiskPath(path string) error {
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("KV Cache 磁盘路径不能是符号链接")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("创建固定 KV Cache 目录失败: %w", err)
	}
	return os.Chmod(path, 0o700)
}

func (m *kvCacheManager) evictOverflowLocked(protect map[string]bool) {
	for used := m.managedBytesLocked(); used > m.llamaBudgetBytes() && len(m.entries) > 0; used = m.managedBytesLocked() {
		var oldest *kvCacheEntry
		for _, entry := range m.entries {
			if protect != nil && protect[entry.Key] {
				continue
			}
			if oldest == nil || entry.UpdatedAt.Before(oldest.UpdatedAt) {
				oldest = entry
			}
		}
		if oldest == nil {
			return
		}
		m.removeEntryLocked(oldest.Key)
		m.stats.Evictions++
	}
}

func (m *kvCacheManager) removeEntryLocked(key string) {
	entry := m.entries[key]
	if entry == nil {
		return
	}
	if entry.File != "" && m.cfg.DiskPath != "" && validKVCacheFilename(entry.File) {
		_ = os.Remove(filepath.Join(kvCacheLlamaDir(m.cfg), entry.File))
	}
	delete(m.entries, key)
}

func (m *kvCacheManager) reclaimLoop() {
	defer m.wg.Done()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
			_ = m.Reclaim(ctx)
			cancel()
		}
	}
}

// Reclaim expires idle entries. The next request also erases a stale llama.cpp
// slot before reuse, so an offline engine never leaves ambiguous restore state.
func (m *kvCacheManager) Reclaim(ctx context.Context) error {
	m.mu.Lock()
	keys := make([]string, 0, len(m.slots))
	for key := range m.slots {
		keys = append(keys, key)
	}
	m.mu.Unlock()
	sort.Strings(keys)
	for _, key := range keys {
		m.mu.Lock()
		slot := m.slots[key]
		m.mu.Unlock()
		if slot == nil {
			continue
		}
		if err := slot.acquire(ctx); err != nil {
			return err
		}
		m.mu.Lock()
		entry := m.entries[key]
		if entry != nil && !m.now().Before(entry.ExpiresAt) {
			m.removeEntryLocked(key)
			m.stats.Expirations++
			if err := m.saveManifestLocked(); err != nil {
				m.setErrorLocked(err)
			}
		}
		m.mu.Unlock()
		slot.release()
	}
	return nil
}

func ensureVLLMStopped(ctx context.Context, engines []Settings) error {
	for _, cfg := range engines {
		if kvCacheEngineForConfig(cfg) != "vllm" {
			continue
		}
		u, err := url.Parse(cfg.BaseURL)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return errors.New("vLLM Base URL 无效，无法确认引擎是否已停止")
		}
		u.Path = strings.TrimRight(u.Path, "/") + "/models"
		u.RawPath, u.RawQuery, u.Fragment = "", "", ""
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return err
		}
		if cfg.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
		}
		client := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
		resp, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// A configured engine that no longer accepts TCP connections has no
			// live process whose in-memory or disk cache we could disrupt.
			continue
		}
		resp.Body.Close()
		return errors.New("vLLM 仍在运行；请先停止引擎，再清空或更改其 KV Cache")
	}
	return nil
}

// Clear waits for active slot leases, best-effort erases configured engine
// slots, then removes only Aide-owned manifest and slot files.
func (m *kvCacheManager) Clear(ctx context.Context, engines []Settings) error {
	m.mu.Lock()
	cfg := m.cfg
	m.mu.Unlock()
	if cfg.Enabled && cfg.VLLM {
		if err := ensureVLLMStopped(ctx, engines); err != nil {
			return err
		}
	}
	m.mu.Lock()
	keys := make([]string, 0, len(m.slots))
	for key := range m.slots {
		keys = append(keys, key)
	}
	m.mu.Unlock()
	sort.Strings(keys)
	for _, key := range keys {
		m.mu.Lock()
		slot := m.slots[key]
		m.mu.Unlock()
		if slot == nil {
			continue
		}
		if err := slot.acquire(ctx); err != nil {
			return err
		}
		defer slot.release()
	}
	var actionErrors []error
	if cfg.Enabled && cfg.LlamaCpp {
		for _, engineCfg := range engines {
			if kvCacheEngineForConfig(engineCfg) != "llamacpp" {
				continue
			}
			if _, err := llamaSlotAction(ctx, engineCfg, 0, "erase", ""); err != nil {
				actionErrors = append(actionErrors, err)
			}
		}
	}
	m.mu.Lock()
	if len(actionErrors) > 0 {
		m.setErrorLocked(fmt.Errorf("引擎槽位清理未全部确认；下次使用仍会先擦除: %w", errors.Join(actionErrors...)))
	}
	for key := range m.entries {
		m.removeEntryLocked(key)
	}
	m.generation++
	m.stats.Generation = m.generation
	err := m.saveManifestLocked()
	if cfg.Enabled && cfg.Backend == "disk" && cfg.VLLM {
		if removeErr := os.RemoveAll(kvCacheVLLMDir(cfg)); removeErr != nil {
			err = errors.Join(err, removeErr)
		} else if prepareErr := prepareKVCacheDiskPath(kvCacheVLLMDir(cfg)); prepareErr != nil {
			err = errors.Join(err, prepareErr)
		}
	}
	m.setErrorLocked(err)
	m.mu.Unlock()
	return err
}

func (m *kvCacheManager) Configure(ctx context.Context, cfg KVCacheSettings, engines []Settings) error {
	next, err := normalizeKVCacheSettings(m.dataPath, cfg)
	if err != nil {
		return err
	}
	m.mu.Lock()
	previous := m.cfg
	m.mu.Unlock()
	rollback := func(cause error) error {
		m.mu.Lock()
		m.cfg = previous
		m.entries = map[string]*kvCacheEntry{}
		m.generation++
		m.stats.Generation = m.generation
		m.mu.Unlock()
		errs := []error{cause}
		if previous.Enabled && previous.Backend == "disk" {
			paths := []string{previous.DiskPath}
			if previous.LlamaCpp {
				paths = append(paths, kvCacheLlamaDir(previous))
			}
			if previous.VLLM {
				paths = append(paths, kvCacheVLLMDir(previous))
			}
			for _, path := range paths {
				if err := prepareKVCacheDiskPath(path); err != nil {
					errs = append(errs, err)
				}
			}
			if err := m.loadManifest(); err != nil {
				errs = append(errs, err)
			}
		}
		if err := writeLMCacheConfig(m.dataPath, previous); err != nil {
			errs = append(errs, err)
		}
		err := errors.Join(errs...)
		m.mu.Lock()
		m.setErrorLocked(err)
		m.mu.Unlock()
		return err
	}
	if next.Enabled && next.Backend == "disk" {
		if err := prepareKVCacheDiskPath(next.DiskPath); err != nil {
			return err
		}
		if next.LlamaCpp {
			if err := prepareKVCacheDiskPath(kvCacheLlamaDir(next)); err != nil {
				return err
			}
		}
		if next.VLLM {
			if err := prepareKVCacheDiskPath(kvCacheVLLMDir(next)); err != nil {
				return err
			}
		}
	}
	if err := m.Clear(ctx, engines); err != nil {
		return fmt.Errorf("清理上一代 KV Cache 未完成: %w", err)
	}
	m.mu.Lock()
	m.cfg = next
	m.generation++
	m.stats.Generation = m.generation
	m.stats.LastError = ""
	m.entries = map[string]*kvCacheEntry{}
	m.mu.Unlock()
	if next.Enabled && next.Backend == "disk" {
		if err := m.loadManifest(); err != nil {
			return rollback(err)
		}
	}
	if err := writeLMCacheConfig(m.dataPath, next); err != nil {
		return rollback(err)
	}
	return nil
}

func (m *kvCacheManager) ObserveUsage(usage TokenUsage) {
	if usage.CacheHit <= 0 {
		return
	}
	m.mu.Lock()
	if m.cfg.Enabled {
		m.stats.CachedTokens += uint64(usage.CacheHit)
	}
	m.mu.Unlock()
}

func (m *kvCacheManager) Stats() kvCacheStats {
	m.mu.Lock()
	defer m.mu.Unlock()
	stats := m.stats
	stats.CapacityBytes = m.capacityBytes()
	stats.ManagedBytes = m.managedBytesLocked()
	stats.UsedBytes = stats.ManagedBytes
	if m.cfg.Enabled && m.cfg.Backend == "disk" {
		observed, err := kvCacheObservedDiskUsage(m.cfg.DiskPath)
		if err != nil {
			m.setErrorLocked(err)
			stats.LastError = err.Error()
		} else {
			stats.UsedBytes = observed
		}
	}
	stats.EntryCount = len(m.entries)
	stats.Generation = m.generation
	return stats
}

func writeLMCacheConfig(data string, cfg KVCacheSettings) error {
	if err := os.MkdirAll(ConfigDir(data), 0o700); err != nil {
		return err
	}
	path := filepath.Join(ConfigDir(data), "lmcache.yaml")
	var b strings.Builder
	b.WriteString("# Generated by Aide experimental KV Cache. Fixed budget; do not edit while engines are running.\n")
	b.WriteString("chunk_size: 256\ncache_policy: LRU\nsave_decode_cache: false\nenable_lazy_memory_allocator: false\nrun_script_api_enabled: false\ninternal_api_server_enabled: false\n")
	if !cfg.Enabled || !cfg.VLLM {
		b.WriteString("local_cpu: false\nlocal_disk: null\n")
		return atomicJSONBytes(path, []byte(b.String()))
	}
	budgetMiB := kvCacheEngineBudgetMiB(cfg, "vllm")
	if cfg.Backend == "memory" {
		b.WriteString("local_cpu: true\nlocal_disk: null\nmax_local_cpu_size: ")
		b.WriteString(strconv.FormatFloat(float64(budgetMiB)/1024, 'f', 6, 64))
		b.WriteString("\nreserve_local_cpu_size: 0.0\n")
	} else {
		b.WriteString("local_cpu: false\nlocal_disk: ")
		quoted, _ := json.Marshal(kvCacheVLLMDir(cfg))
		b.Write(quoted)
		b.WriteString("\nmax_local_disk_size: ")
		b.WriteString(strconv.FormatFloat(float64(budgetMiB)/1024, 'f', 6, 64))
		b.WriteString("\n")
	}
	return atomicJSONBytes(path, []byte(b.String()))
}

func llamaLaunchArgs(cfg KVCacheSettings) []string {
	if cfg.Backend == "memory" {
		return []string{"--cache-ram", strconv.FormatInt(kvCacheEngineBudgetMiB(cfg, "llamacpp"), 10)}
	}
	return []string{"--slot-save-path", kvCacheLlamaDir(cfg)}
}

func (a *App) kvCacheStatusLocked() map[string]any {
	cfg := a.settings.KVCache
	status := map[string]any{
		"config": cfg, "stats": a.kvCache.Stats(), "experimental": true,
		"fixedCapacity": true, "engineRestartRequired": cfg.Enabled && (cfg.LlamaCpp || cfg.VLLM),
		"llamaLaunchArgs":    llamaLaunchArgs(cfg),
		"lmcacheConfigPath":  filepath.Join(ConfigDir(a.dataPath), "lmcache.yaml"),
		"lmcacheEnvironment": map[string]string{"LMCACHE_CONFIG_FILE": filepath.Join(ConfigDir(a.dataPath), "lmcache.yaml")},
		"notes": []string{
			"云端前缀缓存只保存哈希键，不保存提示词或答案；节省量以提供商 cached_tokens 为准。",
			"固定总预算在同时启用的真实 KV 引擎间均分；llama.cpp 与 LMCache 使用独立子目录，磁盘统计包含二者文件。",
			"llama.cpp 磁盘模式要求 AIDE R&D 与引擎共享同一固定目录；内存模式需在引擎启动参数中设置同等固定 --cache-ram。",
			"TTL 仅管理 AIDE R&D 的 llama.cpp 条目；LMCache 内部 LRU 由引擎执行，清理或变更 vLLM 缓存前必须停止引擎。",
			"vLLM 需以生成的 LMCACHE_CONFIG_FILE 和 LMCacheConnectorV1 重启；AIDE R&D 不把配置写入成功计作 KV 命中。",
		},
	}
	return status
}

func (a *App) kvCacheAPI(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.mu.Lock()
		status := a.kvCacheStatusLocked()
		a.mu.Unlock()
		jsonOut(w, 200, status)
	case http.MethodPut:
		var cfg KVCacheSettings
		if err := decode(w, r, &cfg); err != nil {
			fail(w, 400, err)
			return
		}
		a.mu.Lock()
		engines := a.kvCacheEngineConfigsLocked()
		a.mu.Unlock()
		normalized, err := normalizeKVCacheSettings(a.dataPath, cfg)
		if err != nil {
			fail(w, 400, err)
			return
		}
		if err := a.kvCache.Configure(r.Context(), normalized, engines); err != nil {
			fail(w, 400, err)
			return
		}
		a.mu.Lock()
		previous := a.settings.KVCache
		a.settings.KVCache = normalized
		if err := atomicJSON(SettingsPath(a.dataPath), a.settings); err != nil {
			a.settings.KVCache = previous
			a.mu.Unlock()
			rollbackCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			rollbackErr := a.kvCache.Configure(rollbackCtx, previous, engines)
			cancel()
			if rollbackErr != nil {
				err = fmt.Errorf("%w；运行时缓存配置回滚失败: %v", err, rollbackErr)
			}
			fail(w, 500, err)
			return
		}
		status := a.kvCacheStatusLocked()
		a.mu.Unlock()
		jsonOut(w, 200, status)
	case http.MethodDelete:
		a.mu.Lock()
		engines := a.kvCacheEngineConfigsLocked()
		a.mu.Unlock()
		if err := a.kvCache.Clear(r.Context(), engines); err != nil {
			fail(w, 409, err)
			return
		}
		a.mu.Lock()
		status := a.kvCacheStatusLocked()
		a.mu.Unlock()
		jsonOut(w, 200, status)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func kvCacheAdapterSignature(providers []ModelProvider) string {
	parts := make([]string, 0, len(providers))
	for _, provider := range providers {
		parts = append(parts, provider.ID+"\x00"+provider.KVCacheEngine)
	}
	sort.Strings(parts)
	return strings.Join(parts, "\x01")
}

func (a *App) kvCacheEngineConfigsForSettingsLocked(settings Settings) []Settings {
	out := make([]Settings, 0, len(settings.ModelProviders))
	for _, provider := range settings.ModelProviders {
		if provider.KVCacheEngine != "llamacpp" && provider.KVCacheEngine != "vllm" {
			continue
		}
		cfg := settings
		cfg.BaseURL = provider.BaseURL
		cfg.APIKey, _ = a.vaultKeyByIDLocked(modelProviderVaultID(provider.ID, provider.ActiveKeyID))
		out = append(out, cfg)
	}
	return out
}

func (a *App) kvCacheEngineConfigsLocked() []Settings {
	return a.kvCacheEngineConfigsForSettingsLocked(a.settings)
}
