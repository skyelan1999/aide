package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const knowledgeRemoteInterval = 30 * time.Second
const knowledgeRemoteTimeout = 4 * time.Second

func knowledgeSafePath(p string) bool {
	return safePath(p) == nil && !strings.ContainsAny(p, "\x00\r\n\t")
}

// A source's public catalogue never contains its endpoint, credentials or MCP
// command. Origin is an opaque configuration identity used for stale citations.
type knowledgeCoverage struct {
	ScopePaths       []string `json:"scopePaths,omitempty"`
	Progressive      bool     `json:"progressive,omitempty"`
	Pending          int      `json:"pending,omitempty"`
	Cycle            int      `json:"cycle,omitempty"`
	CatalogLimit     int      `json:"catalogLimit,omitempty"`
	Files            int      `json:"files"`
	Directories      int      `json:"directories"`
	FileBudget       int      `json:"fileBudget"`
	DirectoryBudget  int      `json:"directoryBudget"`
	DepthBudget      int      `json:"depthBudget"`
	TextBytesPerFile int      `json:"textBytesPerFile"`
	TotalKnown       bool     `json:"totalKnown"`
	Reasons          []string `json:"reasons"`
}

type knowledgeSource struct {
	ID        string             `json:"id"`
	Name      string             `json:"name"`
	Type      string             `json:"type"`
	Region    string             `json:"region"`
	State     string             `json:"state"`
	Message   string             `json:"message,omitempty"`
	NodeCount int                `json:"nodeCount"`
	Coverage  *knowledgeCoverage `json:"coverage,omitempty"`
}

type knowledgeArea struct {
	root, source, name, kind, location, scope, origin string
	enabled                                           bool
	src                                               Source
	ws                                                WorkspaceConfig
	epoch, generation                                 uint64
}

func (ar knowledgeArea) region() string {
	if ar.source != "" {
		return "source:" + ar.source
	}
	return ar.root
}
func (ar knowledgeArea) id() string {
	if ar.source != "" {
		return ar.source
	}
	return ar.root
}
func (ar knowledgeArea) local() bool { return ar.kind == "local" || ar.kind == "skill" }
func (ar knowledgeArea) node(n knowledgeNode) knowledgeNode {
	n.SourceName, n.SourceType, n.Origin = ar.name, ar.kind, ar.origin
	if n.Kind == "file" {
		n.Format = strings.ToLower(path.Ext(n.Path))
	}
	if n.Kind == "file" && (ar.kind == "link" || ar.kind == "smb") {
		if u, err := url.Parse(ar.src.Config.URL); err == nil {
			if ext := strings.ToLower(path.Ext(u.Path)); ext != "" {
				n.Format = ext
			}
		}
	}
	return n
}

// Caller holds a.mu; configurations and slice fields are copied before any I/O.
func (a *App) knowledgeAreasLocked(scope, workspaceName, referenceName string) []knowledgeArea {
	state := &a.knowledgeUpdates
	state.originOnce.Do(func() {
		state.originSeed = make([]byte, 32)
		if _, err := rand.Read(state.originSeed); err != nil {
			state.originSeed = []byte(hash([]byte(fmt.Sprintf("%p:%d", a, time.Now().UnixNano()))))
		}
	})
	ws := a.wsConfig
	workspaceKind := "local"
	if ws.Workspace.Mode == "ssh" {
		workspaceKind = "ssh"
	}
	areas := []knowledgeArea{{root: "workspace", name: "工作区", kind: workspaceKind, location: workspaceName, enabled: true}}
	if referenceName != "" {
		areas = append(areas, knowledgeArea{root: "context", name: "引用目录", kind: "local", location: referenceName, enabled: true})
	}
	for _, src := range a.sourceRegistry.Sources {
		src.Config.Args = append([]string(nil), src.Config.Args...)
		src.Config.MCPTools = append([]MCPTool(nil), src.Config.MCPTools...)
		ar := knowledgeArea{root: "source", source: src.ID, name: src.Name, kind: src.Type, enabled: src.Enabled, src: src}
		if ar.local() {
			ar.location = referenceName
			if strings.TrimSpace(src.Config.Path) != "" {
				ar.location, _, _ = a.resolveHostPath(src.Config.Path)
			}
		}
		areas = append(areas, ar)
	}
	for i := range areas {
		ar := &areas[i]
		ar.scope, ar.ws, ar.epoch = scope, ws, a.wsRevision
		switch ar.kind {
		case "ssh", "workspace-sftp":
			ar.generation = a.sshSessionGeneration(sshControlSocket)
		case "sftp":
			ar.generation = a.sshSessionGeneration(sourceSocket(ar.source))
		}
		identity := []any{scope, ar.root, ar.source, ar.kind, ar.location, ar.enabled, ar.src, ar.epoch, ar.generation}
		if ar.source != "" {
			password, key := a.sourceCredentialLocked(ar.source)
			identity = append(identity, password, key)
		}
		if ar.kind == "ssh" || ar.kind == "workspace-sftp" {
			identity = append(identity, ws.Workspace)
		}
		b, _ := json.Marshal(identity)
		ar.origin = "O" + hash(append(append([]byte(nil), state.originSeed...), b...))[:24]
	}
	return areas
}

