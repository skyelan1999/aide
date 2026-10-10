package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type knowledgeEmbeddingPolicy struct {
	Enabled      bool    `json:"enabled"`
	BaseURL      string  `json:"baseURL"`
	Model        string  `json:"model"`
	VectorWeight float64 `json:"vectorWeight"`
	Generation   uint64  `json:"generation"`
}

func (p knowledgeEmbeddingPolicy) validate() error {
	if math.IsNaN(p.VectorWeight) || math.IsInf(p.VectorWeight, 0) || p.VectorWeight < 0 || p.VectorWeight > 1 || len(p.Model) > 128 || len(p.BaseURL) > 2048 {
		return errors.New("embedding 配置超出范围")
	}
	if p.Enabled || p.BaseURL != "" {
		u, err := url.Parse(p.BaseURL)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("embedding 地址须为无凭据的 HTTP(S) Base URL")
		}
	}
	if p.Enabled && strings.TrimSpace(p.Model) == "" {
		return errors.New("启用向量检索需要模型名称")
	}
	return nil
}
func (a *App) embeddingPolicyPath() string {
	return filepath.Join(a.dataPath, "config", "knowledge-embedding", hash([]byte(a.wsID()))+".json")
}
func (a *App) embeddingVaultID() string { return "knowledge:embedding:" + hash([]byte(a.wsID())) }

