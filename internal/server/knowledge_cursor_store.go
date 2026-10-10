package server

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const knowledgeCursorBytes = 16 << 20

type knowledgeCursorPayload struct {
	Version   int
	Identity  string
	Discovery *knowledgeRemoteDiscovery
}

// Caller holds a.mu. Process epochs and socket generations are deliberately
// excluded: persistent identity follows configuration, not runtime handles.
func (a *App) knowledgeCursorIdentityLocked(ar knowledgeArea, policy string, code bool, files, dirs int) string {
	password, key := a.sourceCredentialLocked(ar.source)
	b, _ := json.Marshal([]any{ar.scope, ar.root, ar.source, ar.kind, ar.location, ar.enabled, ar.src, ar.ws, password, key, policy, code, files, dirs})
	defer zeroBytes(b)
	return hash(b)
}
func knowledgeCursorFile(data string, ar knowledgeArea, code bool) string {
	b, _ := json.Marshal([]any{ar.scope, ar.root, ar.source, code})
	return filepath.Join(data, "cache", "knowledge-cursors", hash(b)+".json")
}
func validateKnowledgeRemoteCursor(d *knowledgeRemoteDiscovery, ar knowledgeArea) error {
	if d == nil || d.Cycle < 1 || d.Visited == nil || d.Reasons == nil || d.Current == nil || d.Published == nil || d.Code == nil || d.CurrentCode == nil || len(d.Queue) > 50000 || len(d.Visited) > 50000 || len(d.Current) > 50000 || len(d.Published) > 50000 || len(d.Reasons) > 16 || d.Complete != (len(d.Queue) == 0) {
		return fmt.Errorf("invalid cursor bounds")
	}
	safe := func(p string) bool {
		if !knowledgeSafePath(p) || path.Clean(p) != p {
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
		if !safe(q.Path) || q.Offset < 0 || q.Depth < 0 || q.Depth > 64 || (q.Stamp != "" && len(q.Stamp) != 64) {
			return fmt.Errorf("invalid directory cursor")
		}
	}
	for p := range d.Visited {
		if !safe(p) {
			return fmt.Errorf("invalid visited path")
		}
	}
	for _, nodes := range []map[string]knowledgeNode{d.Current, d.Published} {
		for p, n := range nodes {
			if !safe(p) || n.Path != p || n.Root != ar.root || n.Source != ar.source || n.ID != knowledgeID(ar.scope, ar.root, ar.source, p) || (n.Kind != "file" && n.Kind != "directory") || len(n.Text) > 8192 {
				return fmt.Errorf("invalid cached node")
			}
			n.Origin, n.SourceName, n.SourceType = ar.origin, ar.name, ar.kind
			nodes[p] = n
		}
	}
	for _, files := range []map[string]knowledgeCodeFile{d.Code, d.CurrentCode} {
		bytes := 0
		if len(files) > 80 {
			return fmt.Errorf("invalid code count")
		}
		for p, f := range files {
			bytes += len(f.Text)
			if !safe(p) || len(f.Text) > 256*1024 || bytes > 4<<20 || f.Node.Path != p || f.ID != knowledgeID(ar.scope, ar.root, ar.source, p) || f.Node.ID != f.ID {
				return fmt.Errorf("invalid cached code")
			}
			f.Node.Origin, f.Node.SourceName, f.Node.SourceType = ar.origin, ar.name, ar.kind
			files[p] = f
		}
	}
	return nil
}
func loadKnowledgeCursor(file, identity string, ar knowledgeArea, key []byte) (*knowledgeRemoteDiscovery, error) {
	info, err := os.Lstat(file)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > 24<<20 {
		return nil, fmt.Errorf("invalid cursor file")
	}
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, (24<<20)+1))
	if err != nil || len(b) > 24<<20 {
		return nil, fmt.Errorf("cursor file exceeds capacity")
	}
	var env knowledgeVectorEnvelope
	if json.Unmarshal(b, &env) != nil || env.Version != 1 {
		return nil, fmt.Errorf("invalid cursor envelope")
	}
	plain, err := openWithKey(key, env.Ciphertext, env.Nonce)
	if err != nil {
		return nil, fmt.Errorf("cursor decrypt failed")
	}
	defer zeroBytes(plain)
	var payload knowledgeCursorPayload
	if len(plain) > knowledgeCursorBytes || json.Unmarshal(plain, &payload) != nil || payload.Version != 1 {
		return nil, fmt.Errorf("invalid cursor payload")
	}
	if payload.Identity != identity {
		return nil, nil
	}
	if err := validateKnowledgeRemoteCursor(payload.Discovery, ar); err != nil {
		return nil, err
	}
	return payload.Discovery, nil
}
func saveKnowledgeCursor(file, identity string, d *knowledgeRemoteDiscovery, key []byte) error {
	plain, err := json.Marshal(knowledgeCursorPayload{1, identity, d})
	if err != nil {
		return err
	}
	defer zeroBytes(plain)
	if len(plain) > knowledgeCursorBytes {
		return fmt.Errorf("cursor exceeds capacity")
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
		return fmt.Errorf("invalid cursor directory")
	}
	if info, err := os.Lstat(file); err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("invalid cursor destination")
	}
	return atomicJSON(file, knowledgeVectorEnvelope{1, ct, nonce})
}
func (a *App) knowledgeCursorCheckpoint(ar knowledgeArea, policy string, code bool, files, dirs int, save *knowledgeRemoteDiscovery) (*knowledgeRemoteDiscovery, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if ar.epoch != a.wsRevision || a.settings.UserPasswordHash != "" && a.loadLockStateLocked().Locked {
		return nil, fmt.Errorf("cursor workspace changed or locked")
	}
	identity := a.knowledgeCursorIdentityLocked(ar, policy, code, files, dirs)
	file := knowledgeCursorFile(a.dataPath, ar, code)
	key := knowledgeTimeKey("cursor-v1\x00" + a.token)
	if save != nil {
		if err := validateKnowledgeRemoteCursor(save.clone(), ar); err != nil {
			return nil, err
		}
		return nil, saveKnowledgeCursor(file, identity, save, key)
	}
	return loadKnowledgeCursor(file, identity, ar, key)
}
