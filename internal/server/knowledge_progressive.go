package server

import (
	"context"
	"os"
	"path"
	"sort"
	"time"
)

type knowledgeDiscoveryView struct {
	Cursor    *knowledgeDiscovery
	Published map[string]knowledgeDiscoveryEntry
	Cycle     int
}

func (v *knowledgeDiscoveryView) clone() *knowledgeDiscoveryView {
	c := &knowledgeDiscoveryView{Cursor: v.Cursor.Clone(), Published: make(map[string]knowledgeDiscoveryEntry, len(v.Published)), Cycle: v.Cycle}
	for p, e := range v.Published {
		c.Published[p] = e
	}
	return c
}

// Called under the existing scan gate. The new cache owns this candidate; a
// failed/cancelled scan never modifies the previously published generation.
func (c *knowledgeScanCache) discover(ctx context.Context, root *os.Root, ar knowledgeArea, p knowledgeIndexPolicy, files, dirs int) (*knowledgeDiscoveryView, knowledgeDiscoveryBatch, error) {
	if files < 1 {
		files = 1
	}
	if dirs < 1 {
		dirs = 1
	}
	key := knowledgeDiscoveryKey(ar)
	v := c.discovery[key]
	if v == nil {
		v = &knowledgeDiscoveryView{Cursor: newKnowledgeDiscovery(4 * (files + dirs)), Published: map[string]knowledgeDiscoveryEntry{}, Cycle: 1}
		c.discovery[key] = v
	}
	if v.Cursor.Complete {
		v.Cursor = newKnowledgeDiscovery(v.Cursor.MaxEntries)
		v.Cycle++
	}
	v.Cursor.ScopePaths = append([]string(nil), p.ScopePaths[ar.id()]...)
	b, err := v.Cursor.Step(ctx, root, files, dirs, p.DepthBudget, 4*(files+dirs), p.PriorityPaths[ar.id()])
	if err != nil {
		return v, b, err
	}
	if v.Cursor.Complete {
		v.Published = make(map[string]knowledgeDiscoveryEntry, len(v.Cursor.Entries))
	}
	for k, e := range v.Cursor.Entries {
		v.Published[k] = e
	}
	return v, b, nil
}
func (a *App) knowledgeProgressiveArea(ctx context.Context, g *knowledgeGraph, cache *knowledgeScanCache, root *os.Root, ar knowledgeArea, scope, module string, p knowledgeIndexPolicy, fileQuota, dirQuota int, includeCode bool, codeFiles *[]knowledgeCodeFile, codeBytes, codeSkipped *int) {
	if err := a.knowledgeLocalResume(cache, ar, p, includeCode, fileQuota, dirQuota); err != nil {
		g.Warnings = append(g.Warnings, ar.name+"：本地索引检查点未恢复，重新扫描")
	}
	v, b, err := cache.discover(ctx, root, ar, p, fileQuota, dirQuota)
	if err != nil {
		cache.fail()
		return
	}
	coverage := &knowledgeCoverage{FileBudget: fileQuota, DirectoryBudget: dirQuota, DepthBudget: p.DepthBudget, TextBytesPerFile: 8192, Reasons: b.Reasons, ScopePaths: append([]string(nil), p.ScopePaths[ar.id()]...), Progressive: true, Pending: b.Pending, Cycle: v.Cycle, CatalogLimit: v.Cursor.MaxEntries}
	paths := make([]string, 0, len(v.Published))
	for k := range v.Published {
		paths = append(paths, k)
	}
	sort.Strings(paths)
	first := len(g.Nodes)
	blocked := map[string]bool{}
	for _, rel := range paths {
		if ctx.Err() != nil {
			cache.fail()
			return
		}
		denied := false
		for parent := path.Dir(rel); ; parent = path.Dir(parent) {
			if blocked[parent] {
				denied = true
				break
			}
			if parent == "." {
				break
			}
		}
		if denied {
			continue
		}
		e := v.Published[rel]
		info := e.Info
		nid := knowledgeID(scope, ar.root, ar.source, rel)
		node := knowledgeNode{ID: nid, Name: path.Base(rel), Kind: "file", Region: ar.region(), Root: ar.root, Source: ar.source, Path: rel, Size: info.Size(), Modified: info.ModTime().UTC().Format(time.RFC3339Nano)}
		if info.IsDir() {
			current, statErr := root.Lstat(rel)
			if os.IsNotExist(statErr) || statErr == nil && (!current.IsDir() || current.Mode()&os.ModeSymlink != 0) {
				blocked[rel] = true
				continue
			}
			if statErr != nil {
				cache.fail()
				return
			}
			node.Kind = "directory"
			node.Size = 0
			coverage.Directories++
			if rel == "." {
				node.Name = ar.name
			}
		} else {
			coverage.Files++
			if _, seen := v.Cursor.Entries[rel]; seen {
				current, statErr := root.Lstat(rel)
				if os.IsNotExist(statErr) || statErr == nil && (!current.Mode().IsRegular() || current.Mode()&os.ModeSymlink != 0) {
					delete(v.Cursor.Entries, rel)
					delete(v.Published, rel)
					coverage.Files--
					continue
				}
				if statErr != nil {
					cache.fail()
					return
				}
				info = current
				node.Size = info.Size()
				node.Modified = info.ModTime().UTC().Format(time.RFC3339Nano)
				v.Cursor.Entries[rel] = knowledgeDiscoveryEntry{rel, info}
				v.Published[rel] = knowledgeDiscoveryEntry{rel, info}
			}
			wantCode := includeProgressiveCode(rel, info, len(*codeFiles), *codeBytes, includeCode)
			// Only observations from this cycle may read source data. Older regions
			// remain visible until enumeration completes; their memo is a prior snapshot.
			key := ar.location + "\x00" + rel
			var text, code string
			if _, seen := v.Cursor.Entries[rel]; seen {
				text, code, err = cache.material(root, ar.location, rel, info, wantCode)
			} else if memo, ok := cache.previous[key]; ok {
				text, code = memo.text, memo.code
				if !wantCode {
					memo.code, memo.codeReady = "", false
					code = ""
				}
				cache.files[key] = memo
			} else {
				cache.fail()
				return
			}
			if err != nil {
				cache.fail()
				return
			}
			node.Text = text
			if wantCode {
				*codeBytes += len(code)
				*codeFiles = append(*codeFiles, knowledgeCodeFile{ID: nid, Text: code, Node: ar.node(node), Module: module})
			} else if includeCode && codeLanguage(rel) != "" {
				(*codeSkipped)++
				g.Truncated = true
			}
		}
		g.Nodes = append(g.Nodes, ar.node(node))
		if rel != "." {
			parent := path.Dir(rel)
			if _, ok := v.Published[parent]; ok {
				g.Edges = append(g.Edges, knowledgeEdge{From: knowledgeID(scope, ar.root, ar.source, parent), To: nid, Kind: "contains"})
			}
		}
	}
	cache.localCheckpoints = append(cache.localCheckpoints, knowledgeLocalCheckpoint{ar, knowledgeIndexRevision(p), includeCode, fileQuota, dirQuota, v})
	state, message := "ready", ""
	if !b.Complete || len(b.Reasons) > 0 {
		state, message = "partial", "分批索引中，未扫描区域保持上一周期快照"
		g.Truncated = true
	}
	knowledgeSetSource(g, ar, state, message, len(g.Nodes)-first)
	for i := range g.Sources {
		if g.Sources[i].Region == ar.region() {
			g.Sources[i].Coverage = coverage
			break
		}
	}
}
func includeProgressiveCode(p string, info os.FileInfo, count, size int, enabled bool) bool {
	lang := codeLanguage(p)
	return enabled && lang != "" && lang != "unsupported" && count < 80 && info.Size() <= 256*1024 && size+int(info.Size()) <= 4*1024*1024
}
