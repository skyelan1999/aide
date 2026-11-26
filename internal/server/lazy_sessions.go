package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

// This is a disposable catalogue, never a replacement for the session file.
// Startup checks its file stamp; opening history always reads and validates the
// authoritative bytes. The integrity report explicitly counts deferred files.
type sessionCatalogueEntry struct {
	MAC      string   `json:"mac,omitempty"`
	Version  int      `json:"version"`
	Size     int64    `json:"size"`
	Modified int64    `json:"modified"`
	Digest   string   `json:"digest"`
	Metadata *Session `json:"metadata"`
}

type lazySessionStore struct{ data string }
type lazySessionRecord struct {
	path    string
	loading chan struct{}
}

func (l *lazySessionStore) sign(entry sessionCatalogueEntry) string {
	key, err := os.ReadFile(AccessTokenPath(l.data))
	if err != nil || len(key) < 32 {
		return ""
	}
	entry.MAC = ""
	b, err := json.Marshal(entry)
	if err != nil {
		return ""
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("aide-session-catalogue-v1\x00"))
	_, _ = mac.Write(b)
	return hex.EncodeToString(mac.Sum(nil))
}

func (l *lazySessionStore) cataloguePath(path string) string {
	rel, err := filepath.Rel(l.data, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return ""
	}
	sum := sha256.Sum256([]byte(filepath.ToSlash(rel)))
	return filepath.Join(l.data, "cache", "session-catalogue-v1", hex.EncodeToString(sum[:])+".json")
}

func (l *lazySessionStore) metadata(path string) (*Session, string, bool) {
	if l == nil {
		return nil, "", false
	}
	index := l.cataloguePath(path)
	st, err := os.Lstat(index)
	if err != nil || !st.Mode().IsRegular() || st.Size() > 4<<20 {
		return nil, "", false
	}
	f, err := os.Open(index)
	if err != nil {
		return nil, "", false
	}
	b, err := io.ReadAll(io.LimitReader(f, (4<<20)+1))
	f.Close()
	var entry sessionCatalogueEntry
	if err != nil || len(b) > 4<<20 || json.Unmarshal(b, &entry) != nil || entry.Version != 1 || entry.Metadata == nil || !integrityDigestPattern.MatchString(entry.Digest) {
		return nil, "", false
	}
	expected := l.sign(entry)
	if expected == "" || !hmac.Equal([]byte(expected), []byte(entry.MAC)) {
		return nil, "", false
	}
	actual, err := os.Lstat(path)
	if err != nil || !actual.Mode().IsRegular() || actual.Size() != entry.Size || actual.ModTime().UnixNano() != entry.Modified {
		return nil, "", false
	}
	s := entry.Metadata
	if !validSessionID(s.ID) || sessionIDFromName(filepath.Base(path)) != s.ID {
		return nil, "", false
	}
	// Whitelist the catalogue shape rather than accepting history from it.
	meta := sessionCatalogueMetadata(s)
	meta.historyDeferred = true
	return meta, entry.Digest, true
}

func sessionCatalogueMetadata(s *Session) *Session {
	m := &Session{ID: s.ID, Title: s.Title, Preview: sessionListPreview(s), Created: s.Created, Updated: s.Updated, Number: s.Number, Kind: s.Kind, Pinned: s.Pinned, Archived: s.Archived, Deleted: s.Deleted, Checked: s.Checked, ParentID: s.ParentID, AutoArchived: s.AutoArchived, FollowedByAssistant: s.FollowedByAssistant, FollowNote: s.FollowNote, ModelSelectionRevision: s.ModelSelectionRevision}
	// Scalar task summaries support list status, counters and passive polling;
	// they are never executor state and may not be saved as a full session.
	for _, task := range s.Runs {
		if task == nil {
			continue
		}
		view := &Task{}
		src, dst := reflect.ValueOf(task).Elem(), reflect.ValueOf(view).Elem()
		for i := 0; i < src.NumField(); i++ {
			if !dst.Field(i).CanSet() {
				continue
			}
			switch src.Field(i).Kind() {
			case reflect.String:
				dst.Field(i).SetString(clip(src.Field(i).String(), 1024))
			case reflect.Bool, reflect.Int, reflect.Int64, reflect.Uint64, reflect.Float64:
				dst.Field(i).Set(src.Field(i))
			}
		}
		view.Usage = task.Usage
		if view.Status == "running" || view.Status == "awaiting_clarification" {
			view.Status = "interrupted"
			if task.StopRequested {
				view.Status = "cancelled"
			}
		}
		view.ApprovalState = "manual"
		m.Runs = append(m.Runs, view)
	}
	return m
}

func (l *lazySessionStore) record(path string, data []byte, s *Session) {
	if l == nil || s == nil || s.historyDeferred || !validSessionID(s.ID) || sessionIDFromName(filepath.Base(path)) != s.ID {
		return
	}
	st, err := os.Lstat(path)
	if err != nil || !st.Mode().IsRegular() || st.Size() != int64(len(data)) {
		return
	}
	index := l.cataloguePath(path)
	if index == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(index), 0700); err != nil {
		return
	}
	sum := sha256.Sum256(data)
	entry := sessionCatalogueEntry{Version: 1, Size: st.Size(), Modified: st.ModTime().UnixNano(), Digest: hex.EncodeToString(sum[:]), Metadata: sessionCatalogueMetadata(s)}
	entry.MAC = l.sign(entry)
	if entry.MAC == "" {
		return
	}
	if err := atomicJSON(index, entry); err != nil {
		_ = os.Remove(index) // A stale disposable catalogue must not survive a successful payload save.
		log.Printf("session catalogue unavailable: %v", err)
	}
}

