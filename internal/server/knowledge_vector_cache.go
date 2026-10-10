package server

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const knowledgeVectorBytes = 4 << 20

type knowledgeVectorCache struct {
	mu        sync.Mutex
	namespace string
	rows      map[string][]float64
	order     []string
	bytes     int
}
type knowledgeVectorEnvelope struct {
	Version    int    `json:"version"`
	Ciphertext []byte `json:"ciphertext"`
	Nonce      []byte `json:"nonce"`
}
type knowledgeVectorPayload struct {
	Namespace string               `json:"namespace"`
	Rows      map[string][]float64 `json:"rows"`
	Order     []string             `json:"order"`
}

func knowledgeVectorNamespace(workspace string, p knowledgeEmbeddingPolicy) string {
	b, _ := json.Marshal([]any{"embedding-cache-v1", workspace, p.BaseURL, p.Model, p.Generation})
	return hash(b)
}
func knowledgeVectorChunkKey(c documentHit) string {
	b, _ := json.Marshal([]any{c.ID, c.Hash, c.File.Origin, c.File.Source, c.File.Root, c.Locator, c.Offset, hash([]byte(c.Text))})
	return hash(b)
}
func knowledgeVectorEncryptionKey(accessKey []byte) []byte {
	key := sha256.Sum256(append(accessKey, []byte("aide-knowledge-vector-cache-v1")...))
	return key[:]
}
func (c *knowledgeVectorCache) reset(namespace string) {
	c.namespace = namespace
	c.rows = map[string][]float64{}
	c.order = nil
	c.bytes = 0
}
func (c *knowledgeVectorCache) invalidate(namespace, dir string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.namespace == namespace {
		c.reset(namespace)
	}
	_ = os.Remove(filepath.Join(dir, namespace+".vec")) // Regenerable, identity-hashed cache only.
}
func (c *knowledgeVectorCache) loadLocked(namespace, dir string, key []byte) error {
	if c.namespace == namespace {
		return nil
	}
	c.reset(namespace)
	f, err := os.Open(filepath.Join(dir, namespace+".vec"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return errors.New("向量缓存读取失败")
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, (24<<20)+1))
	if err != nil || len(b) > 24<<20 {
		return errors.New("向量缓存超过上限")
	}
	var envelope knowledgeVectorEnvelope
	if json.Unmarshal(b, &envelope) != nil || envelope.Version != 1 {
		return errors.New("向量缓存格式无效")
	}
	plain, err := openWithKey(key, envelope.Ciphertext, envelope.Nonce)
	if err != nil {
		return errors.New("向量缓存解密失败")
	}
	defer zeroBytes(plain)
	var payload knowledgeVectorPayload
	if json.Unmarshal(plain, &payload) != nil || payload.Namespace != namespace || len(payload.Rows) > 4096 || len(payload.Order) != len(payload.Rows) {
		return errors.New("向量缓存身份或数量无效")
	}
	bytes := 0
	seen := map[string]bool{}
	for _, id := range payload.Order {
		v, ok := payload.Rows[id]
		if !ok || len(id) != 64 || seen[id] {
			return errors.New("向量缓存索引无效")
		}
		seen[id] = true
		if _, err := documentUnitVector(v, len(v)); err != nil {
			return errors.New("向量缓存数值无效")
		}
		bytes += len(v)*8 + len(id)
		if bytes > knowledgeVectorBytes {
			return errors.New("向量缓存超过内存上限")
		}
	}
	c.rows = payload.Rows
	c.order = payload.Order
	c.bytes = bytes
	return nil
}

// Cache hits supply vectors only; callers always re-read authorized source bytes.
func (c *knowledgeVectorCache) lookup(namespace, dir string, key []byte, ids []string) (map[int][]float64, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	warning := ""
	if err := c.loadLocked(namespace, dir, key); err != nil {
		warning = err.Error() + "，重新生成"
	}
	out := map[int][]float64{}
	for i, id := range ids {
		if v, ok := c.rows[id]; ok {
			out[i] = append([]float64(nil), v...)
		}
	}
	return out, warning
}
func (c *knowledgeVectorCache) store(namespace, dir string, key []byte, rows map[string][]float64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.loadLocked(namespace, dir, key); err != nil {
		c.reset(namespace)
	}
	ids := make([]string, 0, len(rows))
	for id := range rows {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		v := rows[id]
		if _, err := documentUnitVector(v, len(v)); err != nil {
			return err
		}
		if _, exists := c.rows[id]; exists {
			continue
		}
		c.rows[id] = append([]float64(nil), v...)
		c.order = append(c.order, id)
		c.bytes += len(v)*8 + len(id)
		for c.bytes > knowledgeVectorBytes || len(c.order) > 4096 {
			old := c.order[0]
			c.order = c.order[1:]
			c.bytes -= len(c.rows[old])*8 + len(old)
			delete(c.rows, old)
		}
	}
	plain, err := json.Marshal(knowledgeVectorPayload{namespace, c.rows, c.order})
	if err != nil {
		return err
	}
	defer zeroBytes(plain)
	ct, nonce, err := sealWithKey(key, plain)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err = atomicJSON(filepath.Join(dir, namespace+".vec"), knowledgeVectorEnvelope{1, ct, nonce}); err != nil {
		return err
	}
	// Only this cache's SHA-256 .vec files are regenerable; leave other files alone.
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	type file struct {
		name  string
		stamp int64
	}
	files := []file{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || len(name) != 68 || !strings.HasSuffix(name, ".vec") {
			continue
		}
		valid := true
		for _, r := range strings.TrimSuffix(name, ".vec") {
			if !strings.ContainsRune("0123456789abcdef", r) {
				valid = false
				break
			}
		}
		if !valid {
			continue
		}
		if info, e := entry.Info(); e == nil {
			files = append(files, file{name, info.ModTime().UnixNano()})
		}
	}
	sort.Slice(files, func(i, j int) bool {
		currentI, currentJ := files[i].name == namespace+".vec", files[j].name == namespace+".vec"
		if currentI != currentJ {
			return currentI
		}
		if files[i].stamp == files[j].stamp {
			return files[i].name < files[j].name
		}
		return files[i].stamp > files[j].stamp
	})
	for _, file := range files[min(8, len(files)):] {
		if err = os.Remove(filepath.Join(dir, file.name)); err != nil {
			return err
		}
	}
	return nil
}
