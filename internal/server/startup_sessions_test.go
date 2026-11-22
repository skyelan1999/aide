package server

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestStartupSessionCacheRechecksCurrentBytesAndTypedFields(t *testing.T) {
	cache := &startupSessionCache{}
	first := []byte(`{"id":"a","title":"old","runs":[]}`)
	one, err := cache.decode("session", first)
	if err != nil {
		t.Fatal(err)
	}
	two, err := cache.decode("session", append([]byte(nil), first...))
	if err != nil || one != two || cache.decoded != 1 || cache.reused != 1 {
		t.Fatal("identical bytes were not reused")
	}
	// Same path and byte count must never authorize reuse after a content change.
	changed := bytes.Replace(first, []byte("old"), []byte("new"), 1)
	three, err := cache.decode("session", changed)
	if err != nil || three == one || three.Title != "new" {
		t.Fatal("changed bytes reused a stale session")
	}
	if _, err := cache.decode("session", []byte(`{"id":"a","runs":"not an array"}`)); err == nil {
		t.Fatal("typed corruption accepted")
	}
	if _, err := cache.decode("session", []byte(`{"id":"a","runs":[{"stopRequested":"not a bool"}]}`)); err == nil {
		t.Fatal("nested typed corruption accepted")
	}
}

func TestStartupUnchangedSessionPreservesBytesAndModificationTime(t *testing.T) {
	a := testApp(t)
	s := mustPostSession(t, a, "unchanged history")
	work, reference, data := a.workPath, a.refPath, a.dataPath
	a.Close()
	path := SessionPath(data, s.ID, "active")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// An unknown forward-compatible field also detects a needless re-encoding.
	before = append(bytes.TrimSpace(before[:bytes.LastIndexByte(before, '}')]), []byte(",\n\"futureField\":true}\n")...)
	if err := os.WriteFile(path, before, 0600); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(1600000000, 0)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(work, reference, data)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || !bytes.Equal(before, after) || !info.ModTime().Equal(stamp) {
		t.Fatal("unchanged session was rewritten")
	}
	if restarted.sessions[s.ID] == nil {
		t.Fatal("session missing after restart")
	}
}

func TestStartupInterruptedSessionRecoveryIsStillPersisted(t *testing.T) {
	a := testApp(t)
	s := mustPostSession(t, a, "interrupted history")
	a.mu.Lock()
	live := a.sessions[s.ID]
	live.Runs = []*Task{nil, {ID: newID(), Status: "running", ApprovalState: "reviewing", StopRequested: true}}
	if err := a.save(live); err != nil {
		a.mu.Unlock()
		t.Fatal(err)
	}
	a.mu.Unlock()
	work, reference, data := a.workPath, a.refPath, a.dataPath
	a.Close()
	restarted, err := New(work, reference, data)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	raw, err := os.ReadFile(SessionPath(data, s.ID, "active"))
	if err != nil {
		t.Fatal(err)
	}
	var saved Session
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.Runs) != 2 || saved.Runs[1].Status != "cancelled" || saved.Runs[1].CanResume || saved.Runs[1].ApprovalState != "" {
		t.Fatal("interrupted stop intent was not persisted")
	}
}

func TestStartupCacheReleasedAtFirstReadyHealth(t *testing.T) {
	a := &App{startupSessions: &startupSessionCache{}}
	a.confirmImageIntegrityUpgrade()
	if a.startupSessions != nil {
		t.Fatal("startup cache retained beyond readiness")
	}
}

func BenchmarkStartupSessionValidation(b *testing.B) {
	data, err := json.Marshal(&Session{ID: "benchmark", Messages: []Message{{Role: "assistant", Content: strings.Repeat("historical tool output ", 50000)}}})
	if err != nil {
		b.Fatal(err)
	}
	for _, reuse := range []bool{false, true} {
		name := "full-typed-decode"
		if reuse {
			name = "fresh-byte-hash-and-reuse"
		}
		b.Run(name, func(b *testing.B) {
			var cache *startupSessionCache
			if reuse {
				cache = &startupSessionCache{}
				if _, err := cache.decode("session", data); err != nil {
					b.Fatal(err)
				}
			}
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := cache.decode("session", data); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
