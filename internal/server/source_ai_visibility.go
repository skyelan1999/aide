package server

import (
	"encoding/json"
	"errors"
	"net/http"
)

// Missing settings preserve the existing AI access behavior.
func sourceAIVisible(src Source, workspace string) bool {
	for _, hidden := range src.AIHiddenWorkspaces {
		if hidden == workspace {
			return false
		}
	}
	return src.Enabled
}
func (a *App) sourceAIAllowed(id string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	src, ok := a.findSource(id)
	return ok && sourceAIVisible(src, a.wsID())
}
func (a *App) setSourceAIVisibility(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Source    string `json:"source"`
		Workspace string `json:"workspaceId"`
		Visible   *bool  `json:"visible"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&in); err != nil || in.Visible == nil {
		fail(w, 400, errors.New("缺少来源或可见状态"))
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if in.Workspace != a.wsID() {
		fail(w, 409, errors.New("工作区已切换，请重新设置来源可见性"))
		return
	}
	for i, src := range a.sourceRegistry.Sources {
		if src.ID != in.Source {
			continue
		}
		hidden := []string{}
		for _, id := range src.AIHiddenWorkspaces {
			if id != in.Workspace {
				hidden = append(hidden, id)
			}
		}
		if !*in.Visible {
			if len(hidden) >= 128 {
				fail(w, 400, errors.New("来源可见性设置已达上限"))
				return
			}
			hidden = append(hidden, in.Workspace)
		}
		a.sourceRegistry.Sources[i].AIHiddenWorkspaces = hidden
		if err := a.saveSources(); err != nil {
			a.sourceRegistry.Sources[i] = src
			fail(w, 500, err)
			return
		}
		jsonOut(w, 200, map[string]any{"source": src.ID, "workspaceId": in.Workspace, "aiVisible": *in.Visible})
		return
	}
	fail(w, 404, errors.New("来源不存在"))
}

// Filter copies rather than mutates cached graphs; manual graph browsing remains available.
func (a *App) knowledgeForAI(g knowledgeGraph) knowledgeGraph {
	a.mu.Lock()
	defer a.mu.Unlock()
	nodes := []knowledgeNode{}
	allowed := map[string]bool{}
	for _, n := range g.Nodes {
		id := n.Source
		if id == "" && n.Root == "context" {
			id = contextSource
		}
		if id != "" {
			src, ok := a.findSource(id)
			if !ok || !sourceAIVisible(src, a.wsID()) {
				continue
			}
		}
		nodes = append(nodes, n)
		allowed[n.ID] = true
	}
	edges := []knowledgeEdge{}
	for _, e := range g.Edges {
		if allowed[e.From] && allowed[e.To] {
			edges = append(edges, e)
		}
	}
	removed := len(nodes) != len(g.Nodes)
	g.Nodes = nodes
	g.Edges = edges
	// Aggregate code diagnostics can include hidden-source paths.
	if removed {
		g.Code = nil
		g.Warnings = []string{"已排除当前工作区 AI 不可见来源；其余索引仍可能有未覆盖范围，不表示全文已读。"}
	}
	return g
}
