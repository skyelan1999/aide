package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// Index policy changes discovery order and budgets, never source permissions.
type knowledgeIndexPolicy struct {
	LanguageService string              `json:"languageService,omitempty"`
	Version         int                 `json:"version"`
	FileBudget      int                 `json:"fileBudget"`
	DirectoryBudget int                 `json:"directoryBudget"`
	DepthBudget     int                 `json:"depthBudget"`
	PriorityPaths   map[string][]string `json:"priorityPaths"`
	ScopePaths      map[string][]string `json:"scopePaths,omitempty"`
}

func defaultKnowledgeIndexPolicy() knowledgeIndexPolicy {
	return knowledgeIndexPolicy{Version: 1, FileBudget: 800, DirectoryBudget: 200, DepthBudget: 7, PriorityPaths: map[string][]string{}}
}
func (p knowledgeIndexPolicy) validate() error {
	if p.LanguageService != "" && p.LanguageService != "go-types" {
		return fmt.Errorf("不支持的代码语言服务")
	}
	if p.Version != 1 || p.FileBudget < 50 || p.FileBudget > 4000 || p.DirectoryBudget < 20 || p.DirectoryBudget > 1000 || p.DepthBudget < 1 || p.DepthBudget > 12 || len(p.PriorityPaths) > 32 {
		return fmt.Errorf("索引预算超出允许范围")
	}
	for source, paths := range p.PriorityPaths {
		if len(source) == 0 || len(source) > 128 || strings.ContainsAny(source, "\x00\r\n") || len(paths) > 32 {
			return fmt.Errorf("优先来源或路径数量无效")
		}
		seen := map[string]bool{}
		for _, v := range paths {
			if len(v) > 512 || !knowledgeSafePath(v) || v == "." || path.Clean(v) != v || seen[v] {
				return fmt.Errorf("优先路径必须为来源内的唯一相对路径")
			}
			for _, part := range strings.Split(v, "/") {
				if knowledgeSkip(part) {
					return fmt.Errorf("隐藏、依赖或敏感路径不能设为优先索引")
				}
			}
			seen[v] = true
		}
	}
	if len(p.ScopePaths) > 32 {
		return fmt.Errorf("索引范围来源数量无效")
	}
	for source, paths := range p.ScopePaths {
		if len(source) == 0 || len(source) > 128 || strings.ContainsAny(source, "\x00\r\n") || len(paths) > 32 {
			return fmt.Errorf("索引范围来源或路径数量无效")
		}
		seen := map[string]bool{}
		for _, v := range paths {
			if len(v) > 512 || !knowledgeSafePath(v) || v == "." || path.Clean(v) != v || seen[v] {
				return fmt.Errorf("索引范围必须为来源内的唯一相对路径")
			}
			for _, part := range strings.Split(v, "/") {
				if knowledgeSkip(part) {
					return fmt.Errorf("隐藏、依赖或敏感路径不能设为索引范围")
				}
			}
			seen[v] = true
		}
		for _, priority := range p.PriorityPaths[source] {
			if !knowledgeInScope(priority, paths, false) {
				return fmt.Errorf("优先路径必须位于索引范围内")
			}
		}
	}
	return nil
}

// Ancestors may appear as directory metadata, never as admitted file content.
func knowledgeInScope(rel string, scopes []string, ancestors bool) bool {
	if len(scopes) == 0 {
		return true
	}
	for _, scope := range scopes {
		if rel == scope || strings.HasPrefix(rel, scope+"/") || ancestors && (rel == "." || strings.HasPrefix(scope, rel+"/")) {
			return true
		}
	}
	return false
}
func (a *App) knowledgeIndexPolicyPath() string {
	return filepath.Join(a.dataPath, "config", "knowledge-index", hash([]byte(a.wsID()))+".json")
}

// Caller holds a.mu, pinning workspace and configuration during I/O.
func (a *App) loadKnowledgeIndexPolicy() (knowledgeIndexPolicy, error) {
	p := defaultKnowledgeIndexPolicy()
	f, err := os.Open(a.knowledgeIndexPolicyPath())
	if os.IsNotExist(err) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 65537))
	if err != nil {
		return p, err
	}
	if len(raw) > 65536 {
		return p, fmt.Errorf("索引配置超过64 KiB")
	}
	var loaded knowledgeIndexPolicy
	if err = json.Unmarshal(raw, &loaded); err != nil {
		return p, err
	}
	if err = loaded.validate(); err != nil {
		return p, err
	}
	return loaded, nil
}
func knowledgeIndexRevision(p knowledgeIndexPolicy) string { b, _ := json.Marshal(p); return hash(b) }
func (a *App) getKnowledgeIndexPolicy(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	p, err := a.loadKnowledgeIndexPolicy()
	warning := ""
	if err != nil {
		warning = "配置读取失败，使用默认索引预算"
	}
	jsonOut(w, 200, map[string]any{"policy": p, "workspaceId": a.wsID(), "revision": knowledgeIndexRevision(p), "warning": warning})
}
func (a *App) putKnowledgeIndexPolicy(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Policy      knowledgeIndexPolicy `json:"policy"`
		WorkspaceID string               `json:"workspaceId"`
		Revision    string               `json:"revision"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 65536))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		fail(w, 400, err)
		return
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		fail(w, 400, fmt.Errorf("索引配置必须为单一JSON对象"))
		return
	}
	if err := body.Policy.validate(); err != nil {
		fail(w, 400, err)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	current, err := a.loadKnowledgeIndexPolicy()
	if err != nil {
		fail(w, 500, err)
		return
	}
	if body.WorkspaceID != a.wsID() || body.Revision != knowledgeIndexRevision(current) {
		fail(w, 409, fmt.Errorf("工作区或索引配置已变化，请重新加载"))
		return
	}
	target := a.knowledgeIndexPolicyPath()
	if err = os.MkdirAll(filepath.Dir(target), 0700); err == nil {
		err = atomicJSON(target, body.Policy)
	}
	if err != nil {
		fail(w, 500, err)
		return
	}
	a.wsRevision++ // Invalidates both graph-mode caches on their next synchronization.
	jsonOut(w, 200, map[string]any{"policy": body.Policy, "workspaceId": a.wsID(), "revision": knowledgeIndexRevision(body.Policy)})
}
func knowledgePriorityRank(p string, priorities []string) int {
	for i, v := range priorities {
		if p == v || strings.HasPrefix(p, v+"/") || strings.HasPrefix(v, p+"/") {
			return i
		}
	}
	return len(priorities)
}
func knowledgeSortPriority(entries []os.DirEntry, dir string, priorities []string) {
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i].Name(), entries[j].Name()
		ra, rb := knowledgePriorityRank(path.Join(dir, a), priorities), knowledgePriorityRank(path.Join(dir, b), priorities)
		if ra != rb {
			return ra < rb
		}
		return a < b
	})
}
