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

// Keep metadata catalogues bounded even when progressive indexing discovers
// very large trees. The local checkpoint codec enforces the same ceiling.
const knowledgeDiscoveryMaxEntries = 20000

// Read-ahead names are persisted in the encrypted cursor so small progressive
// steps do not have to reopen a large directory and replay its processed prefix.
const knowledgeDiscoveryReadAheadNames = 2048

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
	ReadOffset   int
	PendingNames []string
	ReadDone     bool
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
	for i := range c.Queue {
		c.Queue[i].PendingNames = append([]string(nil), d.Queue[i].PendingNames...)
	}
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

func (d *knowledgeDiscovery) inspectName(root *os.Root, parent, name string, depth int, files *int, children *[]knowledgeDiscoveryCursor) (consume, limitReached bool, err error) {
	consume = true
	if knowledgeSkip(name) {
		return
	}
	rel := path.Join(parent, name)
	if !knowledgeSafePath(rel) {
		return
	}
	current, statErr := root.Lstat(rel)
	if os.IsNotExist(statErr) {
		return
	}
	if statErr != nil {
		return false, false, statErr
	}
	if !knowledgeInScope(rel, d.ScopePaths, current.IsDir()) || current.Mode()&os.ModeSymlink != 0 {
		return
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
				*children = append(*children, knowledgeDiscoveryCursor{Path: rel, Depth: depth + 1})
			}
		}
		return
	}
	if !current.Mode().IsRegular() {
		return
	}
	if _, exists := d.Entries[rel]; !exists && len(d.Entries) >= d.MaxEntries {
		return false, true, nil
	}
	d.Entries[rel] = knowledgeDiscoveryEntry{rel, current}
	*files++
	return
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
			cursor.ReadOffset = 0
			cursor.PendingNames = nil
			cursor.ReadDone = false
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
		// Older checkpoints only stored the number of inspected names. Treat that
		// count as the enumeration offset once, then use the persisted read-ahead.
		if cursor.ReadOffset == 0 && cursor.Offset > 0 && len(cursor.PendingNames) == 0 {
			cursor.ReadOffset = cursor.Offset
		}
		done := false
		limitReached := false
		children := []knowledgeDiscoveryCursor{}
		var readEntries []os.DirEntry
		readEntryIndex := 0
		for b.Files < fileBudget && b.Inspected < entryBudget {
			if err = ctx.Err(); err != nil {
				d.Reasons["time"] = true
				break
			}
			if len(cursor.PendingNames) == 0 && readEntryIndex >= len(readEntries) {
				if cursor.ReadDone {
					done = true
					break
				}
				dir, openErr := root.Open(cursor.Path)
				if openErr != nil {
					return d.result(b), openErr
				}
				// No directory handle survives Step. Resume enumeration at the last
				// read-ahead boundary, not at the last inspected entry.
				skipped := 0
				for skipped < cursor.ReadOffset {
					if err = ctx.Err(); err != nil {
						break
					}
					count := cursor.ReadOffset - skipped
					if count > 256 {
						count = 256
					}
					var entries []os.DirEntry
					entries, err = dir.ReadDir(count)
					skipped += len(entries)
					if err != nil && err != io.EOF {
						break
					}
					if len(entries) == 0 {
						break
					}
				}
				if err != nil && err != io.EOF {
					dir.Close()
					if ctx.Err() != nil {
						d.Reasons["time"] = true
					}
					return d.result(b), err
				}
				if skipped < cursor.ReadOffset {
					dir.Close()
					d.removeTree(cursor.Path)
					cursor.Offset = 0
					cursor.ReadOffset = 0
					cursor.PendingNames = nil
					cursor.ReadDone = false
					cursor.Started = false
					continue
				}
				err = nil
				// Persisted pending names must stay within the checkpoint validator's
				// bound even when this scan step has a much larger entry budget.
				readAhead := knowledgeDiscoveryReadAheadNames
				entries, readErr := dir.ReadDir(readAhead)
				if readErr != nil && readErr != io.EOF {
					dir.Close()
					return d.result(b), readErr
				}
				cursor.ReadOffset += len(entries)
				readEntries = entries
				readEntryIndex = 0
				// File.ReadDir may report EOF only on a subsequent empty read. A
				// short successful batch proves that this directory was exhausted.
				cursor.ReadDone = readErr == io.EOF || len(entries) < readAhead
				dir.Close()
				if len(readEntries) == 0 && cursor.ReadDone {
					done = true
					break
				}
			}
			fromPending := len(cursor.PendingNames) > 0
			name := ""
			if fromPending {
				name = cursor.PendingNames[0]
			} else {
				name = readEntries[readEntryIndex].Name()
			}
			b.Inspected++
			consume, reachedLimit, inspectErr := d.inspectName(root, cursor.Path, name, cursor.Depth, &b.Files, &children)
			if inspectErr != nil {
				err = inspectErr
				consume = false
			}
			if reachedLimit {
				limitReached = true
			}
			if consume {
				cursor.Offset++
				if fromPending {
					cursor.PendingNames = cursor.PendingNames[1:]
				} else {
					readEntryIndex++
				}
			}
			if err != nil || limitReached {
				break
			}
		}
		for readEntryIndex < len(readEntries) {
			cursor.PendingNames = append(cursor.PendingNames, readEntries[readEntryIndex].Name())
			readEntryIndex++
		}
		if len(cursor.PendingNames) > 0 {
			cursor.PendingNames = append([]string(nil), cursor.PendingNames...)
		} else {
			cursor.PendingNames = nil
		}
		if cursor.ReadDone && len(cursor.PendingNames) == 0 {
			done = true
		}
		if limitReached {
			d.Reasons["catalog_limit"] = true
		}
		if err != nil {
			return d.result(b), err
		}
		if ctx.Err() != nil {
			d.Reasons["time"] = true
			return d.result(b), ctx.Err()
		}
		if limitReached {
			return d.result(b), nil
		}
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
