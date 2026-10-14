package server

import (
	"context"
	"os"
	"path"
	"sort"
	"time"
)

// These are observations and fair queue positions, not cached OS errors. A
// failed body is physically retried when its bounded queue turn comes again.
type knowledgeMaterialGap struct {
	Stamp knowledgeFileStamp
	Kind  string
}
type knowledgeMaterialProgress struct {
	Cycle               int
	FileAfter, DirAfter string
	Validated           map[string]knowledgeFileStamp
	Unavailable         map[string]knowledgeMaterialGap
}

func (m knowledgeMaterialProgress) clone() knowledgeMaterialProgress {
	c := m
	c.Validated = make(map[string]knowledgeFileStamp, len(m.Validated))
	c.Unavailable = make(map[string]knowledgeMaterialGap, len(m.Unavailable))
	for k, v := range m.Validated {
		c.Validated[k] = v
	}
	for k, v := range m.Unavailable {
		c.Unavailable[k] = v
	}
	return c
}
func (v *knowledgeDiscoveryView) prepareMaterial() {
	if v.Material.Cycle != v.Cycle || v.Material.Validated == nil {
		v.Material.Cycle = v.Cycle
		v.Material.Validated = map[string]knowledgeFileStamp{}
	}
	if v.Material.Unavailable == nil {
		v.Material.Unavailable = map[string]knowledgeMaterialGap{}
	}
}
func (v *knowledgeDiscoveryView) pruneMaterial() {
	for p := range v.Material.Validated {
		if _, ok := v.Published[p]; !ok {
			delete(v.Material.Validated, p)
		}
	}
	for p := range v.Material.Unavailable {
		if _, ok := v.Published[p]; !ok {
			delete(v.Material.Unavailable, p)
		}
	}
}
func (v *knowledgeDiscoveryView) materialPending() int {
	n := 0
	for p, e := range v.Published {
		if stamp, ok := v.Material.Validated[p]; !ok || stamp != knowledgeStamp(e.Info) {
			n++
		}
	}
	return n
}
func materialQueue(paths []string, after string, limit int) []string {
	if len(paths) == 0 {
		return nil
	}
	start := sort.Search(len(paths), func(i int) bool { return paths[i] > after })
	result := make([]string, 0, min(len(paths), max(1, limit)))
	for i := 0; i < min(len(paths), max(1, limit)); i++ {
		result = append(result, paths[(start+i)%len(paths)])
	}
	return result
}
func (v *knowledgeDiscoveryView) dropMaterialTree(rel string) {
	v.Cursor.removeTree(rel)
	for p := range v.Published {
		if p == rel || rel == "." || len(p) > len(rel) && p[:len(rel)+1] == rel+"/" {
			delete(v.Published, p)
			delete(v.Material.Validated, p)
			delete(v.Material.Unavailable, p)
		}
	}
}

// Bound filesystem work independently of catalogue size. Reconstruction below
// can retain older verified material without reopening every known path.
func (a *App) knowledgeMaterialStep(ctx context.Context, cache *knowledgeScanCache, root *os.Root, ar knowledgeArea, v *knowledgeDiscoveryView, files, dirs int, includeCode bool, codeCount, codeSize int) error {
	v.prepareMaterial()
	filePaths, dirPaths := []string{}, []string{}
	for p, e := range v.Published {
		if e.Info.IsDir() {
			dirPaths = append(dirPaths, p)
		} else {
			filePaths = append(filePaths, p)
		}
	}
	sort.Strings(filePaths)
	sort.Strings(dirPaths)
	wantCode := map[string]bool{}
	for _, p := range filePaths {
		e := v.Published[p]
		if gap, ok := v.Material.Unavailable[p]; ok && gap.Stamp == knowledgeStamp(e.Info) {
			continue
		}
		if includeProgressiveCode(p, e.Info, codeCount, codeSize, includeCode) {
			wantCode[p] = true
			codeCount++
			codeSize += int(e.Info.Size())
		}
	}
	checked := map[string]bool{}
	dirErrors := map[string]error{}
	var firstErr error
	var validateDir func(string) (bool, error)
	validateDir = func(p string) (bool, error) {
		if err := a.knowledgeContextErr(ctx); err != nil {
			return false, err
		}
		if err := dirErrors[p]; err != nil {
			return false, err
		}
		if value, ok := checked[p]; ok {
			return value, nil
		}
		if err := a.knowledgeContextErr(ctx); err != nil {
			return false, err
		}
		info, err := root.Lstat(p)
		if os.IsNotExist(err) || err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
			v.dropMaterialTree(p)
			checked[p] = false
			return false, nil
		}
		if err != nil {
			dirErrors[p] = err
			return false, err
		}
		checked[p] = true
		if _, ok := v.Published[p]; ok {
			v.Published[p] = knowledgeDiscoveryEntry{p, info}
			v.Material.Validated[p] = knowledgeStamp(info)
			if _, seen := v.Cursor.Entries[p]; seen {
				v.Cursor.Entries[p] = knowledgeDiscoveryEntry{p, info}
			}
		}
		return true, nil
	}
	started := time.Now()
	for i, p := range materialQueue(filePaths, v.Material.FileAfter, files) {
		if i > 0 && time.Since(started) >= knowledgeBatchDuration {
			break
		}
		if err := a.knowledgeContextErr(ctx); err != nil {
			return err
		}
		v.Material.FileAfter = p
		allowed := true
		for parent := path.Dir(p); parent != "."; parent = path.Dir(parent) {
			ok, err := validateDir(parent)
			if err != nil {
				if a.knowledgeContextErr(ctx) != nil {
					return err
				}
				if firstErr == nil {
					firstErr = err
				}
				allowed = false
				break
			}
			if !ok {
				allowed = false
				break
			}
		}
		if !allowed {
			continue
		}
		if err := a.knowledgeContextErr(ctx); err != nil {
			return err
		}
		info, err := root.Lstat(p)
		if os.IsNotExist(err) || err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
			v.dropMaterialTree(p)
			continue
		}
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		v.Published[p] = knowledgeDiscoveryEntry{p, info}
		if _, seen := v.Cursor.Entries[p]; seen {
			v.Cursor.Entries[p] = knowledgeDiscoveryEntry{p, info}
		}
		_, _, err = cache.material(root, ar.location, p, info, wantCode[p])
		if stopped := a.knowledgeContextErr(ctx); stopped != nil {
			return stopped
		}
		v.Material.Validated[p] = knowledgeStamp(info)
		if err != nil {
			cache.err = nil
			key := ar.location + "\x00" + p
			delete(cache.files, key)
			delete(cache.previous, key)
			v.Material.Unavailable[p] = knowledgeMaterialGap{knowledgeStamp(info), localReadFailureKind(err)}
		} else {
			delete(v.Material.Unavailable, p)
		}
	}
	started = time.Now()
	for i, p := range materialQueue(dirPaths, v.Material.DirAfter, dirs) {
		if i > 0 && time.Since(started) >= knowledgeBatchDuration {
			break
		}
		v.Material.DirAfter = p
		if _, err := validateDir(p); err != nil {
			if a.knowledgeContextErr(ctx) != nil {
				return err
			}
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}