func (a *App) knowledgeAreaCurrent(ar knowledgeArea) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	var work, ref string
	if a.workspace != nil {
		work = a.workspace.Name()
	}
	if a.reference != nil {
		ref = a.reference.Name()
	}
	for _, current := range a.knowledgeAreasLocked(a.wsID(), work, ref) {
		if current.root == ar.root && current.source == ar.source {
			return current.enabled && current.origin == ar.origin
		}
	}
	return false
}

func knowledgeSetSource(g *knowledgeGraph, ar knowledgeArea, state, message string, count int) {
	for i := range g.Sources {
		if g.Sources[i].Region == ar.region() {
			g.Sources[i].State, g.Sources[i].Message, g.Sources[i].NodeCount = state, message, count
			return
		}
	}
}

type knowledgeRemoteResult struct {
	nodes          []knowledgeNode
	edges          []knowledgeEdge
	code           []knowledgeCodeFile
	state, message string
	truncated      bool
	coverage       *knowledgeCoverage
}
type knowledgeRemoteMemo struct {
	origin              string
	fileLimit, dirLimit int
	checked             time.Time
	result              knowledgeRemoteResult
	pending             chan struct{}
	policy              string
	discovery           *knowledgeRemoteDiscovery
}

func (a *App) knowledgeRemoteCached(ctx context.Context, ar knowledgeArea, code bool, fileLimit, dirLimit int) knowledgeRemoteResult {
	state := &a.knowledgeUpdates
	a.mu.Lock()
	policy, policyErr := a.loadKnowledgeIndexPolicy()
	a.mu.Unlock()
	if policyErr != nil {
		return knowledgeRemoteResult{state: "unavailable", message: "索引规则读取失败，保留上次星图", truncated: true}
	}
	policyID := knowledgeIndexRevision(policy)
	progressive := ar.kind == "ssh" || ar.kind == "workspace-sftp" || ar.kind == "sftp" || ar.kind == "ftp" || ar.kind == "ftps"
	key := ar.scope + "\x00" + ar.region() + "\x00" + fmt.Sprint(code)
	for {
		state.remoteMu.Lock()
		if state.remote == nil {
			state.remote = map[string]*knowledgeRemoteMemo{}
		}
		memo := state.remote[key]
		if memo != nil && memo.origin == ar.origin && memo.policy == policyID && memo.fileLimit == fileLimit && memo.dirLimit == dirLimit {
			if memo.pending != nil {
				pending := memo.pending
				state.remoteMu.Unlock()
				select {
				case <-pending:
					continue
				case <-ctx.Done():
					return knowledgeRemoteResult{state: "unavailable", message: "来源扫描等待超时，稍后自动重试", truncated: true}
				}
			}
			if time.Since(memo.checked) < knowledgeRemoteInterval {
				result := memo.result
				state.remoteMu.Unlock()
				return result
			}
		}
		var previous knowledgeRemoteResult
		var discovery *knowledgeRemoteDiscovery
		if memo != nil && memo.origin == ar.origin && memo.policy == policyID && memo.fileLimit == fileLimit && memo.dirLimit == dirLimit {
			previous = memo.result
			discovery = memo.discovery.clone()
		}
		memo = &knowledgeRemoteMemo{origin: ar.origin, policy: policyID, fileLimit: fileLimit, dirLimit: dirLimit, pending: make(chan struct{})}
		state.remote[key] = memo
		state.remoteMu.Unlock()
		var result knowledgeRemoteResult
		if progressive {
			checkpointWarning := ""
			if discovery == nil {
				var restoreErr error
				discovery, restoreErr = a.knowledgeCursorCheckpoint(ar, policyID, code, fileLimit, dirLimit, nil)
				if restoreErr != nil {
					checkpointWarning = "索引检查点不可恢复，重新扫描"
				}
				if discovery == nil {
					discovery = newKnowledgeRemoteDiscovery()
				}
			}
			candidate := discovery.clone()
			var err error
			result, err = candidate.step(ctx, ar, policy, code, fileLimit, dirLimit, func(p string) ([]map[string]any, error) { return a.knowledgeAreaList(ctx, ar, p) }, func(p string, limit int64) ([]byte, error) { return a.knowledgeAreaRead(ctx, ar, p, limit) })
			if err == nil && a.knowledgeAreaCurrent(ar) {
				discovery = candidate
				if _, saveErr := a.knowledgeCursorCheckpoint(ar, policyID, code, fileLimit, dirLimit, discovery); saveErr != nil {
					checkpointWarning = "索引检查点未保存；本次结果仅保留在内存"
				}
			} else {
				result = discovery.snapshot(ar, policy, fileLimit, dirLimit)
				result.state = "unavailable"
				result.message = "来源扫描未完成，保留已发布节点并稍后重试"
				result.truncated = true
				result.coverage.TotalKnown = false
				result.coverage.Reasons = append(result.coverage.Reasons, "source-unavailable")
			}
			if checkpointWarning != "" {
				result.message = strings.TrimSpace(result.message + "；" + checkpointWarning)
			}
		} else {
			result = a.knowledgeRemoteScan(ctx, ar, code, fileLimit, dirLimit)
		}
		if !a.knowledgeAreaCurrent(ar) {
			result = knowledgeRemoteResult{state: "unavailable", message: "来源配置已变化，等待重新同步", truncated: true}
		}
		if !progressive && (result.state == "unavailable" || result.state == "partial") && len(previous.nodes) != 0 {
			seen := map[string]bool{}
			for _, n := range result.nodes {
				seen[n.ID] = true
			}
			for _, n := range previous.nodes {
				if !seen[n.ID] {
					result.nodes = append(result.nodes, n)
				}
			}
			edges := map[string]bool{}
			for _, e := range result.edges {
				edges[knowledgeEdgeKey(e)] = true
			}
			for _, e := range previous.edges {
				if !edges[knowledgeEdgeKey(e)] {
					result.edges = append(result.edges, e)
				}
			}
			codeSeen := map[string]bool{}
			for _, f := range result.code {
				codeSeen[f.ID] = true
			}
			for _, f := range previous.code {
				if !codeSeen[f.ID] {
					result.code = append(result.code, f)
				}
			}
			result = knowledgeRemoteTrim(result, fileLimit, dirLimit)
			result.truncated = true
			result.message += "；保留未核实的上次节点，正文读取仍需重新核实"
		}
		state.remoteMu.Lock()
		memo.checked, memo.result, memo.discovery = time.Now(), result, discovery
		close(memo.pending)
		memo.pending = nil
		state.remoteMu.Unlock()
		return result
	}
}

