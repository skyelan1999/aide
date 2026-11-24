package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type kvCacheLease struct {
	manager    *kvCacheManager
	cfg        Settings
	key        string
	slot       *kvCacheSlot
	chain      []string
	generation uint64
	finished   bool
}

func (l *kvCacheLease) finish(success bool, text string, calls []ToolCall) {
	if l == nil || l.finished || l.manager == nil {
		return
	}
	l.finished = true
	if l.slot == nil {
		return
	}
	defer l.slot.release()
	defer l.manager.leaseWG.Done()
	defer func() {
		l.manager.mu.Lock()
		l.manager.stats.ActiveLeases--
		l.manager.mu.Unlock()
	}()
	if !success {
		l.invalidate()
		return
	}
	assistant := Message{Role: "assistant", Content: text, ToolCalls: calls}
	l.chain = appendKVChainMessage(l.chain, assistant)
	if len(l.chain) > kvCacheMaxChainMessages {
		l.invalidate()
		return
	}
	ctx, cancel := context.WithTimeout(l.manager.ctx, 30*time.Second)
	defer cancel()
	if l.cfg.KVCache.Backend == "disk" {
		filename := kvCacheFilename(l.key)
		result, err := llamaSlotAction(ctx, l.cfg, 0, "save", filename)
		if err != nil {
			l.recordError(err)
			l.invalidate()
			return
		}
		path := filepath.Join(kvCacheLlamaDir(l.cfg.KVCache), filename)
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			l.recordError(errors.New("llama.cpp 已返回保存成功，但固定共享目录中没有有效槽位文件"))
			l.invalidate()
			return
		}
		if err := l.store(filename, info.Size(), result); err != nil {
			l.recordError(err)
			l.invalidate()
			return
		}
		// Disk mode releases engine RAM after a successful durable save. The next
		// matching prefix restores the file before sending the extended prompt.
		if _, err := llamaSlotAction(ctx, l.cfg, 0, "erase", ""); err != nil {
			l.recordError(err)
		}
		return
	}
	if err := l.store("", 0, 0); err != nil {
		l.recordError(err)
		l.invalidate()
	}
}

func (l *kvCacheLease) invalidate() {
	ctx, cancel := context.WithTimeout(l.manager.ctx, 10*time.Second)
	defer cancel()
	_, _ = llamaSlotAction(ctx, l.cfg, 0, "erase", "")
	l.manager.mu.Lock()
	l.manager.removeEntryLocked(l.key)
	_ = l.manager.saveManifestLocked()
	l.manager.mu.Unlock()
}

func (l *kvCacheLease) recordError(err error) {
	l.manager.mu.Lock()
	l.manager.setErrorLocked(err)
	l.manager.mu.Unlock()
}

func (l *kvCacheLease) store(filename string, size int64, savedTokens int) error {
	m := l.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	if l.generation != m.generation || m.closed || !m.cfg.Enabled {
		return errors.New("KV Cache 配置已变化，当前结果不写入缓存")
	}
	now := m.now()
	entry := &kvCacheEntry{
		Key: l.key, Provider: routerProviderID(l.cfg), Model: l.cfg.Model, Slot: 0,
		File: filename, Size: size, Chain: append([]string(nil), l.chain...),
		UpdatedAt: now, ExpiresAt: now.Add(time.Duration(m.cfg.TTLMinutes) * time.Minute),
	}
	m.entries[l.key] = entry
	m.evictOverflowLocked(map[string]bool{l.key: true})
	if m.managedBytesLocked() > m.llamaBudgetBytes() {
		m.removeEntryLocked(l.key)
		m.stats.OverflowRollbacks++
		_ = m.saveManifestLocked()
		return errors.New("KV Cache 写入超过固定容量，已回滚新缓存；模型回答不受影响")
	}
	if err := m.saveManifestLocked(); err != nil {
		m.removeEntryLocked(l.key)
		return err
	}
	m.stats.Saves++
	if savedTokens > 0 {
		m.stats.SavedTokens += uint64(savedTokens)
	}
	return nil
}

