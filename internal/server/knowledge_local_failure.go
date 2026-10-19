package server

import (
	"errors"
	"os"
	"reflect"
	"syscall"
)

const knowledgeLocalFallbackMaxNodes = knowledgeDiscoveryMaxEntries + 1200
const knowledgeLocalFallbackMaxEdges = 6500

func localReadFailureKind(err error) string {
	if errors.Is(err, syscall.EIO) {
		return "io"
	}
	if errors.Is(err, os.ErrPermission) {
		return "permission"
	}
	if errors.Is(err, os.ErrNotExist) {
		return "changed"
	}
	return "read"
}
func localReadFailureMessage(kind string) string {
	switch kind {
	case "io":
		return localReadUnavailableMessage(syscall.EIO)
	case "permission":
		return localReadUnavailableMessage(os.ErrPermission)
	case "changed":
		return localReadUnavailableMessage(os.ErrNotExist)
	default:
		return localReadUnavailableMessage(nil)
	}
}

func localReadUnavailableMessage(err error) string {
	if errors.Is(err, syscall.EIO) {
		return "本机文件正文读取发生 I/O 错误；云盘占位文件请先下载到本机"
	}
	if errors.Is(err, os.ErrPermission) {
		return "本地来源读取被拒绝，请检查目录权限"
	}
	if errors.Is(err, os.ErrNotExist) {
		return "本地来源目录或文件已变化，等待重新扫描"
	}
	return "本地来源本轮读取未完成，请检查目录和索引范围"
}

// Keep validated prefix nodes and same-origin prior observations. A failed
// source is never rebound; old slices and their coverage remain immutable.
func (c *knowledgeScanCache) localSourceFailure(g *knowledgeGraph, ar knowledgeArea, message string, coverage *knowledgeCoverage) {
	freshFiles := map[string]bool{}
	seen := map[string]knowledgeNode{}
	areaCount := 0
	for _, n := range g.Nodes {
		seen[n.ID] = n
		if n.Region == ar.region() {
			areaCount++
			if n.Kind == "file" {
				freshFiles[n.ID] = true
			}
		}
	}
	retained, clipped := 0, false
	if c != nil && c.priorGraph != nil && c.priorGraph.Workspace == g.Workspace {
		// Parent files first: do not retain stale symbols for a file freshly read in
		// this candidate, or a symbol whose parent did not fit the retained catalogue.
		for pass := 0; pass < 2; pass++ {
			for _, n := range c.priorGraph.Nodes {
				_, exists := seen[n.ID]
				if n.Region != ar.region() || n.Origin != ar.origin || n.Root != ar.root || n.Source != ar.source || exists {
					continue
				}
				if (n.Kind == "symbol") != (pass == 1) {
					continue
				}
				if n.Kind == "symbol" {
					if _, parent := seen[n.ParentFile]; !parent || freshFiles[n.ParentFile] {
						continue
					}
				}
				if areaCount >= knowledgeLocalFallbackMaxNodes {
					clipped = true
					continue
				}
				g.Nodes = append(g.Nodes, n)
				seen[n.ID] = n
				retained++
				areaCount++
			}
		}
		edgeSet := map[string]bool{}
		for _, e := range g.Edges {
			edgeSet[knowledgeEdgeKey(e)] = true
		}
		// Restore old evidence only when both endpoint observations are unchanged.
		// A fresh text/version must not inherit a relation proved by its old contents.
		compatible := map[string]bool{}
		for _, old := range c.priorGraph.Nodes {
			if now, ok := seen[old.ID]; ok && reflect.DeepEqual(old, now) && old.Region == ar.region() && old.Origin == ar.origin {
				compatible[old.ID] = true
			}
		}
		for _, e := range c.priorGraph.Edges {
			key := knowledgeEdgeKey(e)
			if compatible[e.From] && compatible[e.To] && !edgeSet[key] {
				if len(g.Edges) >= knowledgeLocalFallbackMaxEdges {
					clipped = true
					break
				}
				g.Edges = append(g.Edges, e)
				edgeSet[key] = true
			}
		}
		if coverage == nil {
			for _, s := range c.priorGraph.Sources {
				if s.Region == ar.region() && s.Coverage != nil {
					copy := *s.Coverage
					copy.Reasons = append([]string(nil), copy.Reasons...)
					coverage = &copy
					break
				}
			}
		}
	}
	files, dirs, count := 0, 0, 0
	wantedMemos := map[string]bool{}
	for _, n := range g.Nodes {
		if n.Region == ar.region() {
			count++
			if n.Kind == "directory" {
				dirs++
			} else if n.Kind == "file" {
				files++
				wantedMemos[ar.location+"\x00"+n.Path] = true
			}
		}
	}
	state := "unavailable"
	if count > 0 {
		state = "partial"
		if retained > 0 {
			message += "；部分资料保留上次可用观察"
		} else {
			message += "；先显示本轮已读取部分"
		}
	}
	if clipped {
		message += "；缓存观察达到保留上限"
	}
	knowledgeSetSource(g, ar, state, message, count)
	g.Truncated = true
	g.Warnings = append(g.Warnings, ar.name+"："+message)
	if coverage != nil {
		copy := *coverage
		copy.Reasons = append([]string(nil), copy.Reasons...)
		copy.Files = files
		copy.Directories = dirs
		copy.TotalKnown = false
		copy.RefreshProgress = false
		reasons := []string{"source-unavailable"}
		if clipped {
			reasons = append(reasons, "cached-observations-limit")
		}
		for _, reason := range reasons {
			present := false
			for _, prior := range copy.Reasons {
				if prior == reason {
					present = true
				}
			}
			if !present {
				copy.Reasons = append(copy.Reasons, reason)
			}
		}
		for i := range g.Sources {
			if g.Sources[i].Region == ar.region() {
				g.Sources[i].Coverage = &copy
			}
		}
	}
	// Retain retry material only for admitted observations, so repeated failed
	// refreshes cannot accumulate an unbounded history of deleted file contents.
	if c != nil {
		for key := range wantedMemos {
			if memo, ok := c.previous[key]; ok {
				if _, exists := c.files[key]; !exists {
					c.files[key] = memo
				}
			}
		}
	}
}