// Caller holds a.mu; default is opt-out and contains no endpoint or key.
func (a *App) loadEmbeddingPolicy() (knowledgeEmbeddingPolicy, error) {
	p := knowledgeEmbeddingPolicy{VectorWeight: .5}
	b, err := os.ReadFile(a.embeddingPolicyPath())
	if os.IsNotExist(err) {
		return p, nil
	}
	if err != nil || len(b) > 8192 {
		return p, errors.New("embedding 配置读取失败")
	}
	if json.Unmarshal(b, &p) != nil {
		return p, errors.New("embedding 配置解析失败")
	}
	return p, p.validate()
}
func embeddingRevision(p knowledgeEmbeddingPolicy) string { b, _ := json.Marshal(p); return hash(b) }
func (a *App) embeddingPolicyResponse(p knowledgeEmbeddingPolicy) map[string]any {
	return map[string]any{"policy": p, "workspaceId": a.wsID(), "revision": embeddingRevision(p), "hasKey": a.vault != nil && a.vault.Has(a.embeddingVaultID())}
}
func (a *App) getEmbeddingPolicy(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	p, err := a.loadEmbeddingPolicy()
	if err != nil {
		fail(w, 500, err)
		return
	}
	jsonOut(w, 200, a.embeddingPolicyResponse(p))
}
func (a *App) putEmbeddingPolicy(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Policy      knowledgeEmbeddingPolicy `json:"policy"`
		WorkspaceID string                   `json:"workspaceId"`
		Revision    string                   `json:"revision"`
		APIKey      string                   `json:"apiKey"`
		ClearKey    bool                     `json:"clearKey"`
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
	d.DisallowUnknownFields()
	if d.Decode(&in) != nil {
		fail(w, 400, errors.New("embedding 配置请求无效"))
		return
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		fail(w, 400, errors.New("配置必须为单一JSON对象"))
		return
	}
	in.Policy.BaseURL = strings.TrimSpace(in.Policy.BaseURL)
	in.Policy.Model = strings.TrimSpace(in.Policy.Model)
	if err := in.Policy.validate(); err != nil {
		fail(w, 400, err)
		return
	}
	if len(in.APIKey) > 8192 || in.APIKey != "" && in.ClearKey {
		fail(w, 400, errors.New("密钥操作无效"))
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	old, err := a.loadEmbeddingPolicy()
	if err != nil {
		fail(w, 500, err)
		return
	}
	if in.WorkspaceID != a.wsID() || in.Revision != embeddingRevision(old) {
		fail(w, 409, errors.New("工作区或检索配置已变化，请重新加载"))
		return
	}
	in.Policy.Generation = old.Generation + 1
	keyChange := in.APIKey != "" || in.ClearKey
	var previous []byte
	id := a.embeddingVaultID()
	restoreKey := func() error {
		if previous == nil {
			a.vault.Delete(id)
		} else if err := a.vault.Put(id, VaultTypeModelAPIKey, "知识向量检索", previous, ""); err != nil {
			return err
		}
		return a.vault.Save()
	}
	if keyChange {
		if !a.vaultIsUnlocked() {
			fail(w, 400, errVaultLocked)
			return
		}
		if a.vault.Has(id) {
			previous, err = a.vault.Get(id)
			if err != nil {
				fail(w, 500, errors.New("embedding 密钥读取失败"))
				return
			}
		}
		if in.ClearKey {
			a.vault.Delete(id)
		} else {
			err = a.vault.Put(id, VaultTypeModelAPIKey, "知识向量检索", []byte(in.APIKey), "")
		}
		if err == nil {
			err = a.vault.Save()
		}
		if err != nil {
			if restoreKey() != nil {
				fail(w, 500, errors.New("embedding 密钥保存与恢复失败，请检查保险库状态"))
				return
			}
			fail(w, 500, errors.New("embedding 密钥保存失败"))
			return
		}
	}
	path := a.embeddingPolicyPath()
	err = os.MkdirAll(filepath.Dir(path), 0700)
	if err == nil {
		err = atomicJSON(path, in.Policy)
	}
	if err != nil {
		if keyChange {
			if restoreKey() != nil {
				fail(w, 500, errors.New("embedding 配置保存与密钥恢复失败，请检查保险库状态"))
				return
			}
		}
		fail(w, 500, errors.New("embedding 配置保存失败"))
		return
	}
	a.wsRevision++
	jsonOut(w, 200, a.embeddingPolicyResponse(in.Policy))
}

// Called only for RAG, after source extraction and access checks. Disabled
// configuration and provider failures keep lexical evidence with diagnostics.
func (a *App) hybridDocumentHits(ctx context.Context, workspace, query string, chunks []documentHit) ([]documentHit, string, string, error) {
	keyword := documentKeywordRanks(chunks, query)
	lexical := make([]documentHit, 0, min(12, len(keyword)))
	for _, rank := range keyword {
		hit := chunks[rank.Index]
		hit.Score = rank.Score
		lexical = append(lexical, hit)
		if len(lexical) == 12 {
			break
		}
	}
	engine := "local-tfidf-retrieval; no embeddings"
	a.mu.Lock()
	if knowledgeID(a.wsID(), "scope", "", ".") != workspace {
		a.mu.Unlock()
		return nil, "", "", errors.New("工作区已切换，请刷新")
	}
	p, err := a.loadEmbeddingPolicy()
	epoch := a.wsRevision
	if a.knowledgeVectors == nil {
		a.knowledgeVectors = &knowledgeVectorCache{}
	}
	cache := a.knowledgeVectors
	cacheDir := filepath.Join(a.dataPath, "cache", "knowledge-vectors")
	cacheKey := knowledgeVectorEncryptionKey(a.accessTokenVaultKey())
	defer zeroBytes(cacheKey)
	provider := knowledgeEmbeddingProvider{BaseURL: p.BaseURL, Model: p.Model}
	if err == nil && p.Enabled && a.vault != nil && a.vault.Has(a.embeddingVaultID()) {
		var key []byte
		key, err = a.vault.Get(a.embeddingVaultID())
		provider.APIKey = string(key)
	}
	a.mu.Unlock()
	if err != nil {
		return lexical, engine, "向量配置或密钥不可用，已回退关键词检索", nil
	}
	if !p.Enabled || p.VectorWeight == 0 || len(chunks) == 0 {
		return lexical, engine, "", nil
	}
	namespace := knowledgeVectorNamespace(workspace, p)
	ids := make([]string, len(chunks))
	for i, chunk := range chunks {
		ids[i] = knowledgeVectorChunkKey(chunk)
	}
	cached, cacheWarning := cache.lookup(namespace, cacheDir, cacheKey, ids)
	texts := make([]string, 1, len(chunks)+1)
	texts[0] = query
	missing := []int{}
	for i, chunk := range chunks {
		if _, exists := cached[i]; !exists {
			texts = append(texts, chunk.Text)
			missing = append(missing, i)
		}
	}
	vectors, err := knowledgeEmbeddings(ctx, provider, texts)
	if ctx.Err() != nil {
		return nil, "", "", ctx.Err()
	}
	a.mu.Lock()
	stale := knowledgeID(a.wsID(), "scope", "", ".") != workspace || a.wsRevision != epoch
	a.mu.Unlock()
	if stale {
		return nil, "", "", errors.New("工作区或检索配置已变化，请重试")
	}
	if err != nil {
		return lexical, engine, "向量服务不可用，已回退关键词检索：" + err.Error(), nil
	}
	docVectors := make([][]float64, len(chunks))
	for i, v := range cached {
		docVectors[i] = v
	}
	fresh := map[string][]float64{}
	for j, i := range missing {
		docVectors[i] = vectors[j+1]
		fresh[ids[i]] = vectors[j+1]
	}
	ranks, err := documentVectorRanks(vectors[0], docVectors, len(chunks))
	if err != nil {
		cache.invalidate(namespace, cacheDir)
		return lexical, engine, "向量响应无效，已回退关键词检索", nil
	}
	hits, err := documentFuseRanks(chunks, keyword, ranks, p.VectorWeight, 12)
	if err != nil {
		return nil, "", "", fmt.Errorf("混合检索融合失败")
	}
	a.mu.Lock()
	if knowledgeID(a.wsID(), "scope", "", ".") != workspace || a.wsRevision != epoch {
		a.mu.Unlock()
		return nil, "", "", errors.New("工作区或检索配置已变化，请重试")
	}
	if len(fresh) > 0 {
		if err := cache.store(namespace, cacheDir, cacheKey, fresh); err != nil {
			cacheWarning = "向量缓存保存失败；本次检索结果可用"
		}
	}
	a.mu.Unlock()
	return hits, "weighted-rrf; local-tfidf + provider-embeddings", cacheWarning, nil
}