func (a *App) knowledgeAppendRemote(ctx context.Context, g *knowledgeGraph, areas []knowledgeArea, includeCode bool, codeFiles *[]knowledgeCodeFile, codeBytes, codeSkipped *int) {
	remote := []knowledgeArea{}
	allowed := map[string]string{}
	for _, ar := range areas {
		if ar.enabled && !ar.local() {
			remote = append(remote, ar)
			allowed[ar.scope+"\x00"+ar.region()] = ar.origin
		}
	}
	// Stop/removal/reconfiguration drops its old private cache immediately.
	state := &a.knowledgeUpdates
	state.remoteMu.Lock()
	for key, memo := range state.remote {
		keep := false
		for prefix, origin := range allowed {
			if strings.HasPrefix(key, prefix+"\x00") && memo.origin == origin {
				keep = true
				break
			}
		}
		if !keep {
			delete(state.remote, key)
		}
	}
	state.remoteMu.Unlock()
	if len(remote) == 0 {
		return
	}
	budget, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	results := make([]knowledgeRemoteResult, len(remote))
	sem := make(chan struct{}, 3)
	var wg sync.WaitGroup
	fileLimit, dirLimit := knowledgeAreaQuotas(areas)
	a.mu.Lock()
	policy, policyErr := a.loadKnowledgeIndexPolicy()
	a.mu.Unlock()
	if policyErr == nil {
		active := 0
		for _, ar := range areas {
			if ar.enabled {
				active++
			}
		}
		if active > 0 {
			fileLimit = max(1, policy.FileBudget/active)
			dirLimit = max(1, policy.DirectoryBudget/active)
		}
	}
	for i, ar := range remote {
		wg.Add(1)
		go func(i int, ar knowledgeArea) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-budget.Done():
				results[i] = knowledgeRemoteResult{state: "unavailable", message: "本轮远端扫描预算已用完，稍后自动重试", truncated: true}
				return
			}
			child, stop := context.WithTimeout(budget, knowledgeRemoteTimeout)
			defer stop()
			results[i] = a.knowledgeRemoteCached(child, ar, includeCode, fileLimit, dirLimit)
		}(i, ar)
	}
	wg.Wait()
	for i, result := range results {
		ar := remote[i]
		if !a.knowledgeAreaCurrent(ar) {
			result = knowledgeRemoteResult{state: "unavailable", message: "来源已改配或停用，等待重新同步", truncated: true}
		}
		g.Nodes, g.Edges = append(g.Nodes, result.nodes...), append(g.Edges, result.edges...)
		knowledgeSetSource(g, ar, result.state, result.message, len(result.nodes))
		for j := range g.Sources {
			if g.Sources[j].Region == ar.region() {
				g.Sources[j].Coverage = result.coverage
				break
			}
		}
		g.Truncated = g.Truncated || result.truncated
		if result.message != "" {
			g.Warnings = append(g.Warnings, ar.name+"："+result.message)
		}
		for _, f := range result.code {
			if len(*codeFiles) >= 80 || *codeBytes+len(f.Text) > 4*1024*1024 {
				*codeSkipped++
				g.Truncated = true
				continue
			}
			*codeBytes += len(f.Text)
			*codeFiles = append(*codeFiles, f)
		}
	}
}

