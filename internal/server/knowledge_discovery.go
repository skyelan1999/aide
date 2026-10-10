package server

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
)

// Discovery retains relative cursors and bounded metadata, never open directory
// handles or source text. The caller pins one workspace/source and owns the
// instance; Clone permits speculative scans without mutating the published view.
type knowledgeDiscoveryEntry struct {
	Path string
	Info os.FileInfo
}
type knowledgeDiscoveryCursor struct {
	Path         string
	Offset       int
	Stamp        knowledgeFileStamp
	Started      bool
	Depth        int
	MetadataOnly bool
	Direct       bool
}
type knowledgeDiscovery struct {
	Queue      []knowledgeDiscoveryCursor
	Entries    map[string]knowledgeDiscoveryEntry
	Visited    map[string]bool
	Reasons    map[string]bool
	Complete   bool
	MaxEntries int
	ScopePaths []string
	Seeded     bool
}
type knowledgeDiscoveryBatch struct {
	Files       int
	Directories int
	Inspected   int
	Pending     int
	Complete    bool
	Reasons     []string
}

func newKnowledgeDiscovery(maxEntries int) *knowledgeDiscovery {
	if maxEntries < 1 {
		maxEntries = 50000
	}
	return &knowledgeDiscovery{Queue: []knowledgeDiscoveryCursor{{Path: "."}}, Entries: map[string]knowledgeDiscoveryEntry{}, Visited: map[string]bool{}, Reasons: map[string]bool{}, MaxEntries: maxEntries}
}

// Seed priority targets before enumerating the source root. Ancestors contribute
// metadata only: discovering a deep target must not enumerate its siblings.
func (d *knowledgeDiscovery) seed(priorities []string) {
	if d.Seeded {
		return
	}
	d.Seeded = true
	if len(priorities) == 0 && len(d.ScopePaths) == 0 {
		return
	}
	queue := []knowledgeDiscoveryCursor{}
	ancestors := map[string]bool{}
	targets := map[string]bool{}
	for _, rel := range append(append([]string(nil), priorities...), d.ScopePaths...) {
		if !knowledgeInScope(rel, d.ScopePaths, false) {
			continue
		}
		if rel == "." || !knowledgeSafePath(rel) || path.Clean(rel) != rel {
			continue
		}
		denied := false
		for _, part := range strings.Split(rel, "/") {
			if knowledgeSkip(part) {
				denied = true
				break
			}
		}
		if denied {
			continue
		}
		parts := strings.Split(rel, "/")
		for n := 0; n < len(parts); n++ {
			parent := "."
			if n > 0 {
				parent = strings.Join(parts[:n], "/")
			}
			if !ancestors[parent] {
				ancestors[parent] = true
				queue = append(queue, knowledgeDiscoveryCursor{Path: parent, Depth: n, MetadataOnly: true})
			}
		}
		if !targets[rel] {
			targets[rel] = true
			queue = append(queue, knowledgeDiscoveryCursor{Path: rel, Depth: len(parts) - 1, Direct: true})
		}
	}
	if len(d.ScopePaths) > 0 {
		d.Queue = queue
	} else {
		d.Queue = append(queue, d.Queue...)
	}
}
func knowledgeDiscoveryAncestors(root *os.Root, rel string) (bool, error) {
	for parent := path.Dir(rel); parent != "."; parent = path.Dir(parent) {
		info, err := root.Lstat(parent)
		if os.IsNotExist(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return false, nil
		}
	}
	return true, nil
}
func (d *knowledgeDiscovery) Clone() *knowledgeDiscovery {
	c := *d
	c.ScopePaths = append([]string(nil), d.ScopePaths...)
	c.Queue = append([]knowledgeDiscoveryCursor(nil), d.Queue...)
	c.Entries = make(map[string]knowledgeDiscoveryEntry, len(d.Entries))
	for k, v := range d.Entries {
		c.Entries[k] = v
	}
	c.Visited = make(map[string]bool, len(d.Visited))
	for k, v := range d.Visited {
		c.Visited[k] = v
	}
	c.Reasons = make(map[string]bool, len(d.Reasons))
	for k, v := range d.Reasons {
		c.Reasons[k] = v
	}
	return &c
}
func (d *knowledgeDiscovery) result(b knowledgeDiscoveryBatch) knowledgeDiscoveryBatch {
	b.Pending = len(d.Queue)
	b.Complete = d.Complete
	for k := range d.Reasons {
		b.Reasons = append(b.Reasons, k)
	}
	sort.Strings(b.Reasons)
	return b
}

