package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionCandidateKeepsMetadataAndRechecksBytes(t *testing.T) {
	data := t.TempDir()
	dir := filepath.Join(data, "sessions", "archived")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	s := Session{ID: "large-candidate", Archived: true, Created: "2026-01-01", Updated: "2026-01-02", Messages: []Message{{Role: "user", Content: strings.Repeat("history", 10000)}}, Runs: []*Task{{RequestSnapshots: []RequestSnapshot{{Body: json.RawMessage(`{"image":"` + strings.Repeat("large-image", 100000) + `"}`)}}}}}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "session-large-candidate.json")
	if err := os.WriteFile(p, b, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := scanSessionCandidates(data)
	if err != nil || len(got) != 1 {
		t.Fatalf("scan: %v %d", err, len(got))
	}
	c := got[0]
	if c.sess.ID != s.ID || c.sess.Updated != s.Updated || sessionBucketFor(c.sess) != "archived" || len(c.sess.Runs) != 0 || len(c.sess.Messages) != 0 {
		t.Fatal("candidate retained history or lost bucket/winner metadata")
	}
	exact, err := readSessionCandidate(c)
	if err != nil || string(exact) != string(b) {
		t.Fatalf("backup bytes changed: %v", err)
	}
	if err := os.WriteFile(p, append(b, ' '), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readSessionCandidate(c); err == nil {
		t.Fatal("migration accepted a candidate modified after planning")
	}
	// A JSON-valid file with an invalid typed run must still be skipped in place.
	bad := filepath.Join(dir, "session-malformed.json")
	if err := os.WriteFile(bad, []byte(`{"id":"malformed","runs":"invalid"}`), 0600); err != nil {
		t.Fatal(err)
	}
	got, err = scanSessionCandidates(data)
	if err != nil || len(got) != 1 {
		t.Fatalf("typed corruption no longer rejected: %v %d", err, len(got))
	}
	if _, err := os.Stat(bad); err != nil {
		t.Fatal("corrupt source was removed")
	}
}
