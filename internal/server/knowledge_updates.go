package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

const knowledgeUpdateInterval = 2 * time.Second
const knowledgeRevisionLimit = 8

// The zero value is ready to use. One cancellable gate serializes scans for an
// App; caches are confined to its current workspace and its two graph modes.
type knowledgeUpdateState struct {
	once      sync.Once
	gate      chan struct{}
	workspace string
	epoch     uint64
	views     [2]*knowledgeUpdateView
}

type knowledgeUpdateView struct {
	checked  time.Time
	failed   bool
	graph    knowledgeGraph
	cache    *knowledgeScanCache
	revision string
	history  []knowledgeRevision
}

// Older revisions retain fingerprints, not copies of document/source text.
type knowledgeRevision struct {
	id    string
	nodes map[string]string
	edges map[string]knowledgeEdge
}

type knowledgeUpdateResponse struct {
	Workspace    string                `json:"workspace"`
	Revision     string                `json:"revision"`
	Reset        bool                  `json:"reset"`
	Nodes        []knowledgeNode       `json:"nodes"`
	RemovedNodes []string              `json:"removedNodes"`
	Edges        []knowledgeEdge       `json:"edges"`
	RemovedEdges []knowledgeEdge       `json:"removedEdges"`
	Warnings     []string              `json:"warnings"`
	Truncated    bool                  `json:"truncated"`
	Code         *knowledgeCodeSummary `json:"code,omitempty"`
}

type knowledgeFileStamp struct {
	size int64
	date int64
	mode os.FileMode
}

func knowledgeStamp(info os.FileInfo) knowledgeFileStamp {
	return knowledgeFileStamp{info.Size(), info.ModTime().UnixNano(), info.Mode()}
}

type knowledgeFileMemo struct {
	stamp     knowledgeFileStamp
	text      string
	code      string
	codeReady bool
}

type knowledgeCodeMemo struct {
	key       string
	nodes     []knowledgeNode
	edges     []knowledgeEdge
	files     map[string]knowledgeNode
	warnings  []string
	summary   *knowledgeCodeSummary
	truncated bool
}

type knowledgeScanCache struct {
	previous map[string]knowledgeFileMemo
	files    map[string]knowledgeFileMemo
	code     *knowledgeCodeMemo
	err      error
}

func newKnowledgeScanCache(previous *knowledgeScanCache) *knowledgeScanCache {
	c := &knowledgeScanCache{files: make(map[string]knowledgeFileMemo)}
	if previous != nil {
		c.previous, c.code = previous.files, previous.code
	}
	return c
}

func (c *knowledgeScanCache) fail() {
	if c != nil && c.err == nil {
		c.err = fmt.Errorf("资料扫描期间发生读取变化；保留当前星图，稍后自动重试")
	}
}

// Metadata traversal still occurs, so edits made outside Aide can be noticed.
// Content is reused only while size, nanosecond mtime and mode remain equal.
// An external writer preserving all three cannot be detected by this cache.
func (c *knowledgeScanCache) material(root *os.Root, location, p string, info os.FileInfo, wantCode bool) (string, string, error) {
	if c == nil {
		text := knowledgeText(root, p)
		if !wantCode {
			return text, "", nil
		}
		code, err := codeText(root, p)
		return text, code, err
	}
	key := location + "\x00" + p
	stamp := knowledgeStamp(info)
	current, err := root.Lstat(p)
	if err != nil || !current.Mode().IsRegular() || knowledgeStamp(current) != stamp {
		c.fail()
		return "", "", c.err
	}
	if old, ok := c.previous[key]; ok && old.stamp == stamp && (!wantCode || old.codeReady) {
		if !wantCode {
			old.code, old.codeReady = "", false
		}
		c.files[key] = old
		return old.text, old.code, nil
	}
	memo := knowledgeFileMemo{stamp: stamp}
	memo.text, err = knowledgeReadText(root, p)
	if err == nil && wantCode {
		memo.code, err = codeText(root, p)
		memo.codeReady = err == nil
	}
	if err != nil {
		c.fail()
		return "", "", err
	}
	current, err = root.Lstat(p)
	if err != nil || !current.Mode().IsRegular() || knowledgeStamp(current) != stamp {
		c.fail()
		return "", "", c.err
	}
	c.files[key] = memo
	return memo.text, memo.code, nil
}