func knowledgeTextBytes(b []byte) string {
	if len(b) > 8192 {
		b = b[:8192]
	}
	for trimmed := 0; trimmed < 3 && len(b) > 0 && !utf8.Valid(b); trimmed++ {
		b = b[:len(b)-1]
	}
	if !utf8.Valid(b) || strings.ContainsRune(string(b), 0) {
		return ""
	}
	return string(b)
}
func knowledgeTextFormat(ext string) bool {
	return strings.Contains("|.md|.txt|.go|.py|.js|.ts|.tsx|.jsx|.html|.css|.json|.csv|.yaml|.yml|.xml|.svg|.sh|.toml|.rst|", "|"+ext+"|")
}

func knowledgeAreaQuotas(areas []knowledgeArea) (int, int) {
	active := 0
	for _, ar := range areas {
		if ar.enabled {
			active++
		}
	}
	if active == 0 {
		return 0, 0
	}
	return 800 / active, 200 / active
}

func knowledgeRemoteTrim(r knowledgeRemoteResult, fileLimit, dirLimit int) knowledgeRemoteResult {
	kept := map[string]bool{}
	nodes := []knowledgeNode{}
	files, dirs := 0, 0
	for _, n := range r.nodes {
		if n.Kind == "directory" {
			if dirs >= dirLimit {
				continue
			}
			dirs++
		} else {
			if files >= fileLimit {
				continue
			}
			files++
		}
		kept[n.ID] = true
		nodes = append(nodes, n)
	}
	r.nodes = nodes
	// A partial scan may combine new directories with cached children. Remove
	// children whose ancestor was excluded by the directory budget.
	parents := map[string]string{}
	for _, e := range r.edges {
		if e.Kind == "contains" {
			parents[e.To] = e.From
		}
	}
	for changed := true; changed; {
		changed = false
		for id, parent := range parents {
			if kept[id] && !kept[parent] {
				delete(kept, id)
				changed = true
			}
		}
	}
	connected := []knowledgeNode{}
	for _, n := range r.nodes {
		if kept[n.ID] {
			connected = append(connected, n)
		}
	}
	r.nodes = connected
	edges := []knowledgeEdge{}
	for _, e := range r.edges {
		if kept[e.From] && kept[e.To] {
			edges = append(edges, e)
		}
	}
	r.edges = edges
	code := []knowledgeCodeFile{}
	for _, f := range r.code {
		if kept[f.ID] {
			code = append(code, f)
		}
	}
	r.code = code
	return r
}

func (a *App) knowledgeRemoteScan(ctx context.Context, ar knowledgeArea, includeCode bool, fileLimit, dirLimit int) knowledgeRemoteResult {
	r := knowledgeRemoteResult{state: "ready"}
	if ar.kind == "mcp" {
		r.state, r.message = "catalog", "仅索引已发现的工具说明，不连接或调用 MCP；不代表远端文档已索引"
	}
	dirs, files, bytesRead := 0, 0, 0
	var walk func(string, string, int) error
	walk = func(p, parent string, depth int) error {
		if dirs >= dirLimit || depth > 7 {
			r.truncated = true
			return nil
		}
		entries, err := a.knowledgeAreaList(ctx, ar, p)
		if err != nil {
			return err
		}
		dirs++
		id := knowledgeID(ar.scope, ar.root, ar.source, p)
		name := path.Base(p)
		if p == "." {
			name = ar.name
		}
		r.nodes = append(r.nodes, ar.node(knowledgeNode{ID: id, Name: name, Kind: "directory", Region: ar.region(), Root: ar.root, Source: ar.source, Path: p}))
		if parent != "" {
			r.edges = append(r.edges, knowledgeEdge{From: parent, To: id, Kind: "contains"})
		}
		sort.Slice(entries, func(i, j int) bool { return fmt.Sprint(entries[i]["name"]) < fmt.Sprint(entries[j]["name"]) })
		for _, entry := range entries {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			name, _ := entry["name"].(string)
			symlink, _ := entry["symlink"].(bool)
			if name == "" || knowledgeSkip(name) || symlink {
				continue
			}
			rel, _ := entry["path"].(string)
			if rel == "" {
				rel = path.Join(p, name)
			}
			if safePath(rel) != nil {
				continue
			}
			if dir, _ := entry["dir"].(bool); dir {
				if err := walk(rel, id, depth+1); err != nil {
					return err
				}
				continue
			}
			if files >= fileLimit {
				r.truncated = true
				break
			}
			files++
			n := ar.node(knowledgeNode{ID: knowledgeID(ar.scope, ar.root, ar.source, rel), Name: name, Kind: "file", Region: ar.region(), Root: ar.root, Source: ar.source, Path: rel})
			n.Size = knowledgeEntrySize(entry)
			if modified, ok := entry["modified"].(string); ok {
				n.Modified = modified
			}
			wantCode := includeCode && codeLanguage(rel) != "" && codeLanguage(rel) != "unsupported" && n.Size <= 256*1024 && len(r.code) < 16
			limit := int64(64 * 1024)
			if wantCode {
				limit = 256 * 1024
			}
			if remaining := int64(1024*1024 - bytesRead); remaining < limit {
				limit = remaining
			}
			if (knowledgeTextFormat(n.Format) || wantCode || rel == "go.mod") && n.Size <= limit && bytesRead < 1024*1024 {
				b, readErr := a.knowledgeAreaRead(ctx, ar, rel, limit)
				if readErr != nil {
					r.state, r.message = "partial", "部分正文因读取、大小或时间限制未取得"
				} else {
					bytesRead += len(b)
					n.Format = documentNodeBytesExtension(n, b)
					if knowledgeTextFormat(n.Format) || rel == "go.mod" {
						n.Text = knowledgeTextBytes(b)
					}
					if wantCode && utf8.Valid(b) && !bytes.ContainsRune(b, 0) {
						r.code = append(r.code, knowledgeCodeFile{ID: n.ID, Node: n, Text: string(b)})
					}
				}
			} else if knowledgeTextFormat(n.Format) {
				r.state, r.message = "partial", "部分正文超过星图读取预算，仅保留名称；可在文档检索中按预算读取"
			} else if ar.kind == "link" || ar.kind == "smb" {
				r.state, r.message = "partial", "已索引配置的资源名称，正文及连通性在文档检索时核实"
			}
			r.nodes = append(r.nodes, n)
			r.edges = append(r.edges, knowledgeEdge{From: id, To: n.ID, Kind: "contains"})
		}
		return nil
	}
	if err := walk(".", "", 0); err != nil {
		if len(r.nodes) == 0 {
			r.state = "unavailable"
		} else {
			r.state = "partial"
		}
		r.message, r.truncated = "来源不可访问或扫描超时，稍后自动重试", true
	}
	if r.truncated && r.message == "" {
		r.state, r.message = "partial", "达到目录、文件、深度或扫描时间上限"
	}
	module := ""
	for _, n := range r.nodes {
		if n.Path != "go.mod" {
			continue
		}
		for _, line := range strings.Split(n.Text, "\n") {
			parts := strings.Fields(line)
			if len(parts) == 2 && parts[0] == "module" {
				module = strings.Trim(parts[1], "\"")
				break
			}
		}
	}
	for i := range r.code {
		r.code[i].Module = module
	}
	return r
}

