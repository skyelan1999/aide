package server

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const knowledgeTimelineBytes = 8 << 20
const knowledgeTimelineEntries = 32

type knowledgeTimeSnapshot struct {
	ID       string         `json:"id"`
	Observed string         `json:"observed"`
	Digest   string         `json:"digest"`
	Graph    knowledgeGraph `json:"graph"`
}
type knowledgeTimeStore struct {
	Version   int                     `json:"version"`
	Workspace string                  `json:"workspace"`
	Code      bool                    `json:"code"`
	Dropped   int                     `json:"dropped"`
	Snapshots []knowledgeTimeSnapshot `json:"snapshots"`
}
type knowledgeTimeChange struct {
	Before *knowledgeNode `json:"before,omitempty"`
	After  *knowledgeNode `json:"after,omitempty"`
}
type knowledgeTimeComparison struct {
	Before       knowledgeTimeSnapshot `json:"before"`
	After        knowledgeTimeSnapshot `json:"after"`
	Added        []knowledgeNode       `json:"added"`
	Changed      []knowledgeTimeChange `json:"changed"`
	Removed      []knowledgeNode       `json:"removed"`
	AddedEdges   []knowledgeEdge       `json:"addedEdges"`
	RemovedEdges []knowledgeEdge       `json:"removedEdges"`
}

func knowledgeTimeKey(token string) []byte {
	k := sha256.Sum256([]byte("aide-knowledge-timeline-v1\x00" + token))
	return k[:]
}
func knowledgeTimeFile(data, workspace string, code bool) string {
	return filepath.Join(data, "cache", "knowledge-timeline", hash([]byte(fmt.Sprintf("%s:%t", workspace, code)))+".json")
}
func loadKnowledgeTime(file, workspace string, code bool, key []byte) (knowledgeTimeStore, error) {
	out := knowledgeTimeStore{Version: 1, Workspace: workspace, Code: code, Snapshots: []knowledgeTimeSnapshot{}}
	info, err := os.Lstat(file)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return out, fmt.Errorf("知识历史不是可读取的普通文件")
	}
	f, err := os.Open(file)
	if err != nil {
		return out, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, (16<<20)+1))
	if err != nil || len(b) > 16<<20 {
		return out, fmt.Errorf("知识历史文件超出容量")
	}
	var envelope knowledgeVectorEnvelope
	if json.Unmarshal(b, &envelope) != nil || envelope.Version != 1 {
		return out, fmt.Errorf("知识历史封装损坏")
	}
	plain, err := openWithKey(key, envelope.Ciphertext, envelope.Nonce)
	if err != nil {
		return out, fmt.Errorf("知识历史解密失败；保留原文件")
	}
	defer zeroBytes(plain)
	if len(plain) > knowledgeTimelineBytes || json.Unmarshal(plain, &out) != nil || out.Version != 1 || out.Workspace != workspace || out.Code != code || out.Dropped < 0 || len(out.Snapshots) > knowledgeTimelineEntries {
		return out, fmt.Errorf("知识历史身份或格式无效；保留原文件")
	}
	seen := map[string]bool{}
	for _, s := range out.Snapshots {
		if s.ID == "" || seen[s.ID] || s.Graph.Workspace != workspace || len(s.Digest) != 64 {
			return out, fmt.Errorf("知识历史快照无效")
		}
		seen[s.ID] = true
	}
	return out, nil
}