func (m *kvCacheManager) prepareLlama(ctx context.Context, cfg Settings, messages []Message, body map[string]any) (*kvCacheLease, error) {
	chain := kvRequestChain(cfg, messages, body)
	providerID := routerProviderID(cfg)
	identity := providerID + "\x00" + cfg.BaseURL + "\x00" + cfg.Model + "\x000"
	digest := sha256.Sum256([]byte(identity))
	key := "slot-" + hex.EncodeToString(digest[:])
	m.mu.Lock()
	if m.closed || !m.cfg.Enabled {
		m.mu.Unlock()
		return &kvCacheLease{}, nil
	}
	slot, err := m.slotLocked(key)
	generation := m.generation
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if err := slot.acquire(ctx); err != nil {
		return nil, err
	}
	m.mu.Lock()
	if m.closed || !m.cfg.Enabled || generation != m.generation {
		m.mu.Unlock()
		slot.release()
		return &kvCacheLease{}, nil
	}
	m.leaseWG.Add(1)
	m.mu.Unlock()
	lease := &kvCacheLease{manager: m, cfg: cfg, key: key, slot: slot, chain: chain, generation: generation}
	m.mu.Lock()
	m.stats.ActiveLeases++
	entry := m.entries[key]
	if entry != nil && !kvChainPrefix(entry.Chain, chain) {
		m.removeEntryLocked(key)
		entry = nil
		_ = m.saveManifestLocked()
	}
	m.mu.Unlock()
	if len(chain) == 0 || len(chain) > kvCacheMaxChainMessages {
		lease.invalidate()
		lease.finished = true
		lease.slot = nil
		slot.release()
		m.leaseWG.Done()
		m.mu.Lock()
		m.stats.ActiveLeases--
		m.mu.Unlock()
		return &kvCacheLease{}, nil
	}
	if entry == nil && cfg.KVCache.Backend == "disk" {
		// Disk restore needs an unambiguous destination slot. In memory mode,
		// cache_prompt already finds the longest valid prefix; erasing here would
		// destroy that reusable RAM state when runtime-only message text changes.
		if _, err := llamaSlotAction(ctx, cfg, 0, "erase", ""); err != nil {
			lease.recordError(fmt.Errorf("llama.cpp 槽位清理失败，将按普通请求继续: %w", err))
		}
	} else if entry != nil && cfg.KVCache.Backend == "disk" {
		result, err := llamaSlotAction(ctx, cfg, 0, "restore", entry.File)
		if err != nil {
			lease.recordError(fmt.Errorf("llama.cpp KV 恢复失败，已废弃该条目: %w", err))
			lease.invalidate()
		} else {
			m.mu.Lock()
			entry.UpdatedAt = m.now()
			entry.ExpiresAt = entry.UpdatedAt.Add(time.Duration(m.cfg.TTLMinutes) * time.Minute)
			m.stats.Restores++
			if result > 0 {
				m.stats.RestoredTokens += uint64(result)
			}
			_ = m.saveManifestLocked()
			m.mu.Unlock()
		}
	}
	body["id_slot"] = 0
	body["cache_prompt"] = true
	return lease, nil
}

type kvCacheBypassKey struct{}

// withoutKVCache marks an auxiliary model call that must not replace or erase
// the main conversation's engine-local KV state.
func withoutKVCache(ctx context.Context) context.Context {
	return context.WithValue(ctx, kvCacheBypassKey{}, true)
}