func (a *App) knowledgeRevalidateGraph(g *knowledgeGraph) {
	a.mu.Lock()
	var work, ref string
	if a.workspace != nil {
		work = a.workspace.Name()
	}
	if a.reference != nil {
		ref = a.reference.Name()
	}
	scope := a.wsID()
	areas := a.knowledgeAreasLocked(scope, work, ref)
	a.mu.Unlock()
	if knowledgeID(scope, "scope", "", ".") != g.Workspace {
		g.Nodes, g.Edges, g.Sources = []knowledgeNode{}, []knowledgeEdge{}, []knowledgeSource{}
		g.Truncated = true
		g.Warnings = append(g.Warnings, "工作区已切换，本轮索引已丢弃")
		return
	}
	current := map[string]string{}
	for _, ar := range areas {
		if ar.enabled {
			current[ar.region()] = ar.origin
		}
	}
	kept := map[string]bool{}
	nodes := []knowledgeNode{}
	invalid := map[string]bool{}
	for _, n := range g.Nodes {
		if n.Kind != "session" && (n.Origin == "" || current[n.Region] != n.Origin) {
			invalid[n.Region] = true
			continue
		}
		kept[n.ID] = true
		nodes = append(nodes, n)
	}
	g.Nodes = nodes
	edges := []knowledgeEdge{}
	for _, e := range g.Edges {
		if kept[e.From] && kept[e.To] {
			edges = append(edges, e)
		}
	}
	g.Edges = edges
	counts := map[string]int{}
	for _, n := range g.Nodes {
		counts[n.Region]++
	}
	for i := range g.Sources {
		g.Sources[i].NodeCount = counts[g.Sources[i].Region]
		if invalid[g.Sources[i].Region] {
			g.Sources[i].State, g.Sources[i].Message, g.Sources[i].NodeCount = "unavailable", "来源配置已变化，等待下一轮同步", 0
			g.Truncated = true
		}
	}
}

func knowledgeEntrySize(entry map[string]any) int64 {
	switch v := entry["size"].(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	}
	return 0
}

func knowledgeRemoteBase(ar knowledgeArea) string {
	base := ar.ws.Workspace.Path
	if strings.TrimSpace(base) == "" {
		base = "."
	}
	if ar.kind == "sftp" {
		return ar.src.Config.Path
	}
	if ar.kind == "workspace-sftp" {
		if path.IsAbs(ar.src.Config.Path) {
			return path.Clean(ar.src.Config.Path)
		}
		return pathJoinRemote(base, ar.src.Config.Path)
	}
	return base
}