// Step bounds admitted files, newly entered directories AND inspected entries,
// so a huge excluded directory cannot consume an unbounded request. Depth is a
// scope limit, not a claim that the entire source is covered. No retries write.
func (d *knowledgeDiscovery) Step(ctx context.Context, root *os.Root, fileBudget, dirBudget, depthBudget, entryBudget int, priorities []string) (knowledgeDiscoveryBatch, error) {
	b := knowledgeDiscoveryBatch{}
	if fileBudget < 1 || dirBudget < 1 || depthBudget < 0 || entryBudget < 1 {
		return b, fmt.Errorf("invalid discovery budget")
	}
	if d.Complete {
		return d.result(b), nil
	}
	d.seed(priorities)
	delete(d.Reasons, "time")
	delete(d.Reasons, "catalog_limit")
	for len(d.Queue) > 0 && b.Files < fileBudget && b.Inspected < entryBudget {
		if err := ctx.Err(); err != nil {
			d.Reasons["time"] = true
			return d.result(b), err
		}
		cursor := &d.Queue[0]
		if cursor.Depth > depthBudget {
			d.Reasons["depth"] = true
			d.Queue = d.Queue[1:]
			continue
		}
		allowed, ancestorErr := knowledgeDiscoveryAncestors(root, cursor.Path)
		if ancestorErr != nil {
			return d.result(b), ancestorErr
		}
		if !allowed {
			d.removeTree(cursor.Path)
			d.Queue = d.Queue[1:]
			continue
		}
		info, err := root.Lstat(cursor.Path)
		if os.IsNotExist(err) {
			if cursor.Direct {
				for _, scope := range d.ScopePaths {
					if scope == cursor.Path {
						d.Reasons["scope_missing"] = true
					}
				}
			}
			d.removeTree(cursor.Path)
			d.Queue = d.Queue[1:]
			continue
		}
		if err != nil {
			return d.result(b), err
		}
		if cursor.Direct && info.IsDir() {
			cursor.Depth = strings.Count(cursor.Path, "/") + 1
			if cursor.Depth > depthBudget {
				d.Reasons["depth"] = true
				d.Queue = d.Queue[1:]
				continue
			}
		}
		if cursor.Direct && info.Mode().IsRegular() {
			if _, exists := d.Entries[cursor.Path]; !exists && len(d.Entries) >= d.MaxEntries {
				d.Reasons["catalog_limit"] = true
				return d.result(b), nil
			}
			d.Entries[cursor.Path] = knowledgeDiscoveryEntry{cursor.Path, info}
			b.Files++
			b.Inspected++
			d.Queue = d.Queue[1:]
			continue
		}
		if cursor.MetadataOnly && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			if _, exists := d.Entries[cursor.Path]; !exists {
				if b.Directories >= dirBudget {
					break
				}
				if len(d.Entries) >= d.MaxEntries {
					d.Reasons["catalog_limit"] = true
					return d.result(b), nil
				}
				d.Entries[cursor.Path] = knowledgeDiscoveryEntry{cursor.Path, info}
				b.Directories++
				b.Inspected++
			}
			d.Queue = d.Queue[1:]
			continue
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			d.removeTree(cursor.Path)
			d.Queue = d.Queue[1:]
			continue
		}
		stamp := knowledgeStamp(info)
		if cursor.Started && cursor.Stamp != stamp {
			// An insertion/removal changes enumeration offsets. Restart only that
			// directory, removing stale children; stable IDs will be deduplicated.
			d.removeTree(cursor.Path)
			cursor.Offset = 0
			cursor.Started = false
		}
		if !cursor.Started {
			if b.Directories >= dirBudget {
				break
			}
			if len(d.Entries) >= d.MaxEntries {
				d.Reasons["catalog_limit"] = true
				return d.result(b), nil
			}
			cursor.Started = true
			cursor.Stamp = stamp
			b.Directories++
			d.Entries[cursor.Path] = knowledgeDiscoveryEntry{cursor.Path, info}
			d.Visited[cursor.Path] = true
		}
		dir, err := root.Open(cursor.Path)
		if err != nil {
			return d.result(b), err
		}
		// Reopen at each step. No handle survives request cancellation or switching.
		skipped := 0
		for skipped < cursor.Offset {
			if err = ctx.Err(); err != nil {
				dir.Close()
				d.Reasons["time"] = true
				return d.result(b), err
			}
			count := cursor.Offset - skipped
			if count > 256 {
				count = 256
			}
			var entries []os.DirEntry
			entries, err = dir.ReadDir(count)
			skipped += len(entries)
			if err != nil {
				break
			}
		}
		if err != nil && err != io.EOF {
			dir.Close()
			return d.result(b), err
		}
		if skipped < cursor.Offset {
			dir.Close()
			cursor.Offset = 0
			cursor.Started = false
			continue
		}
		// One entry at a time avoids losing buffered entries when a budget is hit.
		done := false
		limitReached := false
		children := []knowledgeDiscoveryCursor{}
		for b.Files < fileBudget && b.Inspected < entryBudget {
			if err = ctx.Err(); err != nil {
				break
			}
			var entries []os.DirEntry
			entries, err = dir.ReadDir(1)
			if len(entries) == 0 {
				done = err == io.EOF
				break
			}
			e := entries[0]
			cursor.Offset++
			b.Inspected++
			if knowledgeSkip(e.Name()) || e.Type()&os.ModeSymlink != 0 {
				continue
			}
			rel := path.Join(cursor.Path, e.Name())
			if !knowledgeSafePath(rel) {
				continue
			}
			current, readErr := root.Lstat(rel)
			if os.IsNotExist(readErr) {
				continue
			}
			if readErr != nil {
				err = readErr
				break
			}
			if !knowledgeInScope(rel, d.ScopePaths, current.IsDir()) {
				continue
			}
			if current.Mode()&os.ModeSymlink != 0 {
				continue
			}
			if current.IsDir() {
				if !d.Visited[rel] {
					d.Visited[rel] = true
					queued := false
					for _, pending := range d.Queue {
						if pending.Path == rel {
							queued = true
							break
						}
					}
					if !queued {
						children = append(children, knowledgeDiscoveryCursor{Path: rel, Depth: cursor.Depth + 1})
					}
				}
				continue
			}
			if !current.Mode().IsRegular() {
				continue
			}
			if _, exists := d.Entries[rel]; !exists && len(d.Entries) >= d.MaxEntries {
				// Keep this entry pending for an explicit higher catalog limit.
				cursor.Offset--
				d.Reasons["catalog_limit"] = true
				limitReached = true
				break
			}
			d.Entries[rel] = knowledgeDiscoveryEntry{rel, current}
			b.Files++
		}
		dir.Close()
		sort.SliceStable(children, func(i, j int) bool {
			a, b := knowledgePriorityRank(children[i].Path, priorities), knowledgePriorityRank(children[j].Path, priorities)
			if a != b {
				return a < b
			}
			return children[i].Path < children[j].Path
		})
		d.Queue = append(d.Queue, children...)
		if limitReached {
			return d.result(b), nil
		}
		if err != nil && err != io.EOF {
			if ctx.Err() != nil {
				d.Reasons["time"] = true
			}
			return d.result(b), err
		}
		if done {
			d.Queue = d.Queue[1:]
		}
	}
	if len(d.Queue) == 0 {
		d.Complete = true
	}
	return d.result(b), nil
}
func (d *knowledgeDiscovery) removeTree(p string) {
	for k := range d.Entries {
		if k == p || p == "." || len(k) > len(p) && k[:len(p)+1] == p+"/" {
			delete(d.Entries, k)
		}
	}
	// Pending descendants must be discoverable again after an offset restart.
	for k := range d.Visited {
		if k != p && (p == "." || len(k) > len(p) && k[:len(p)+1] == p+"/") {
			delete(d.Visited, k)
		}
	}
}
