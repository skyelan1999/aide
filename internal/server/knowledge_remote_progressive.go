package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"unicode/utf8"
)

type knowledgeRemoteCursor struct {
	Path   string
	Offset int
	Stamp  string
	Depth  int
}
type knowledgeRemoteDiscovery struct {
	Queue       []knowledgeRemoteCursor
	Visited     map[string]bool
	Current     map[string]knowledgeNode
	Published   map[string]knowledgeNode
	Code        map[string]knowledgeCodeFile
	CurrentCode map[string]knowledgeCodeFile
	Reasons     map[string]bool
	Cycle       int
	Complete    bool
}

func newKnowledgeRemoteDiscovery() *knowledgeRemoteDiscovery {
	return &knowledgeRemoteDiscovery{Queue: []knowledgeRemoteCursor{{Path: "."}}, Visited: map[string]bool{}, Current: map[string]knowledgeNode{}, Published: map[string]knowledgeNode{}, Code: map[string]knowledgeCodeFile{}, CurrentCode: map[string]knowledgeCodeFile{}, Reasons: map[string]bool{}, Cycle: 1}
}
func (d *knowledgeRemoteDiscovery) clone() *knowledgeRemoteDiscovery {
	if d == nil {
		return nil
	}
	c := *d
	c.Queue = append([]knowledgeRemoteCursor(nil), d.Queue...)
	c.Visited = map[string]bool{}
	for k, v := range d.Visited {
		c.Visited[k] = v
	}
	c.Reasons = map[string]bool{}
	for k, v := range d.Reasons {
		c.Reasons[k] = v
	}
	c.Current = map[string]knowledgeNode{}
	for k, v := range d.Current {
		c.Current[k] = v
	}
	c.Published = map[string]knowledgeNode{}
	for k, v := range d.Published {
		c.Published[k] = v
	}
	c.Code = map[string]knowledgeCodeFile{}
	for k, v := range d.Code {
		c.Code[k] = v
	}
	c.CurrentCode = map[string]knowledgeCodeFile{}
	for k, v := range d.CurrentCode {
		c.CurrentCode[k] = v
	}
	return &c
}