func (a *App) knowledgeAreaList(ctx context.Context, ar knowledgeArea, p string) ([]map[string]any, error) {
	if !knowledgeSafePath(p) || !a.knowledgeAreaCurrent(ar) {
		return nil, fmt.Errorf("来源路径或配置已变化")
	}
	switch ar.kind {
	case "ssh", "workspace-sftp", "sftp":
		if strings.ContainsAny(knowledgeRemoteBase(ar), "\x00\r\n\t\\") {
			return nil, fmt.Errorf("来源远端目录包含不支持的控制字符")
		}
		out, err := a.knowledgeSFTP(ctx, ar, "cd "+shellQuoteRemote(pathJoinRemote(knowledgeRemoteBase(ar), p))+"\nls -l\n", 256*1024)
		if err != nil {
			return nil, err
		}
		return parseSFTPListLimit(string(out), p, 0), nil
	case "link", "smb":
		if p != "." {
			return nil, fmt.Errorf("单资源来源不支持目录浏览")
		}
		return []map[string]any{{"name": "resource.txt", "path": "resource.txt", "dir": false}}, nil
	case "mcp":
		return mcpVirtualFiles(ar.src, p)
	case "ftp", "ftps":
		out, err := a.knowledgeCurl(ctx, ar, p, true, 256*1024)
		if err != nil {
			return nil, err
		}
		return knowledgeFTPListing(string(out), p), nil
	}
	return nil, fmt.Errorf("不支持的引用来源类型")
}

// The subprocess already bounds listing bytes. Preserve all returned entries so
// a resumable catalogue cannot silently treat the first 2,000 as a full directory.
func knowledgeFTPListing(out, p string) []map[string]any {
	items := parseSFTPListLimit(out, p, 0)
	if len(items) > 0 {
		return items
	}
	for _, line := range strings.Split(out, "\n") {
		name := strings.TrimSpace(line)
		fields := strings.Fields(name)
		if len(fields) >= 8 && len(fields[0]) >= 10 && strings.ContainsRune("-dlpsbc?", rune(fields[0][0])) {
			continue
		}
		dir := strings.HasSuffix(name, "/")
		name = strings.TrimSuffix(name, "/")
		if name == "" || strings.HasPrefix(name, "total ") || strings.Contains(name, "/") || !knowledgeSafePath(name) || name == "." || name == ".." {
			continue
		}
		items = append(items, map[string]any{"name": name, "path": path.Join(p, name), "dir": dir})
	}
	return items
}

// A cancellation-aware bounded writer closes the subprocess as soon as its
// output reaches the request limit. Errors never include URL/credential output.
type knowledgeLimitWriter struct {
	buf      bytes.Buffer
	max      int64
	stop     context.CancelFunc
	overflow bool
}

func (w *knowledgeLimitWriter) Write(p []byte) (int, error) {
	if int64(w.buf.Len()+len(p)) > w.max {
		w.overflow = true
		w.stop()
		return 0, fmt.Errorf("来源超过读取字节上限")
	}
	return w.buf.Write(p)
}
func knowledgeRun(ctx context.Context, cmd *exec.Cmd, max int64) ([]byte, error) {
	w := &knowledgeLimitWriter{max: max, stop: func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}}
	cmd.Stdout, cmd.WaitDelay = w, 2*time.Second
	if cmd.Stderr == nil {
		cmd.Stderr = io.Discard
	}
	err := cmd.Run()
	if w.overflow {
		return nil, fmt.Errorf("来源超过读取字节上限")
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, fmt.Errorf("来源读取失败")
	}
	return w.buf.Bytes(), nil
}

func (a *App) knowledgeSFTP(ctx context.Context, ar knowledgeArea, batch string, max int64) ([]byte, error) {
	sock, target, port := sshControlSocket, "", ar.ws.Workspace.Port
	if ar.kind == "sftp" {
		sock, target, port = sourceSocket(ar.source), a.sftpTargetOf(ar.src), ar.src.Config.Port
		if err := a.ensureSourceSession(ctx, ar.src); err != nil {
			return nil, fmt.Errorf("SFTP 来源连接不可用")
		}
	} else {
		u := ar.ws.Workspace.Username
		if u == "" {
			u = "root"
		}
		target = u + "@" + ar.ws.Workspace.Host
		if err := a.ensureSSHSession(ctx); err != nil {
			return nil, fmt.Errorf("SSH 工作区连接不可用")
		}
	}
	unlock, err := a.lockSSHSession(ctx, sock)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if ar.generation != a.sshSessionGeneration(sock) {
		return nil, fmt.Errorf("SSH 连接配置已变化")
	}
	if port == 0 {
		port = 22
	}
	args := []string{"-o", "ControlPath=" + sock, "-o", "BatchMode=yes", "-o", "LogLevel=ERROR", "-P", strconv.Itoa(port), "-b", "-", target}
	// Limit temporary downloads in the child process as well as captured output.
	// POSIX shells express -f in blocks; Linux dash uses 1KiB. On 512-byte
	// implementations the cap is stricter. No source data is a shell command.
	blocks := (max + 1023) / 1024
	cmdArgs := append([]string{"-c", "ulimit -f " + strconv.FormatInt(blocks, 10) + " || exit 1; exec \"$@\"", "aide-knowledge-sftp", a.sftpBin}, args...)
	cmd := exec.CommandContext(ctx, "/bin/sh", cmdArgs...)
	cmd.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=/home/aide"}
	cmd.Stdin = strings.NewReader(batch)
	diagnostic := &knowledgeLimitWriter{max: 64 * 1024, stop: func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}}
	cmd.Stderr = diagnostic
	out, err := knowledgeRun(ctx, cmd, max)
	if err == nil && (diagnostic.overflow || sftpCommandDiagnosticFailed(string(out)+"\n"+diagnostic.buf.String())) {
		err = fmt.Errorf("SFTP 目录或文件不可读取")
	}
	return out, err
}