// Cross-file name resolution is rebuilt whenever a source input changes. When
// all admitted source inputs and the base edge budget are identical, reuse the
// complete code expansion; unchanged polls do not launch parser subprocesses.
func (c *knowledgeScanCache) expandCode(ctx context.Context, g *knowledgeGraph, files []knowledgeCodeFile) {
	if c == nil {
		expandKnowledgeCode(ctx, g, files)
		return
	}
	inputs := []string{g.Workspace, fmt.Sprint(len(g.Edges))}
	for _, f := range files {
		inputs = append(inputs, f.ID, f.Node.Root, f.Node.Source, f.Node.Path, f.Module, hash([]byte(f.Text)))
	}
	raw, _ := json.Marshal(inputs)
	key := hash(raw)
	if memo := c.code; memo != nil && memo.key == key {
		for i, n := range g.Nodes {
			if cached, ok := memo.files[n.ID]; ok {
				g.Nodes[i].Language, g.Nodes[i].ContentHash = cached.Language, cached.ContentHash
			}
		}
		g.Nodes = append(g.Nodes, memo.nodes...)
		g.Edges = append(g.Edges, memo.edges...)
		g.Warnings = append(g.Warnings, memo.warnings...)
		g.Code, g.Truncated = memo.summary, g.Truncated || memo.truncated
		return
	}
	nodeCount, edgeCount, warningCount := len(g.Nodes), len(g.Edges), len(g.Warnings)
	beforeTruncated := g.Truncated
	g.Truncated = false
	expandKnowledgeCode(ctx, g, files)
	codeTruncated := g.Truncated
	g.Truncated = beforeTruncated || codeTruncated
	for i := nodeCount; i < len(g.Nodes); i++ {
		g.Nodes[i].Modified = "" // File touch alone is not a new AST declaration.
	}
	if ctx.Err() != nil {
		return
	}
	for _, diagnostic := range g.Code.Diagnostics {
		if strings.Contains(diagnostic, "解析器") || strings.Contains(diagnostic, "代码分析等待超时") || strings.Contains(diagnostic, "代码分析超时") {
			c.code = nil // Transient parser failures must be retried.
			return
		}
	}
	memo := &knowledgeCodeMemo{key: key, nodes: append([]knowledgeNode(nil), g.Nodes[nodeCount:]...), edges: append([]knowledgeEdge(nil), g.Edges[edgeCount:]...), files: map[string]knowledgeNode{}, warnings: append([]string(nil), g.Warnings[warningCount:]...), summary: g.Code, truncated: codeTruncated}
	for _, n := range g.Nodes[:nodeCount] {
		if n.ContentHash != "" {
			memo.files[n.ID] = n
		}
	}
	c.code = memo
}

func knowledgeEdgeKey(edge knowledgeEdge) string {
	raw, _ := json.Marshal(edge)
	return string(raw)
}

func knowledgeGraphRevision(g *knowledgeGraph) knowledgeRevision {
	sort.Slice(g.Nodes, func(i, j int) bool { return g.Nodes[i].ID < g.Nodes[j].ID })
	sort.Slice(g.Edges, func(i, j int) bool { return knowledgeEdgeKey(g.Edges[i]) < knowledgeEdgeKey(g.Edges[j]) })
	unique := g.Edges[:0]
	edges := make(map[string]knowledgeEdge, len(g.Edges))
	for _, edge := range g.Edges {
		key := knowledgeEdgeKey(edge)
		if _, ok := edges[key]; !ok {
			edges[key] = edge
			unique = append(unique, edge)
		}
	}
	g.Edges = unique
	rev := knowledgeRevision{nodes: make(map[string]string, len(g.Nodes)), edges: edges}
	for _, node := range g.Nodes {
		raw, _ := json.Marshal(node)
		rev.nodes[node.ID] = hash(raw)
	}
	raw, _ := json.Marshal(g)
	rev.id = "K" + hash(raw)[:24]
	return rev
}