// The caller runs this on a clone and commits only a successful, still-current
// source observation. No open handles or credentials are retained by the cursor.
func (d *knowledgeRemoteDiscovery) step(ctx context.Context, ar knowledgeArea, policy knowledgeIndexPolicy, includeCode bool, fileQuota, dirQuota int, list func(string) ([]map[string]any, error), read func(string, int64) ([]byte, error)) (knowledgeRemoteResult, error) {
	if d.Complete {
		n := newKnowledgeRemoteDiscovery()
		n.Published = d.Published
		n.Code = d.Code
		n.Cycle = d.Cycle + 1
		*d = *n
	}
	// SFTP text reads are sequential. Keep each batch short enough for the
	// existing four-second source deadline even with large configured budgets.
	fileQuota = min(32, max(1, fileQuota))
	dirQuota = max(1, dirQuota)
	capEntries := min(50000, max(1000, 4*(policy.FileBudget+policy.DirectoryBudget)))
	files, dirs, bytesRead := 0, 0, 0
	codeBytes := 0
	for _, f := range d.CurrentCode {
		codeBytes += len(f.Text)
	}
	scopes, priorities := policy.ScopePaths[ar.id()], policy.PriorityPaths[ar.id()]
	priority := func(p string) int {
		for i, v := range priorities {
			if p == v || strings.HasPrefix(v, p+"/") || strings.HasPrefix(p, v+"/") {
				return i
			}
		}
		return len(priorities)
	}
	for len(d.Queue) > 0 && files < fileQuota && dirs < dirQuota {
		if err := ctx.Err(); err != nil {
			return knowledgeRemoteResult{}, err
		}
		cursor := d.Queue[0]
		if cursor.Depth > policy.DepthBudget {
			d.Reasons["depth-budget"] = true
			d.Queue = d.Queue[1:]
			continue
		}
		entries, err := list(cursor.Path)
		if err != nil {
			return knowledgeRemoteResult{}, err
		}
		dirs++
		// Sort before fingerprinting; changing remote enumeration order alone must
		// not restart a directory. Priority ordering is stable under this policy.
		sort.Slice(entries, func(i, j int) bool {
			pi, pj := path.Join(cursor.Path, fmt.Sprint(entries[i]["name"])), path.Join(cursor.Path, fmt.Sprint(entries[j]["name"]))
			if priority(pi) != priority(pj) {
				return priority(pi) < priority(pj)
			}
			return pi < pj
		})
		raw, _ := json.Marshal(entries)
		stamp := hash(raw)
		if cursor.Stamp != "" && cursor.Stamp != stamp {
			// Restart the candidate cycle when a resumed directory changes. Otherwise
			// removed entries already admitted in this cycle could survive completion.
			n := newKnowledgeRemoteDiscovery()
			n.Published = d.Published
			n.Code = d.Code
			n.Cycle = d.Cycle + 1
			*d = *n
			break
		}
		cursor.Stamp = stamp
		id := knowledgeID(ar.scope, ar.root, ar.source, cursor.Path)
		name := path.Base(cursor.Path)
		if cursor.Path == "." {
			name = ar.name
		}
		d.Current[cursor.Path] = ar.node(knowledgeNode{ID: id, Name: name, Path: cursor.Path, Kind: "directory", Region: ar.region(), Root: ar.root, Source: ar.source})
		d.Visited[cursor.Path] = true
		for cursor.Offset < len(entries) && files < fileQuota {
			if err := ctx.Err(); err != nil {
				return knowledgeRemoteResult{}, err
			}
			e := entries[cursor.Offset]
			cursor.Offset++
			name, _ := e["name"].(string)
			rel := path.Join(cursor.Path, name)
			isDir, _ := e["dir"].(bool)
			symlink, _ := e["symlink"].(bool)
			if name == "" || path.Base(name) != name || knowledgeSkip(name) || symlink || !knowledgeSafePath(rel) || !knowledgeInScope(rel, scopes, isDir) {
				continue
			}
			if len(d.Current)+len(d.Queue) >= capEntries {
				d.Reasons["catalog-limit"] = true
				d.Queue = nil
				break
			}
			if isDir {
				if !d.Visited[rel] {
					d.Visited[rel] = true
					d.Queue = append(d.Queue, knowledgeRemoteCursor{Path: rel, Depth: cursor.Depth + 1})
				}
				continue
			}
			files++
			n := ar.node(knowledgeNode{ID: knowledgeID(ar.scope, ar.root, ar.source, rel), Name: name, Path: rel, Kind: "file", Region: ar.region(), Root: ar.root, Source: ar.source, Size: knowledgeEntrySize(e)})
			if v, ok := e["modified"].(string); ok {
				n.Modified = v
			}
			codeCandidate := includeCode && codeLanguage(rel) != "" && codeLanguage(rel) != "unsupported"
			wantCode := codeCandidate && n.Size <= 256*1024 && len(d.CurrentCode) < 80 && int64(codeBytes)+n.Size <= 4<<20
			if codeCandidate && !wantCode {
				d.Reasons["code-budget"] = true
			}
			limit := int64(64 * 1024)
			if wantCode {
				limit = 256 * 1024
			}
			limit = min(limit, int64(1024*1024-bytesRead))
			if knowledgeTextFormat(n.Format) || wantCode || rel == "go.mod" {
				if n.Size <= limit && limit > 0 {
					b, err := read(rel, limit)
					if err != nil {
						return knowledgeRemoteResult{}, err
					}
					bytesRead += len(b)
					n.Format = documentNodeBytesExtension(n, b)
					n.Text = knowledgeTextBytes(b)
					if wantCode && utf8.Valid(b) && !bytes.ContainsRune(b, 0) {
						if codeBytes+len(b) <= 4<<20 && len(b) <= 256*1024 {
							d.CurrentCode[rel] = knowledgeCodeFile{ID: n.ID, Node: n, Text: string(b)}
							codeBytes += len(b)
						} else {
							d.Reasons["code-budget"] = true
						}
					}
				} else {
					d.Reasons["text-budget"] = true
				}
			}
			d.Current[rel] = n
		}
		if len(d.Queue) == 0 {
			break
		}
		if cursor.Offset < len(entries) {
			d.Queue[0] = cursor
			break
		}
		d.Queue = d.Queue[1:]
	}
	d.Complete = len(d.Queue) == 0
	// Text/code limits do not hide directory entries. A complete enumeration
	// can remove missing metadata even when content coverage remains partial.
	catalogueComplete := d.Complete
	for reason := range d.Reasons {
		if reason != "text-budget" && reason != "code-budget" {
			catalogueComplete = false
		}
	}
	if catalogueComplete {
		d.Published = map[string]knowledgeNode{}
		d.Code = map[string]knowledgeCodeFile{}
	}
	for p, n := range d.Current {
		d.Published[p] = n
		delete(d.Code, p)
	}
	for p, f := range d.CurrentCode {
		d.Code[p] = f
	}
	// Old generations share the same budget. Keep current-cycle observations
	// first, then deterministically retain old snippets within the remaining space.
	codePaths := make([]string, 0, len(d.Code))
	for p := range d.Code {
		codePaths = append(codePaths, p)
	}
	sort.Slice(codePaths, func(i, j int) bool {
		_, currentI := d.CurrentCode[codePaths[i]]
		_, currentJ := d.CurrentCode[codePaths[j]]
		if currentI != currentJ {
			return currentI
		}
		return codePaths[i] < codePaths[j]
	})
	kept, retainedBytes := 0, 0
	for _, p := range codePaths {
		f := d.Code[p]
		if kept >= 80 || retainedBytes+len(f.Text) > 4<<20 {
			delete(d.Code, p)
			d.Reasons["code-budget"] = true
			continue
		}
		kept++
		retainedBytes += len(f.Text)
	}
	// Bound retained generations as well as this cycle's catalogue. Retain the
	// current cycle first; old observations remain only in the remaining capacity.
	if len(d.Published) > capEntries {
		old := []string{}
		for p := range d.Published {
			if _, ok := d.Current[p]; !ok {
				old = append(old, p)
			}
		}
		sort.Strings(old)
		for _, p := range old {
			if len(d.Published) <= capEntries {
				break
			}
			delete(d.Published, p)
			delete(d.Code, p)
		}
		d.Reasons["catalog-limit"] = true
	}
	return d.snapshot(ar, policy, fileQuota, dirQuota), nil
}