func (a *App) knowledgeCurl(ctx context.Context, ar knowledgeArea, p string, listing bool, max int64) ([]byte, error) {
	// Snapshot the credential together with the exact URL/configuration. Never
	// pair a newly saved password with an old endpoint from a stale graph.
	a.mu.Lock()
	current, exists := a.findSource(ar.source)
	currentJSON, _ := json.Marshal(current)
	pinnedJSON, _ := json.Marshal(ar.src)
	if !exists || !current.Enabled || !bytes.Equal(currentJSON, pinnedJSON) {
		a.mu.Unlock()
		return nil, fmt.Errorf("来源配置已变化")
	}
	password, _ := a.sourceCredentialLocked(ar.source)
	a.mu.Unlock()
	if !a.knowledgeAreaCurrent(ar) {
		return nil, fmt.Errorf("来源凭据已变化")
	}
	target := ar.src.Config.URL
	if p != "" && p != "." && ar.kind != "link" && ar.kind != "smb" {
		u, parseErr := url.Parse(target)
		if parseErr != nil {
			return nil, fmt.Errorf("来源地址无效")
		}
		u.Path, u.RawPath = strings.TrimRight(u.Path, "/")+"/"+p, ""
		target = u.String()
	}
	args := []string{"-sS", "--fail", "--max-time", "15", "-L", "--proto", "=http,https,ftp,ftps,smb,smbs", "--proto-redir", "=http,https,ftp,ftps,smb,smbs"}
	if ar.kind == "ftps" {
		args = append(args, "--ssl-reqd")
	}
	if ar.src.Config.Username != "" {
		args = append(args, "-u", ar.src.Config.Username+":"+password)
	}
	args = append(args, target)
	if listing {
		args[len(args)-1] = strings.TrimRight(args[len(args)-1], "/") + "/"
	}
	args = append([]string{"--max-filesize", strconv.FormatInt(max, 10)}, args...)
	out, err := knowledgeRun(ctx, exec.CommandContext(ctx, a.curlBin, args...), max)
	if err == nil && !a.knowledgeAreaCurrent(ar) {
		return nil, fmt.Errorf("引用来源已变化")
	}
	return out, err
}