// Origin is a process-local citation guard, not a historical content change.
// Historical nodes deliberately cannot masquerade as live source citations.
func knowledgeTimeGraph(g knowledgeGraph) knowledgeGraph {
	g.Nodes = append([]knowledgeNode(nil), g.Nodes...)
	g.Edges = append([]knowledgeEdge(nil), g.Edges...)
	for i := range g.Nodes {
		g.Nodes[i].Origin = ""
	}
	sort.Slice(g.Nodes, func(i, j int) bool { return g.Nodes[i].ID < g.Nodes[j].ID })
	sort.Slice(g.Edges, func(i, j int) bool { return knowledgeEdgeKey(g.Edges[i]) < knowledgeEdgeKey(g.Edges[j]) })
	unique := g.Edges[:0]
	last := ""
	for _, edge := range g.Edges {
		key := knowledgeEdgeKey(edge)
		if len(unique) == 0 || key != last {
			unique = append(unique, edge)
			last = key
		}
	}
	g.Edges = unique
	return g
}
func appendKnowledgeTime(store knowledgeTimeStore, g knowledgeGraph, now time.Time) (knowledgeTimeStore, bool, error) {
	g = knowledgeTimeGraph(g)
	semantic, _ := json.Marshal(struct {
		Nodes []knowledgeNode
		Edges []knowledgeEdge
		Code  *knowledgeCodeSummary
	}{g.Nodes, g.Edges, g.Code})
	digest := hash(semantic)
	if len(store.Snapshots) > 0 && store.Snapshots[len(store.Snapshots)-1].Digest == digest {
		return store, false, nil
	}
	observed := now.UTC().Format(time.RFC3339Nano)
	snapshot := knowledgeTimeSnapshot{ID: "H" + hash([]byte(digest + observed))[:24], Observed: observed, Digest: digest, Graph: g}
	// Build a new slice so a failed write cannot mutate the caller's loaded history.
	store.Snapshots = append(append([]knowledgeTimeSnapshot(nil), store.Snapshots...), snapshot)
	for {
		raw, err := json.Marshal(store)
		if err != nil {
			return store, false, err
		}
		if len(store.Snapshots) <= knowledgeTimelineEntries && len(raw) <= knowledgeTimelineBytes {
			return store, true, nil
		}
		if len(store.Snapshots) == 1 {
			return store, false, fmt.Errorf("单次索引快照超过知识历史容量；现有历史保留")
		}
		store.Snapshots = store.Snapshots[1:]
		store.Dropped++
	}
}
func saveKnowledgeTime(file string, store knowledgeTimeStore, key []byte) error {
	plain, err := json.Marshal(store)
	if err != nil {
		return err
	}
	defer zeroBytes(plain)
	if len(plain) > knowledgeTimelineBytes {
		return fmt.Errorf("知识历史超出容量")
	}
	ct, nonce, err := sealWithKey(key, plain)
	if err != nil {
		return err
	}
	dir := filepath.Dir(file)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if info, e := os.Lstat(dir); e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("知识历史目录无效")
	}
	if info, e := os.Lstat(file); e == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return fmt.Errorf("知识历史目标无效")
	}
	return atomicJSON(file, knowledgeVectorEnvelope{1, ct, nonce})
}
func (a *App) recordKnowledgeTime(g knowledgeGraph, code bool, epoch uint64) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if epoch != a.wsRevision || g.Workspace != knowledgeID(a.wsID(), "scope", "", ".") {
		return fmt.Errorf("工作区已变化，未保存知识历史")
	}
	if a.settings.UserPasswordHash != "" && a.loadLockStateLocked().Locked {
		return nil
	}
	file := knowledgeTimeFile(a.dataPath, g.Workspace, code)
	key := knowledgeTimeKey(a.token)
	store, err := loadKnowledgeTime(file, g.Workspace, code, key)
	if err != nil {
		return err
	}
	store, changed, err := appendKnowledgeTime(store, g, time.Now())
	if err != nil || !changed {
		return err
	}
	return saveKnowledgeTime(file, store, key)
}
func compareKnowledgeTime(before, after knowledgeTimeSnapshot) knowledgeTimeComparison {
	out := knowledgeTimeComparison{Before: before, After: after, Added: []knowledgeNode{}, Changed: []knowledgeTimeChange{}, Removed: []knowledgeNode{}, AddedEdges: []knowledgeEdge{}, RemovedEdges: []knowledgeEdge{}}
	old := map[string]knowledgeNode{}
	current := map[string]bool{}
	for _, n := range before.Graph.Nodes {
		old[n.ID] = n
	}
	for _, n := range after.Graph.Nodes {
		current[n.ID] = true
		if prev, ok := old[n.ID]; !ok {
			out.Added = append(out.Added, n)
		} else {
			a, _ := json.Marshal(prev)
			b, _ := json.Marshal(n)
			if string(a) != string(b) {
				prevCopy, nextCopy := prev, n
				out.Changed = append(out.Changed, knowledgeTimeChange{&prevCopy, &nextCopy})
			}
		}
	}
	for _, n := range before.Graph.Nodes {
		if !current[n.ID] {
			out.Removed = append(out.Removed, n)
		}
	}
	oldEdges, nextEdges := map[string]bool{}, map[string]bool{}
	for _, e := range before.Graph.Edges {
		oldEdges[knowledgeEdgeKey(e)] = true
	}
	for _, e := range after.Graph.Edges {
		nextEdges[knowledgeEdgeKey(e)] = true
		if !oldEdges[knowledgeEdgeKey(e)] {
			out.AddedEdges = append(out.AddedEdges, e)
		}
	}
	for _, e := range before.Graph.Edges {
		if !nextEdges[knowledgeEdgeKey(e)] {
			out.RemovedEdges = append(out.RemovedEdges, e)
		}
	}
	// Keep historical coverage/code diagnostics but avoid returning two redundant full graphs.
	out.Before.Graph.Nodes = nil
	out.Before.Graph.Edges = nil
	out.After.Graph.Nodes = nil
	out.After.Graph.Edges = nil
	return out
}
func (a *App) knowledgeTimeline(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	workspace := r.URL.Query().Get("workspace")
	code := r.URL.Query().Get("code") == "1"
	var query struct {
		Before string `json:"before"`
		After  string `json:"after"`
	}
	if r.Method == "POST" {
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
		d.DisallowUnknownFields()
		if d.Decode(&query) != nil || query.Before == "" || query.After == "" || len(query.Before) > 64 || len(query.After) > 64 {
			fail(w, 400, fmt.Errorf("历史比较参数无效"))
			return
		}
		if d.Decode(&struct{}{}) != io.EOF {
			fail(w, 400, fmt.Errorf("历史比较只接受一个对象"))
			return
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.settings.UserPasswordHash != "" && a.loadLockStateLocked().Locked {
		fail(w, 423, fmt.Errorf("请先解锁工作台"))
		return
	}
	if workspace == "" || workspace != knowledgeID(a.wsID(), "scope", "", ".") {
		fail(w, 409, fmt.Errorf("工作区已变化，请重新同步星图"))
		return
	}
	store, err := loadKnowledgeTime(knowledgeTimeFile(a.dataPath, workspace, code), workspace, code, knowledgeTimeKey(a.token))
	if err != nil {
		fail(w, 500, err)
		return
	}
	if r.Method == "POST" {
		var before, after *knowledgeTimeSnapshot
		for i := range store.Snapshots {
			s := &store.Snapshots[i]
			if s.ID == query.Before {
				before = s
			}
			if s.ID == query.After {
				after = s
			}
		}
		if before == nil || after == nil {
			fail(w, 404, fmt.Errorf("历史快照不存在或已超出保留范围"))
			return
		}
		jsonOut(w, 200, compareKnowledgeTime(*before, *after))
		return
	}
	entries := []map[string]any{}
	for _, s := range store.Snapshots {
		entries = append(entries, map[string]any{"id": s.ID, "observed": s.Observed, "digest": s.Digest, "nodes": len(s.Graph.Nodes), "edges": len(s.Graph.Edges), "truncated": s.Graph.Truncated, "sources": s.Graph.Sources, "warnings": s.Graph.Warnings})
	}
	jsonOut(w, 200, map[string]any{"workspace": workspace, "code": code, "entries": entries, "dropped": store.Dropped, "limit": knowledgeTimelineEntries, "bytesLimit": knowledgeTimelineBytes})
}
