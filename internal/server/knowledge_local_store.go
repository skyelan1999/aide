package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// No OS handles survive restart. The DTO retains the bounded observations and
// materials necessary to keep older regions visible while a new cycle advances.
type knowledgeLocalStamp struct {
	Size, Date int64
	Mode       os.FileMode
}

func localStamp(s knowledgeFileStamp) knowledgeLocalStamp {
	return knowledgeLocalStamp{s.size, s.date, s.mode}
}
func (s knowledgeLocalStamp) stamp() knowledgeFileStamp {
	return knowledgeFileStamp{s.Size, s.Date, s.Mode}
}

type knowledgeLocalInfo struct {
	Rel   string
	Stamp knowledgeLocalStamp
}

func (s knowledgeLocalInfo) Name() string       { return path.Base(s.Rel) }
func (s knowledgeLocalInfo) Size() int64        { return s.Stamp.Size }
func (s knowledgeLocalInfo) ModTime() time.Time { return time.Unix(0, s.Stamp.Date) }
func (s knowledgeLocalInfo) Mode() os.FileMode  { return s.Stamp.Mode }
func (s knowledgeLocalInfo) IsDir() bool        { return s.Mode().IsDir() }
func (s knowledgeLocalInfo) Sys() any           { return nil }

type knowledgeLocalQueue struct {
	Path                 string
	Offset               int
	Stamp                knowledgeLocalStamp
	Started              bool
	Depth                int
	MetadataOnly, Direct bool
}
type knowledgeLocalMaterial struct {
	Stamp      knowledgeLocalStamp
	Text, Code string
	CodeReady  bool
}
type knowledgeLocalPayload struct {
	Version            int
	Identity           string
	Cycle              int
	Queue              []knowledgeLocalQueue
	Entries, Published map[string]knowledgeLocalInfo
	Visited, Reasons   map[string]bool
	Complete           bool
	MaxEntries         int
	ScopePaths         []string
	Seeded             bool
	Materials          map[string]knowledgeLocalMaterial
}
type knowledgeLocalCheckpoint struct {
	Area        knowledgeArea
	Policy      string
	Code        bool
	Files, Dirs int
	View        *knowledgeDiscoveryView
}