func recoverSessionAtStartup(s *Session) bool {
	changed := false
	for _, task := range s.Runs {
		if task == nil {
			continue
		}
		if task.ApprovalState != "" && task.ApprovalState != "manual" {
			task.ApprovalState = "manual"
			changed = true
		}
		if task.Status != "running" && task.Status != "awaiting_clarification" {
			continue
		}
		changed = true
		recoverExecutionCheckpoint(task)
		task.Status, task.PendingQuestion, task.ApprovalState, task.PauseRequested = "interrupted", nil, "", false
		if task.StopRequested {
			task.Status, task.CanResume, task.CheckpointMessages, task.CheckpointStep, task.Error = "cancelled", false, nil, "", "任务已停止"
			continue
		}
		if len(task.CheckpointMessages) > 0 {
			task.CanResume = true
		}
		if task.CanResume {
			task.Error = "服务重启，任务已中断。可以从已保存检查点继续。"
		} else {
			task.Error = "服务重启，任务已中断。可重新提交。"
		}
	}
	return changed
}

func readSessionHistory(path, id string) (*Session, error) {
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() {
		return nil, fmt.Errorf("session history unavailable")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return nil, errors.New("session history changed during loading; retry")
	}
	var s Session
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("session history invalid: %w", err)
	}
	if s.ID != id || s.Deleted {
		return nil, errors.New("session identity changed; catalogue rebuild required")
	}
	return &s, nil
}

// Caller holds a.mu. Synchronous internal/tool access is a correctness fallback;
// HTTP history requests use ensureSessionHistory and perform disk I/O unlocked.
func (a *App) readHistoryFile(path, id string) (*Session, error) {
	if a.sessionHistoryReader != nil {
		return a.sessionHistoryReader(path, id)
	}
	return readSessionHistory(path, id)
}

func (a *App) sessionLocked(id string) *Session {
	s := a.sessions[id]
	if s == nil || !s.historyDeferred {
		return s
	}
	record := a.lazySessions[id]
	if record == nil {
		return nil
	}
	loaded, err := a.readHistoryFile(record.path, id)
	if err != nil {
		log.Printf("session history load failed: %v", err)
		return nil
	}
	changed := recoverSessionAtStartup(loaded)
	previous := *s
	*s = *loaded
	if changed {
		if err := a.save(s); err != nil {
			*s = previous
			log.Printf("session recovery persistence failed: %v", err)
			return nil
		}
	}
	delete(a.lazySessions, id)
	return s
}

func (a *App) ensureSessionHistory(ctx context.Context, id string) error {
	for {
		a.mu.Lock()
		s := a.sessions[id]
		if s == nil || !s.historyDeferred {
			a.mu.Unlock()
			return nil
		}
		record := a.lazySessions[id]
		if record == nil {
			a.mu.Unlock()
			return errors.New("session catalogue unavailable")
		}
		if record.loading != nil {
			wait := record.loading
			a.mu.Unlock()
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		wait := make(chan struct{})
		record.loading = wait
		a.mu.Unlock()
		loaded, err := a.readHistoryFile(record.path, id)
		a.mu.Lock()
		if a.sessions[id] == s && s.historyDeferred && err == nil && ctx.Err() == nil {
			changed := recoverSessionAtStartup(loaded)
			previous := *s
			*s = *loaded
			if changed {
				err = a.save(s)
			}
			if err != nil {
				*s = previous
			} else {
				delete(a.lazySessions, id)
			}
		} else if err == nil && (a.sessions[id] != s || s.Deleted) {
			err = errors.New("session removed while loading")
		}
		record.loading = nil
		close(wait)
		a.mu.Unlock()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
}

func (a *App) sessionBodiesLocked() map[string]*Session {
	out := make(map[string]*Session, len(a.sessions))
	for id := range a.sessions {
		if s := a.sessionLocked(id); s != nil {
			out[id] = s
		}
	}
	return out
}

func (a *App) pendingSessionBodiesLocked() map[string]*Session {
	out := make(map[string]*Session, len(a.sessions))
	for id, s := range a.sessions {
		pending := false
		for _, task := range s.Runs {
			if task != nil {
				switch task.Status {
				case "running", "awaiting_approval", "awaiting_clarification", "queued", "paused":
					pending = true
				}
			}
		}
		if pending && s.historyDeferred {
			s = a.sessionLocked(id)
			if s == nil {
				continue
			}
		}
		out[id] = s
	}
	return out
}

func (a *App) prepareRequestSessionHistory(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path == "/api/export" || r.URL.Path == "/api/search" {
		a.mu.Lock()
		ids := make([]string, 0, len(a.sessions))
		for id := range a.sessions {
			ids = append(ids, id)
		}
		a.mu.Unlock()
		for _, id := range ids {
			if err := a.ensureSessionHistory(r.Context(), id); err != nil {
				fail(w, http.StatusServiceUnavailable, err)
				return false
			}
		}
		return true
	}
	if !strings.HasPrefix(r.URL.Path, "/api/sessions/") {
		return true
	}
	id := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/api/sessions/"), "/", 2)[0]
	if !validSessionID(id) {
		return true
	}
	if err := a.ensureSessionHistory(r.Context(), id); err != nil {
		fail(w, http.StatusServiceUnavailable, err)
		return false
	}
	return true
}