func knowledgeDelta(view *knowledgeUpdateView, cursor string) knowledgeUpdateResponse {
	g := view.graph
	out := knowledgeUpdateResponse{Workspace: g.Workspace, Revision: view.revision, Nodes: []knowledgeNode{}, RemovedNodes: []string{}, Edges: []knowledgeEdge{}, RemovedEdges: []knowledgeEdge{}, Warnings: g.Warnings, Truncated: g.Truncated, Code: g.Code}
	var baseline *knowledgeRevision
	for i := range view.history {
		if cursor != "" && view.history[i].id == cursor {
			baseline = &view.history[i]
			break
		}
	}
	if baseline == nil {
		out.Reset, out.Nodes, out.Edges = true, g.Nodes, g.Edges
		return out
	}
	latest := view.history[len(view.history)-1]
	for _, node := range g.Nodes {
		if latest.nodes[node.ID] != baseline.nodes[node.ID] {
			out.Nodes = append(out.Nodes, node)
		}
	}
	for id := range baseline.nodes {
		if _, ok := latest.nodes[id]; !ok {
			out.RemovedNodes = append(out.RemovedNodes, id)
		}
	}
	for _, edge := range g.Edges {
		if _, ok := baseline.edges[knowledgeEdgeKey(edge)]; !ok {
			out.Edges = append(out.Edges, edge)
		}
	}
	for key, edge := range baseline.edges {
		if _, ok := latest.edges[key]; !ok {
			out.RemovedEdges = append(out.RemovedEdges, edge)
		}
	}
	sort.Strings(out.RemovedNodes)
	sort.Slice(out.RemovedEdges, func(i, j int) bool {
		return knowledgeEdgeKey(out.RemovedEdges[i]) < knowledgeEdgeKey(out.RemovedEdges[j])
	})
	return out
}

func (a *App) knowledgeUpdatesWorkspace() (string, uint64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return knowledgeID(a.wsID(), "scope", "", "."), a.wsRevision
}

func (a *App) knowledgeMapUpdates(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	cursor := r.URL.Query().Get("cursor")
	if len(cursor) > 128 {
		fail(w, http.StatusBadRequest, fmt.Errorf("星图版本编号无效"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	state := &a.knowledgeUpdates
	state.once.Do(func() { state.gate = make(chan struct{}, 1) })
	select {
	case state.gate <- struct{}{}:
		defer func() { <-state.gate }()
	case <-ctx.Done():
		fail(w, http.StatusServiceUnavailable, fmt.Errorf("资料更新扫描繁忙；保留当前星图，稍后重试"))
		return
	}
	workspace, epoch := a.knowledgeUpdatesWorkspace()
	if state.workspace != workspace || state.epoch != epoch {
		state.workspace, state.epoch, state.views = workspace, epoch, [2]*knowledgeUpdateView{}
	}
	mode := 0
	if r.URL.Query().Get("code") == "1" {
		mode = 1
	}
	view := state.views[mode]
	if view == nil {
		view = &knowledgeUpdateView{}
		state.views[mode] = view
	}
	if view.checked.IsZero() || time.Since(view.checked) >= knowledgeUpdateInterval {
		cache := newKnowledgeScanCache(view.cache)
		g := a.knowledgeSnapshotModeCached(ctx, mode == 1, cache)
		view.checked = time.Now() // Also throttle failed/cancelled attempts.
		if ctx.Err() != nil || cache.err != nil {
			view.failed = true
			fail(w, http.StatusServiceUnavailable, fmt.Errorf("资料更新扫描未完成；保留当前星图，稍后自动重试"))
			return
		}
		currentWorkspace, currentEpoch := a.knowledgeUpdatesWorkspace()
		if workspace != g.Workspace || currentWorkspace != workspace || currentEpoch != epoch {
			state.workspace, state.views = "", [2]*knowledgeUpdateView{}
			fail(w, http.StatusConflict, fmt.Errorf("工作区已切换，请重新同步星图"))
			return
		}
		revision := knowledgeGraphRevision(&g)
		revision.id = "K" + hash([]byte(fmt.Sprintf("%d:%s", epoch, revision.id)))[:24]
		if view.revision != revision.id {
			view.history = append(view.history, revision)
			if len(view.history) > knowledgeRevisionLimit {
				view.history = append([]knowledgeRevision(nil), view.history[len(view.history)-knowledgeRevisionLimit:]...)
			}
		}
		view.graph, view.revision, view.cache, view.failed = g, revision.id, cache, false
	}
	if view.failed || view.revision == "" {
		fail(w, http.StatusServiceUnavailable, fmt.Errorf("资料更新扫描未完成；保留当前星图，稍后自动重试"))
		return
	}
	currentWorkspace, currentEpoch := a.knowledgeUpdatesWorkspace()
	if ctx.Err() != nil || currentWorkspace != workspace || currentEpoch != epoch {
		fail(w, http.StatusConflict, fmt.Errorf("工作区已切换或请求已取消，请重新同步星图"))
		return
	}
	jsonOut(w, http.StatusOK, knowledgeDelta(view, cursor))
}