func knowledgeDiscoveryKey(ar knowledgeArea) string {
	return ar.region() + "\x00" + ar.location + "\x00" + ar.origin
}
func knowledgeLocalCursorFile(data string, ar knowledgeArea, code bool) string {
	return filepath.Join(filepath.Dir(knowledgeCursorFile(data, ar, code)), "local-"+filepath.Base(knowledgeCursorFile(data, ar, code)))
}
func localPayload(identity string, v *knowledgeDiscoveryView, materials map[string]knowledgeFileMemo, location string) knowledgeLocalPayload {
	d := v.Cursor
	out := knowledgeLocalPayload{Version: 1, Identity: identity, Cycle: v.Cycle, Entries: map[string]knowledgeLocalInfo{}, Published: map[string]knowledgeLocalInfo{}, Visited: d.Visited, Reasons: d.Reasons, Complete: d.Complete, MaxEntries: d.MaxEntries, ScopePaths: d.ScopePaths, Seeded: d.Seeded, Materials: map[string]knowledgeLocalMaterial{}}
	for _, q := range d.Queue {
		out.Queue = append(out.Queue, knowledgeLocalQueue{q.Path, q.Offset, localStamp(q.Stamp), q.Started, q.Depth, q.MetadataOnly, q.Direct})
	}
	for p, e := range d.Entries {
		out.Entries[p] = knowledgeLocalInfo{p, localStamp(knowledgeStamp(e.Info))}
	}
	for p, e := range v.Published {
		if !e.Info.IsDir() {
			m, ok := materials[location+"\x00"+p]
			if !ok {
				continue
			} // Removed/blocked descendants were not published by this scan.
			out.Materials[p] = knowledgeLocalMaterial{localStamp(m.stamp), m.text, m.code, m.codeReady}
		}
		out.Published[p] = knowledgeLocalInfo{p, localStamp(knowledgeStamp(e.Info))}
	}
	return out
}
func validateKnowledgeLocalCursor(d *knowledgeLocalPayload) error {
	if d.Version != 1 || d.Cycle < 1 || d.MaxEntries < 1 || d.MaxEntries > 20000 || d.Entries == nil || d.Published == nil || d.Visited == nil || d.Reasons == nil || d.Materials == nil || len(d.Queue) > 50000 || len(d.Visited) > 50000 || len(d.Entries) > d.MaxEntries || len(d.Published) > 2*d.MaxEntries || len(d.Materials) > 2*d.MaxEntries || len(d.Reasons) > 16 || len(d.ScopePaths) > 32 || d.Complete != (len(d.Queue) == 0) {
		return fmt.Errorf("invalid local cursor bounds")
	}
	safe := func(p string) bool {
		if len(p) > 4096 || !knowledgeSafePath(p) || path.Clean(p) != p {
			return false
		}
		for _, part := range strings.Split(p, "/") {
			if part != "." && knowledgeSkip(part) {
				return false
			}
		}
		return true
	}
	for _, q := range d.Queue {
		if !safe(q.Path) || q.Offset < 0 || q.Depth < 0 || q.Depth > 64 {
			return fmt.Errorf("invalid local queue")
		}
	}
	for p := range d.Visited {
		if !safe(p) {
			return fmt.Errorf("invalid local visited path")
		}
	}
	for _, p := range d.ScopePaths {
		if !safe(p) {
			return fmt.Errorf("invalid local scope")
		}
	}
	for _, entries := range []map[string]knowledgeLocalInfo{d.Entries, d.Published} {
		for p, e := range entries {
			if !safe(p) || e.Rel != p || e.Size() < 0 || e.Mode()&os.ModeSymlink != 0 || !(e.IsDir() || e.Mode().IsRegular()) {
				return fmt.Errorf("invalid local metadata")
			}
		}
	}
	codeBytes, codeFiles := 0, 0
	for p, m := range d.Materials {
		info, ok := d.Published[p]
		if !ok || info.IsDir() || !safe(p) || m.Stamp != info.Stamp || len(m.Text) > 8192 || len(m.Code) > 256*1024 || !m.CodeReady && m.Code != "" {
			return fmt.Errorf("invalid local material")
		}
		if m.CodeReady {
			codeFiles++
			codeBytes += len(m.Code)
		}
	}
	if codeFiles > 80 || codeBytes > 4<<20 {
		return fmt.Errorf("invalid local code bounds")
	}
	for p, e := range d.Published {
		if !e.IsDir() {
			if _, ok := d.Materials[p]; !ok {
				return fmt.Errorf("missing local material")
			}
		}
	}
	return nil
}
func (d knowledgeLocalPayload) restore(location string) (*knowledgeDiscoveryView, map[string]knowledgeFileMemo) {
	c := newKnowledgeDiscovery(d.MaxEntries)
	c.Queue = nil
	c.Visited = d.Visited
	c.Reasons = d.Reasons
	c.Complete = d.Complete
	c.ScopePaths = d.ScopePaths
	c.Seeded = d.Seeded
	for _, q := range d.Queue {
		c.Queue = append(c.Queue, knowledgeDiscoveryCursor{q.Path, q.Offset, q.Stamp.stamp(), q.Started, q.Depth, q.MetadataOnly, q.Direct})
	}
	for p, e := range d.Entries {
		c.Entries[p] = knowledgeDiscoveryEntry{p, e}
	}
	v := &knowledgeDiscoveryView{Cursor: c, Published: map[string]knowledgeDiscoveryEntry{}, Cycle: d.Cycle}
	for p, e := range d.Published {
		v.Published[p] = knowledgeDiscoveryEntry{p, e}
	}
	materials := map[string]knowledgeFileMemo{}
	for p, m := range d.Materials {
		materials[location+"\x00"+p] = knowledgeFileMemo{m.Stamp.stamp(), m.Text, m.Code, m.CodeReady}
	}
	return v, materials
}
func loadKnowledgeLocalCursor(file, identity string, key []byte) (*knowledgeLocalPayload, error) {
	info, err := os.Lstat(file)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > 24<<20 {
		return nil, fmt.Errorf("invalid local cursor file")
	}
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, (24<<20)+1))
	if err != nil || len(b) > 24<<20 {
		return nil, fmt.Errorf("local cursor exceeds capacity")
	}
	var env knowledgeVectorEnvelope
	if json.Unmarshal(b, &env) != nil || env.Version != 1 {
		return nil, fmt.Errorf("invalid local cursor envelope")
	}
	plain, err := openWithKey(key, env.Ciphertext, env.Nonce)
	if err != nil {
		return nil, fmt.Errorf("local cursor decrypt failed")
	}
	defer zeroBytes(plain)
	var d knowledgeLocalPayload
	if len(plain) > knowledgeCursorBytes || json.Unmarshal(plain, &d) != nil {
		return nil, fmt.Errorf("invalid local cursor payload")
	}
	if d.Identity != identity {
		return nil, nil
	}
	if err := validateKnowledgeLocalCursor(&d); err != nil {
		return nil, err
	}
	return &d, nil
}
func saveKnowledgeLocalCursor(file string, d knowledgeLocalPayload, key []byte) error {
	if err := validateKnowledgeLocalCursor(&d); err != nil {
		return err
	}
	plain, err := json.Marshal(d)
	if err != nil {
		return err
	}
	defer zeroBytes(plain)
	if len(plain) > knowledgeCursorBytes {
		return fmt.Errorf("local cursor exceeds capacity")
	}
	ct, nonce, err := sealWithKey(key, plain)
	if err != nil {
		return err
	}
	dir := filepath.Dir(file)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if info, err := os.Lstat(dir); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("invalid local cursor directory")
	}
	if info, err := os.Lstat(file); err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("invalid local cursor destination")
	}
	return atomicJSON(file, knowledgeVectorEnvelope{1, ct, nonce})
}
func (a *App) knowledgeLocalResume(cache *knowledgeScanCache, ar knowledgeArea, p knowledgeIndexPolicy, code bool, files, dirs int) error {
	key := knowledgeDiscoveryKey(ar)
	if cache.discovery[key] != nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if ar.epoch != a.wsRevision || a.settings.UserPasswordHash != "" && a.loadLockStateLocked().Locked {
		return fmt.Errorf("local cursor workspace changed or locked")
	}
	identity := a.knowledgeCursorIdentityLocked(ar, knowledgeIndexRevision(p), code, files, dirs)
	d, err := loadKnowledgeLocalCursor(knowledgeLocalCursorFile(a.dataPath, ar, code), identity, knowledgeTimeKey("local-cursor-v1\x00"+a.token))
	if err != nil || d == nil {
		return err
	}
	v, materials := d.restore(ar.location)
	cache.discovery[key] = v
	// Never mutate a previous published cache while building a candidate.
	previous := make(map[string]knowledgeFileMemo, len(cache.previous)+len(materials))
	for k, v := range cache.previous {
		previous[k] = v
	}
	for k, v := range materials {
		previous[k] = v
	}
	cache.previous = previous
	return nil
}

// Commit only after a successful scan and final scope/cancellation checks.
func (a *App) knowledgeLocalCommit(ctx context.Context, cache *knowledgeScanCache, epoch uint64) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if ctx.Err() != nil || cache.err != nil || epoch != a.wsRevision || a.settings.UserPasswordHash != "" && a.loadLockStateLocked().Locked {
		return fmt.Errorf("local cursor candidate cancelled, changed or locked")
	}
	for _, c := range cache.localCheckpoints {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		identity := a.knowledgeCursorIdentityLocked(c.Area, c.Policy, c.Code, c.Files, c.Dirs)
		d := localPayload(identity, c.View, cache.files, c.Area.location)
		if err := saveKnowledgeLocalCursor(knowledgeLocalCursorFile(a.dataPath, c.Area, c.Code), d, knowledgeTimeKey("local-cursor-v1\x00"+a.token)); err != nil {
			return err
		}
	}
	return nil
}
