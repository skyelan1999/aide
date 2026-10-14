package server

import (
	"context"
	"fmt"
	"os"
	"path"
	"sort"
	"time"
)

type knowledgeDiscoveryView struct {
	Cursor    *knowledgeDiscovery
	Published map[string]knowledgeDiscoveryEntry
	Cycle     int
	Material  knowledgeMaterialProgress
}

func (v *knowledgeDiscoveryView) clone() *knowledgeDiscoveryView {
	c := &knowledgeDiscoveryView{Cursor: v.Cursor.Clone(), Published: make(map[string]knowledgeDiscoveryEntry, len(v.Published)), Cycle: v.Cycle}
	c.Material = v.Material.clone()
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
	// A directory that outgrows its first catalogue should keep indexing on
	// subsequent snapshots. Grow the retained catalogue geometrically, bounded
	// by the same hard limit enforced by the durable local checkpoint format.
	// This keeps the first view responsive without making the first batch's
	// capacity look like a permanent source-size limit.
	if v.Cursor.Reasons["catalog_limit"] && v.Cursor.MaxEntries < knowledgeDiscoveryMaxEntries {
		next := max(v.Cursor.MaxEntries*2, v.Cursor.MaxEntries+4*(files+dirs))
		v.Cursor.MaxEntries = min(knowledgeDiscoveryMaxEntries, next)
	}
	// A budget increase should extend the catalog in place. Replacing the
	// cursor here would discard its directory offsets and rescan from the root.
	if limit := 4 * (files + dirs); limit > v.Cursor.MaxEntries {
		v.Cursor.MaxEntries = limit
	}
	v.prepareMaterial()
	if v.Cursor.Complete && v.materialPending() == 0 {
		v.Cursor = newKnowledgeDiscovery(v.Cursor.MaxEntries)
		v.Cycle++
		v.prepareMaterial()
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
	v.pruneMaterial()
	return v, b, nil
}
func (a *App) knowledgeProgressiveArea(ctx context.Context, g *knowledgeGraph, cache *knowledgeScanCache, root *os.Root, ar knowledgeArea, scope, module string, p knowledgeIndexPolicy, fileQuota, dirQuota int, includeCode bool, codeFiles *[]knowledgeCodeFile, codeBytes, codeSkipped *int) {
	originalError := cache.err
	readFailureMessage := ""
	var coverage *knowledgeCoverage
	defer func() {
		if originalError == nil && cache.err != nil && a.knowledgeContextErr(ctx) == nil {
			cause := cache.err
			cache.err = nil
			cache.localSourceFailure(g, ar, localReadUnavailableMessage(cause), coverage)
		}
	}()
	if err := a.knowledgeLocalResume(cache, ar, p, includeCode, fileQuota, dirQuota); err != nil {
		g.Warnings = append(g.Warnings, ar.name+"：本地索引检查点未恢复，重新扫描")
	}
	v, b, err := cache.discover(ctx, root, ar, p, fileQuota, dirQuota)
	if err != nil {
		cache.fail(err)
		return
	}
	coverage = &knowledgeCoverage{FileBudget: fileQuota, DirectoryBudget: dirQuota, DepthBudget: p.DepthBudget, TextBytesPerFile: 8192, Reasons: b.Reasons, ScopePaths: append([]string(nil), p.ScopePaths[ar.id()]...), Progressive: true, Pending: b.Pending, Cycle: v.Cycle, CatalogLimit: v.Cursor.MaxEntries}
	paths := make([]string, 0, len(v.Published))
	for k := range v.Published {
		paths = append(paths, k)
	}
	sort.Strings(paths)
	if err := a.knowledgeMaterialStep(ctx, cache, root, ar, v, fileQuota, dirQuota, includeCode, len(*codeFiles), *codeBytes); err != nil {
		cache.fail(err)
		if a.knowledgeContextErr(ctx) != nil {
			return
		}
	}
	first := len(g.Nodes)
	for _, rel := range paths {
		if ctx.Err() != nil {
			cache.fail()
			return
		}
		e, exists := v.Published[rel]
		if !exists {
			continue
		}
		info := e.Info
		nid := knowledgeID(scope, ar.root, ar.source, rel)
		node := knowledgeNode{ID: nid, Name: path.Base(rel), Kind: "file", Region: ar.region(), Root: ar.root, Source: ar.source, Path: rel, Size: info.Size(), Modified: info.ModTime().UTC().Format(time.RFC3339Nano)}
		if info.IsDir() {
			node.Kind = "directory"
			node.Size = 0
			coverage.Directories++
			if rel == "." {
				node.Name = ar.name
			}
		} else {
			coverage.Files++
			key := ar.location + "\x00" + rel
			memo, ready := cache.files[key]
			if !ready || memo.stamp != knowledgeStamp(info) {
				memo, ready = cache.previous[key]
			}
			ready = ready && memo.stamp == knowledgeStamp(info)
			wantCode := includeProgressiveCode(rel, info, len(*codeFiles), *codeBytes, includeCode) && ready && memo.codeReady
			if gap, failed := v.Material.Unavailable[rel]; failed && gap.Stamp == knowledgeStamp(info) {
				node.ReadState = "unavailable"
				coverage.ReadErrors++
				ready = false
				wantCode = false
				if readFailureMessage == "" {
					readFailureMessage = localReadFailureMessage(gap.Kind)
				}
			} else if !ready {
				node.ReadState = "pending"
				coverage.ReadPending++
				delete(v.Material.Validated, rel)
			}
			if ready {
				node.Text = memo.text
				if !wantCode {
					memo.code = ""
					memo.codeReady = false
				}
				cache.files[key] = memo
			}
			if wantCode {
				*codeBytes += len(memo.code)
				*codeFiles = append(*codeFiles, knowledgeCodeFile{ID: nid, Text: memo.code, Node: ar.node(node), Module: module})
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
	coverage.MaterialPending = v.materialPending()
	cache.localCheckpoints = append(cache.localCheckpoints, knowledgeLocalCheckpoint{ar, knowledgeLocalDiscoveryRevision(p), includeCode, 0, 0, v})
	state, message := "ready", ""
	if !b.Complete || len(b.Reasons) > 0 || coverage.MaterialPending > 0 {
		state, message = "partial", "分批索引中，未扫描区域保持上一周期快照"
		g.Truncated = true
	}
	if coverage.ReadErrors > 0 {
		state = "partial"
		message = fmt.Sprintf("%d 个文件正文暂不可读取，已继续索引其他文件；%s", coverage.ReadErrors, readFailureMessage)
		coverage.Reasons = append(coverage.Reasons, "file-read-error")
		g.Truncated = true
		g.Warnings = append(g.Warnings, ar.name+"："+message)
	}
	if coverage.MaterialPending > 0 {
		coverage.Reasons = append(coverage.Reasons, "material_pending")
		message += fmt.Sprintf("；%d 个条目等待分批复核，未复核区域保留上次观察", coverage.MaterialPending)
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