func (a *App) knowledgeAreaRead(ctx context.Context, ar knowledgeArea, p string, maxBytes int64) ([]byte, error) {
	if !knowledgeSafePath(p) || !a.knowledgeAreaCurrent(ar) {
		return nil, fmt.Errorf("引用来源已变化或路径无效")
	}
	var b []byte
	var err error
	switch ar.kind {
	case "local", "skill":
		root, openErr := os.OpenRoot(ar.location)
		if openErr != nil {
			return nil, openErr
		}
		defer root.Close()
		// Recheck every path component; os.Root also prevents symlink escape.
		parts := strings.Split(p, "/")
		for i := range parts {
			info, statErr := root.Lstat(strings.Join(parts[:i+1], "/"))
			if statErr != nil {
				return nil, statErr
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("不索引符号链接")
			}
			if i == len(parts)-1 && (!info.Mode().IsRegular() || info.Size() > maxBytes) {
				return nil, fmt.Errorf("文件不是普通文件或超过读取上限")
			}
		}
		f, openErr := root.Open(p)
		if openErr != nil {
			return nil, openErr
		}
		defer f.Close()
		info, statErr := f.Stat()
		if statErr != nil || !info.Mode().IsRegular() || info.Size() > maxBytes {
			return nil, fmt.Errorf("文件状态已变化或超过读取上限")
		}
		b, err = io.ReadAll(io.LimitReader(f, maxBytes+1))
	case "mcp":
		b, err = mcpVirtualFile(ar.src, p)
	case "link", "smb", "ftp", "ftps":
		if (ar.kind == "link" || ar.kind == "smb") && p != "resource.txt" {
			return nil, fmt.Errorf("未知单资源路径")
		}
		b, err = a.knowledgeCurl(ctx, ar, p, false, maxBytes)
	case "ssh", "workspace-sftp", "sftp":
		if strings.ContainsAny(knowledgeRemoteBase(ar), "\x00\r\n\t\\") {
			return nil, fmt.Errorf("来源远端目录包含不支持的控制字符")
		}
		parts := strings.Split(p, "/")
		for i, part := range parts {
			parent := "."
			if i > 0 {
				parent = strings.Join(parts[:i], "/")
			}
			items, listErr := a.knowledgeAreaList(ctx, ar, parent)
			if listErr != nil {
				return nil, listErr
			}
			found := false
			for _, item := range items {
				if item["name"] != part {
					continue
				}
				if link, _ := item["symlink"].(bool); link {
					return nil, fmt.Errorf("不索引符号链接")
				}
				dir, _ := item["dir"].(bool)
				if dir != (i < len(parts)-1) {
					return nil, fmt.Errorf("来源文件路径类型已变化")
				}
				if !dir && knowledgeEntrySize(item) > maxBytes {
					return nil, fmt.Errorf("来源文件超过读取字节上限")
				}
				found = true
				break
			}
			if !found {
				return nil, os.ErrNotExist
			}
		}
		tmp, createErr := os.CreateTemp("", "aide-knowledge-read-*")
		if createErr != nil {
			return nil, createErr
		}
		tmpPath := tmp.Name()
		tmp.Close()
		defer os.Remove(tmpPath)
		_, err = a.knowledgeSFTP(ctx, ar, "get "+shellQuoteRemote(pathJoinRemote(knowledgeRemoteBase(ar), p))+" "+shellQuoteRemote(tmpPath)+"\n", maxBytes)
		if err == nil {
			f, readErr := os.Open(tmpPath)
			if readErr != nil {
				return nil, readErr
			}
			b, err = io.ReadAll(io.LimitReader(f, maxBytes+1))
			f.Close()
		}
	default:
		err = fmt.Errorf("不支持的引用来源类型")
	}
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > maxBytes {
		return nil, fmt.Errorf("来源文件超过读取字节上限")
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if !a.knowledgeAreaCurrent(ar) {
		return nil, fmt.Errorf("引用来源已变化，请重新检索")
	}
	return b, nil
}

// Shared by raw search, RAG and citation insertion. Never reads through a new
// configuration using the old node's ID; origin is checked before and after I/O.
func (a *App) knowledgeReadNode(ctx context.Context, n knowledgeNode, workspace string, maxBytes int64) ([]byte, error) {
	if n.Kind != "file" || maxBytes <= 0 || maxBytes > 16*1024*1024 {
		return nil, fmt.Errorf("文件读取范围无效")
	}
	a.mu.Lock()
	scope := a.wsID()
	var work, ref string
	if a.workspace != nil {
		work = a.workspace.Name()
	}
	if a.reference != nil {
		ref = a.reference.Name()
	}
	areas := a.knowledgeAreasLocked(scope, work, ref)
	a.mu.Unlock()
	if workspace != knowledgeID(scope, "scope", "", ".") {
		return nil, fmt.Errorf("工作区已切换，请重新检索")
	}
	for _, ar := range areas {
		if ar.root != n.Root || ar.source != n.Source {
			continue
		}
		if !ar.enabled || (n.Origin != "" && n.Origin != ar.origin) {
			return nil, fmt.Errorf("引用来源已变化或停用，请重新检索")
		}
		bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		return a.knowledgeAreaRead(bounded, ar, n.Path, maxBytes)
	}
	return nil, fmt.Errorf("引用来源已不存在")
}

// A star-map viewer carries the graph's source identity into all content reads.
// Ordinary SSH raw reads can omit it, but still pin the current connection.
func (a *App) knowledgeReadViewer(ctx context.Context, rootName, sourceID, p, origin, workspace string, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 || maxBytes > 64<<20 || origin != "" && workspace == "" {
		return nil, fmt.Errorf("查看范围或来源身份无效")
	}
	if sourceID != "" {
		rootName = "source"
	} else if rootName == "" {
		rootName = "workspace"
	}
	a.mu.Lock()
	scope := a.wsID()
	var work, ref string
	if a.workspace != nil {
		work = a.workspace.Name()
	}
	if a.reference != nil {
		ref = a.reference.Name()
	}
	areas := a.knowledgeAreasLocked(scope, work, ref)
	a.mu.Unlock()
	if workspace != "" && workspace != knowledgeID(scope, "scope", "", ".") {
		return nil, fmt.Errorf("工作区已变化，请从星图重新打开")
	}
	for _, ar := range areas {
		if ar.root != rootName || ar.source != sourceID {
			continue
		}
		if !ar.enabled || origin != "" && origin != ar.origin {
			return nil, fmt.Errorf("引用来源已变化或停用，请从星图重新打开")
		}
		bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		return a.knowledgeAreaRead(bounded, ar, p, maxBytes)
	}
	return nil, fmt.Errorf("引用来源已不存在")
}

// Raw viewer compatibility: read from the selected configured source without
// requiring membership of the bounded star-map catalogue. It is still pinned to
// the supplied source configuration and to the current workspace revision.
func (a *App) knowledgeReadSource(ctx context.Context, src Source, p string, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 || maxBytes > 64*1024*1024 {
		return nil, fmt.Errorf("来源读取范围无效")
	}
	a.mu.Lock()
	var work, ref string
	if a.workspace != nil {
		work = a.workspace.Name()
	}
	if a.reference != nil {
		ref = a.reference.Name()
	}
	areas := a.knowledgeAreasLocked(a.wsID(), work, ref)
	a.mu.Unlock()
	wanted, _ := json.Marshal(src)
	for _, ar := range areas {
		if ar.root != "source" || ar.source != src.ID {
			continue
		}
		current, _ := json.Marshal(ar.src)
		if !ar.enabled || !bytes.Equal(wanted, current) {
			return nil, fmt.Errorf("引用来源已变化或停用")
		}
		bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		return a.knowledgeAreaRead(bounded, ar, p, maxBytes)
	}
	return nil, fmt.Errorf("引用来源已不存在")
}