// Project committed observations without source I/O. Used after a restored
// cursor's speculative scan fails; never publish the failed candidate's data.
func (d *knowledgeRemoteDiscovery) snapshot(ar knowledgeArea, policy knowledgeIndexPolicy, fileQuota, dirQuota int) knowledgeRemoteResult {
	fileQuota = min(32, max(1, fileQuota))
	dirQuota = max(1, dirQuota)
	capEntries := min(50000, max(1000, 4*(policy.FileBudget+policy.DirectoryBudget)))
	scopes := policy.ScopePaths[ar.id()]
	r := knowledgeRemoteResult{state: "ready"}
	c := &knowledgeCoverage{Progressive: true, Cycle: d.Cycle, Pending: len(d.Queue), CatalogLimit: capEntries, FileBudget: fileQuota, DirectoryBudget: dirQuota, DepthBudget: policy.DepthBudget, TextBytesPerFile: 8192, ScopePaths: append([]string(nil), scopes...), Reasons: []string{}, TotalKnown: d.Complete && len(d.Reasons) == 0}
	for reason := range d.Reasons {
		c.Reasons = append(c.Reasons, reason)
	}
	sort.Strings(c.Reasons)
	if !d.Complete || len(d.Reasons) > 0 {
		r.state = "partial"
		r.message = "远端分批索引中，未核实区域保留上一周期快照"
		r.truncated = true
	}
	paths := make([]string, 0, len(d.Published))
	for p := range d.Published {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	module := ""
	if n, ok := d.Published["go.mod"]; ok {
		for _, line := range strings.Split(n.Text, "\n") {
			f := strings.Fields(line)
			if len(f) == 2 && f[0] == "module" {
				module = strings.Trim(f[1], "\"")
				break
			}
		}
	}
	for _, p := range paths {
		n := d.Published[p]
		r.nodes = append(r.nodes, n)
		if n.Kind == "file" {
			c.Files++
		} else {
			c.Directories++
		}
		if p != "." {
			if parent, ok := d.Published[path.Dir(p)]; ok {
				r.edges = append(r.edges, knowledgeEdge{From: parent.ID, To: n.ID, Kind: "contains"})
			}
		}
		if f, ok := d.Code[p]; ok {
			f.Module = module
			r.code = append(r.code, f)
		}
	}
	r.coverage = c
	return r
}
