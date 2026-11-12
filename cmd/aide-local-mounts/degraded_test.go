package main

import (
	"aide/internal/localmounts"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestIssue762MountApplyReportsForwardingDegraded(t *testing.T) {
	fixtureDirectoryBridge(t)
	if runtime.GOOS == "windows" {
		t.Skip("POSIX bridge executable fixture; Windows controller compiled separately")
	}
	root := t.TempDir()
	native := filepath.Join(root, ".aide-data/local-mounts")
	if err := os.MkdirAll(native, 0700); err != nil {
		t.Fatal(err)
	}
	previous := localmounts.Empty()
	localmounts.Write(filepath.Join(native, "active.json"), previous)
	localmounts.Write(filepath.Join(native, "compose.json"), map[string]any{"services": map[string]any{}})
	desired := localmounts.Config{Version: 1, Entries: []localmounts.Entry{{ID: "fixture", Name: "fixture", Path: t.TempDir(), ReadOnly: true}}}
	localmounts.Write(filepath.Join(root, ".aide-data/storage/data/local-mounts/desired.json"), desired)
	docker := filepath.Join(root, "docker-compose")
	os.WriteFile(docker, []byte("#!/bin/sh\nif [ \"$1\" = config ];then echo '{\"services\":{\"aide\":{\"volumes\":[]}}}';fi\n"), 0700)
	t.Setenv("AIDE_LOCAL_MOUNTS_COMPOSE", docker)
	os.Mkdir(filepath.Join(root, "scripts"), 0700)
	script := filepath.Join(root, "scripts/message-lan-control.sh")
	os.WriteFile(script, []byte("#!/bin/sh\nexit 1\n"), 0700)
	prior := confirmApplication
	confirmApplication = func(context.Context) error { return nil }
	defer func() { confirmApplication = prior }()
	err := apply(context.Background(), root, docker, localmounts.Revision(desired))
	var warning mountAppliedWarning
	if !errors.As(err, &warning) {
		t.Fatalf("committed mount apply lost degraded status: %v", err)
	}
	result := response{ID: "fixture", State: "done"}
	setResponseError(&result, err)
	if result.State != "done" || result.Error != "" || result.Warning == "" {
		t.Fatalf("wrong mailbox state: %+v", result)
	}
	active, err := localmounts.Read(filepath.Join(native, "active.json"))
	if err != nil || localmounts.Revision(active) != localmounts.Revision(desired) {
		t.Fatal("committed mounts were rolled back", err)
	}
	os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0700)
	if err := refreshForwarders(context.Background(), root, docker); err != nil {
		t.Fatal("forwarding retry failed", err)
	}
	ordinary := response{State: "done"}
	setResponseError(&ordinary, errors.New("real apply failure"))
	if ordinary.State != "error" || ordinary.Warning != "" {
		t.Fatal("ordinary failure was hidden")
	}
}
