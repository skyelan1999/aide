package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"
)

type knowledgePathRequest struct {
	Workspace        string `json:"workspace"`
	Revision         string `json:"revision"`
	From             string `json:"from"`
	To               string `json:"to"`
	Code             bool   `json:"code"`
	Undirected       bool   `json:"undirected"`
	IncludeStructure bool   `json:"includeStructure"`
}

type knowledgePathStep struct {
	Edge     knowledgeEdge `json:"edge"`
	Reversed bool          `json:"reversed"`
}

type knowledgePathResult struct {
	Workspace string              `json:"workspace"`
	Revision  string              `json:"revision"`
	Found     bool                `json:"found"`
	Limited   bool                `json:"limited"`
	Nodes     []knowledgeNode     `json:"nodes"`
	Steps     []knowledgePathStep `json:"steps"`
	Warnings  []string            `json:"warnings"`
	Truncated bool                `json:"truncated"`
}

// Paths are indexed evidence trails, not runtime traces. Structural containment
// is opt-in; traversing a relation backwards never changes its original arrow.
func findKnowledgePath(ctx context.Context, g knowledgeGraph, q knowledgePathRequest) (knowledgePathResult, error) {
	out := knowledgePathResult{Workspace: g.Workspace, Revision: q.Revision, Nodes: []knowledgeNode{}, Steps: []knowledgePathStep{}, Warnings: g.Warnings, Truncated: g.Truncated}
	nodes := make(map[string]knowledgeNode, len(g.Nodes))
	for _, n := range g.Nodes {
		nodes[n.ID] = n
	}
	if _, ok := nodes[q.From]; !ok {
		return out, fmt.Errorf("起点不在当前索引中")
	}
	if _, ok := nodes[q.To]; !ok {
		return out, fmt.Errorf("终点不在当前索引中")
	}
	type arc struct {
		to   string
		step knowledgePathStep
	}
	adj := map[string][]arc{}
	for _, e := range g.Edges {
		if e.Kind == "contains" && !q.IncludeStructure {
			continue
		}
		if _, ok := nodes[e.From]; !ok {
			continue
		}
		if _, ok := nodes[e.To]; !ok {
			continue
		}
		adj[e.From] = append(adj[e.From], arc{e.To, knowledgePathStep{Edge: e}})
		if q.Undirected {
			adj[e.To] = append(adj[e.To], arc{e.From, knowledgePathStep{Edge: e, Reversed: true}})
		}
	}
	for k := range adj {
		sort.Slice(adj[k], func(i, j int) bool {
			a, b := adj[k][i], adj[k][j]
			if a.to != b.to {
				return a.to < b.to
			}
			return knowledgeEdgeKey(a.step.Edge) < knowledgeEdgeKey(b.step.Edge)
		})
	}
	type previous struct {
		id    string
		step  knowledgePathStep
		depth int
	}
	seen := map[string]previous{q.From: {}}
	queue := []string{q.From}
	for head := 0; head < len(queue); head++ {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		id := queue[head]
		if id == q.To {
			break
		}
		if seen[id].depth >= 32 {
			out.Limited = true
			continue
		}
		for _, next := range adj[id] {
			if _, ok := seen[next.to]; ok {
				continue
			}
			if len(seen) >= 16000 {
				out.Limited = true
				continue
			}
			seen[next.to] = previous{id, next.step, seen[id].depth + 1}
			queue = append(queue, next.to)
		}
	}
	if _, ok := seen[q.To]; !ok {
		return out, nil
	}
	out.Found = true
	for id := q.To; ; id = seen[id].id {
		out.Nodes = append(out.Nodes, nodes[id])
		if id == q.From {
			break
		}
		out.Steps = append(out.Steps, seen[id].step)
	}
	for i, j := 0, len(out.Nodes)-1; i < j; i, j = i+1, j-1 {
		out.Nodes[i], out.Nodes[j] = out.Nodes[j], out.Nodes[i]
	}
	for i, j := 0, len(out.Steps)-1; i < j; i, j = i+1, j-1 {
		out.Steps[i], out.Steps[j] = out.Steps[j], out.Steps[i]
	}
	return out, nil
}

func (a *App) knowledgePaths(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var q knowledgePathRequest
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	d.DisallowUnknownFields()
	if err := d.Decode(&q); err != nil {
		fail(w, 400, fmt.Errorf("路径查询参数无效"))
		return
	}
	if err := d.Decode(&struct{}{}); err != io.EOF {
		fail(w, 400, fmt.Errorf("路径查询仅接受一个对象"))
		return
	}
	if q.Workspace == "" || q.Revision == "" || q.From == "" || q.To == "" || len(q.Workspace) > 128 || len(q.Revision) > 128 || len(q.From) > 128 || len(q.To) > 128 {
		fail(w, 400, fmt.Errorf("路径查询缺少有效编号和版本"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	s := &a.knowledgeUpdates
	s.once.Do(func() { s.gate = make(chan struct{}, 1) })
	select {
	case s.gate <- struct{}{}:
		defer func() { <-s.gate }()
	case <-ctx.Done():
		fail(w, 503, fmt.Errorf("索引繁忙，请稍后重试"))
		return
	}
	workspace, epoch := a.knowledgeUpdatesWorkspace()
	mode := 0
	if q.Code {
		mode = 1
	}
	v := s.views[mode]
	if workspace != q.Workspace || s.workspace != workspace || s.epoch != epoch || v == nil || v.failed || v.revision != q.Revision {
		fail(w, 409, fmt.Errorf("星图版本已变化，请同步后重新查询路径"))
		return
	}
	out, err := findKnowledgePath(ctx, v.graph, q)
	if err != nil {
		fail(w, 400, err)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.settings.UserPasswordHash != "" && a.loadLockStateLocked().Locked {
		fail(w, 423, fmt.Errorf("请先解锁工作台"))
		return
	}
	current := knowledgeID(a.wsID(), "scope", "", ".")
	if current != workspace || a.wsRevision != epoch || ctx.Err() != nil {
		fail(w, 409, fmt.Errorf("工作区已变化或查询已取消"))
		return
	}
	jsonOut(w, 200, out)
}
