package main

import (
	"aide/internal/localmounts"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestApplyRejectsStaleAndCancelled(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".aide-data/storage/data/local-mounts")
	c := localmounts.Empty()
	localmounts.Write(filepath.Join(dir, "desired.json"), c)
	prior := confirmApplication
	defer func() { confirmApplication = prior }()
	called := false
	confirmApplication = func(context.Context) error { called = true; return errors.New("cancelled") }
	if apply(context.Background(), root, "must-not-execute", "old") == nil || called {
		t.Fatal("stale plan reached confirmation")
	}
	if apply(context.Background(), root, "must-not-execute", localmounts.Revision(c)) == nil || !called {
		t.Fatal("cancelled plan reached Docker")
	}
}

// Optional real-Docker integration runs ONLY against this explicitly named
// disposable fixture. OS confirmation is replaced in the test binary, never
// by a production runtime flag or an API option.
func TestApplyIsolatedBrowserFixture(t *testing.T) {
	root := os.Getenv("AIDE_MOUNT_APPLY_FIXTURE")
	if root == "" {
		t.Skip("explicit disposable Docker fixture required")
	}
	b, err := os.ReadFile(filepath.Join(root, "compose.yaml"))
	if err != nil || !strings.Contains(string(b), `"container_name": "aide-mount-acceptance-20261022"`) {
		t.Fatal("refuse non-fixture root")
	}
	if err = os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, ".aide-data/storage/data/local-mounts")
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	prior := confirmApplication
	defer func() { confirmApplication = prior }()
	confirmApplication = func(context.Context) error { return nil }
	for {
		localmounts.Write(filepath.Join(dir, "host.json"), map[string]any{"updated": time.Now().Unix(), "os": "darwin"})
		raw, _ := os.ReadFile(filepath.Join(dir, "request.json"))
		var r request
		if json.Unmarshal(raw, &r) == nil && r.Action == "apply" && r.Created >= time.Now().Unix()-5 {
			localmounts.Write(filepath.Join(dir, "response.json"), response{ID: r.ID, State: "running"})
			err = apply(ctx, root, os.Getenv("AIDE_MOUNT_DOCKER"), r.Revision)
			reply := response{ID: r.ID, State: "done"}
			if err != nil {
				reply.Error = err.Error()
				reply.State = "error"
			}
			localmounts.Write(filepath.Join(dir, "response.json"), reply)
			if err != nil {
				t.Fatal(err)
			}
			t.Log("real Docker apply from authenticated browser request passed; OS confirmation simulated only in test binary")
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(200 * time.Millisecond):
		}
	}
}
