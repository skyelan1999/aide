package server

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

type knowledgeCursorProcessInput struct {
	Work, Reference, Data, Source, Receipt string
	Files, Dirs                            int
}
type knowledgeCursorProcessReceipt struct {
	Before, After int
	Origin        string
}

// Invoked only by the real isolated SSH acceptance test as a fresh process.
// Paths and identifiers refer exclusively to that test's temporary data.
func TestSSHKnowledgeCursorRestartHelper(t *testing.T) {
	raw := os.Getenv("AIDE_TEST_CURSOR_PROCESS")
	if raw == "" {
		t.Skip("isolated subprocess helper")
	}
	var input knowledgeCursorProcessInput
	if err := json.Unmarshal([]byte(raw), &input); err != nil {
		t.Fatal(err)
	}
	a, err := New(input.Work, input.Reference, input.Data)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.mu.Lock()
	var area knowledgeArea
	for _, ar := range a.knowledgeAreasLocked(a.wsID(), a.workspace.Name(), a.reference.Name()) {
		if ar.source == input.Source {
			area = ar
		}
	}
	policy, err := a.loadKnowledgeIndexPolicy()
	a.mu.Unlock()
	if err != nil || area.source == "" {
		t.Fatalf("reloaded source/policy missing: %v", err)
	}
	d, err := a.knowledgeCursorCheckpoint(area, knowledgeIndexRevision(policy), false, input.Files, input.Dirs, nil)
	if err != nil || d == nil {
		t.Fatalf("fresh process checkpoint not restored: %v", err)
	}
	before := d.snapshot(area, policy, input.Files, input.Dirs)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	after := a.knowledgeRemoteCached(ctx, area, false, input.Files, input.Dirs)
	if after.state == "unavailable" || after.coverage == nil || after.coverage.Files <= before.coverage.Files {
		t.Fatalf("fresh process did not resume real SSH: state=%s before=%d", after.state, before.coverage.Files)
	}
	for _, n := range after.nodes {
		if n.Origin != area.origin {
			t.Fatal("old process origin survived restart")
		}
		if n.Kind == "file" {
			b, err := a.knowledgeAreaRead(ctx, area, n.Path, 1024)
			if err != nil || string(b) != "real remote progressive evidence" {
				t.Fatalf("live SSH read after restart failed: %v", err)
			}
			break
		}
	}
	receipt, err := json.Marshal(knowledgeCursorProcessReceipt{before.coverage.Files, after.coverage.Files, area.origin})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input.Receipt, receipt, 0600); err != nil {
		t.Fatal(err)
	}
}