// prepareKVCacheRequest is called only after the provider body has been fully
// built. Disabled, bypassed or unadapted providers return without changing the body.
func prepareKVCacheRequest(ctx context.Context, cfg Settings, messages []Message, body map[string]any) (*kvCacheLease, error) {
	m := cfg.kvCache
	if m == nil {
		return &kvCacheLease{}, nil
	}
	m.mu.Lock()
	enabled := m.cfg.Enabled && !m.closed
	m.mu.Unlock()
	if !enabled {
		return &kvCacheLease{}, nil
	}
	engine := kvCacheEngineForConfig(cfg)
	if bypass, _ := ctx.Value(kvCacheBypassKey{}).(bool); bypass {
		if engine == "llamacpp" && cfg.KVCache.LlamaCpp {
			// The auxiliary call may share the engine slot and its RAM cache, but
			// it must not invalidate the main conversation's Aide-side chain.
			body["id_slot"] = 0
			body["cache_prompt"] = true
		}
		return &kvCacheLease{}, nil
	}
	if cfg.KVCache.CloudPrefix && (engine == "openai" || (engine == "" && isOpenAIEndpoint(cfg.BaseURL))) {
		body["prompt_cache_key"] = kvCloudPrefixKey(cfg, messages, body)
		m.mu.Lock()
		m.stats.PrefixRequests++
		m.mu.Unlock()
	}
	if engine == "llamacpp" && cfg.KVCache.LlamaCpp {
		lease, err := m.prepareLlama(ctx, cfg, messages, body)
		if err == nil {
			return lease, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		m.mu.Lock()
		m.setErrorLocked(err)
		m.mu.Unlock()
		return &kvCacheLease{}, nil
	}
	return &kvCacheLease{}, nil
}

func kvCacheEngineForConfig(cfg Settings) string {
	providerID := routerProviderID(cfg)
	for _, provider := range cfg.ModelProviders {
		if (providerID != "" && provider.ID == providerID) || (providerID == "" && strings.TrimRight(provider.BaseURL, "/") == strings.TrimRight(cfg.BaseURL, "/")) {
			if provider.KVCacheEngine == "off" {
				return "off"
			}
			return provider.KVCacheEngine
		}
	}
	return ""
}

func kvBaseFingerprint(cfg Settings, body map[string]any) [32]byte {
	base := make(map[string]any, len(body))
	for key, value := range body {
		switch key {
		case "messages", "input", "stream", "stream_options", "id_slot", "cache_prompt", "prompt_cache_key":
			continue
		}
		base[key] = value
	}
	identity := struct {
		Provider string
		BaseURL  string
		Model    string
		Body     map[string]any
	}{routerProviderID(cfg), cfg.BaseURL, cfg.Model, base}
	b, _ := json.Marshal(identity)
	return sha256.Sum256(b)
}

func kvRequestChain(cfg Settings, messages []Message, body map[string]any) []string {
	previous := kvBaseFingerprint(cfg, body)
	chain := make([]string, 0, len(messages))
	for _, message := range messages {
		b, _ := json.Marshal(message)
		digest := sha256.New()
		_, _ = digest.Write(previous[:])
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(b)))
		_, _ = digest.Write(size[:])
		_, _ = digest.Write(b)
		copy(previous[:], digest.Sum(nil))
		chain = append(chain, hex.EncodeToString(previous[:]))
	}
	return chain
}

func appendKVChainMessage(chain []string, message Message) []string {
	if len(chain) == 0 {
		return chain
	}
	previous, err := hex.DecodeString(chain[len(chain)-1])
	if err != nil {
		return chain
	}
	b, _ := json.Marshal(message)
	digest := sha256.New()
	_, _ = digest.Write(previous)
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(b)))
	_, _ = digest.Write(size[:])
	_, _ = digest.Write(b)
	return append(chain, hex.EncodeToString(digest.Sum(nil)))
}

func kvChainPrefix(previous, next []string) bool {
	if len(previous) == 0 || len(previous) > len(next) {
		return false
	}
	for i := range previous {
		if previous[i] != next[i] {
			return false
		}
	}
	return true
}

func kvCloudPrefixKey(cfg Settings, messages []Message, body map[string]any) string {
	chain := kvRequestChain(cfg, messages, body)
	if len(chain) == 0 {
		digest := kvBaseFingerprint(cfg, body)
		return "aide-" + hex.EncodeToString(digest[:])
	}
	if len(chain) > 2 {
		chain = chain[:2]
	}
	digest := sha256.Sum256([]byte(strings.Join(chain, "\x00")))
	return "aide-" + hex.EncodeToString(digest[:])
}

func kvCacheFilename(key string) string {
	digest := sha256.Sum256([]byte(key))
	return "aide-" + hex.EncodeToString(digest[:]) + ".bin"
}

// llamaSlotAction uses the model endpoint's origin, never a user-supplied slot
// URL. Redirects and response bodies are not followed or logged.
func llamaSlotAction(ctx context.Context, cfg Settings, slot int, action, filename string) (int, error) {
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return 0, errors.New("llama.cpp Base URL 无效")
	}
	u.Path = "/slots/" + strconv.Itoa(slot)
	u.RawPath = ""
	u.Fragment = ""
	query := url.Values{"action": []string{action}}
	if filename != "" {
		if !validKVCacheFilename(filename) {
			return 0, errors.New("llama.cpp 槽位文件名无效")
		}
		query.Set("filename", filename)
	}
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(nil))
	if err != nil {
		return 0, err
	}
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("llama.cpp 槽位 %s 返回 HTTP %d", action, resp.StatusCode)
	}
	var out struct {
		NTokens int `json:"n_tokens"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&out); err != nil {
		return 0, fmt.Errorf("llama.cpp 槽位响应无效: %w", err)
	}
	if out.NTokens < 0 {
		return 0, errors.New("llama.cpp 返回了负的缓存 token 数")
	}
	return out.NTokens, nil
}
