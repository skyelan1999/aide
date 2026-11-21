package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func filenameLookupDecode(t *testing.T, text string) filenameLookupResult {
	t.Helper()
	var result filenameLookupResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatalf("invalid lookup result: %v: %s", err, text)
	}
	return result
}

func TestFilenameLookupRealToolNoContentsOrWriteBaseline(t *testing.T) {
	a, _, task := proposalReadFixture(t)
	for _, name := range []string{"parts/子图/WORLD170.STEP", "parts/records/world169.json", "parts/unrelated.txt", "parts/.aide-data/world-private.json"} {
		file := filepath.Join(a.workPath, name)
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte{0, 255, 0}, 0600); err != nil {
			t.Fatal(err)
		}
	}
	large := filepath.Join(a.workPath, "parts/子图/WORLD170.STEP")
	f, err := os.OpenFile(large, os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(256 << 20); err != nil {
		t.Fatal(err)
	}
	f.Close()
	before, _ := os.Stat(large)
	outside := filepath.Join(t.TempDir(), "world-secret.step")
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(a.workPath, "parts/world-link.step")); err != nil {
		t.Fatal(err)
	}
	text := a.executeToolCall(context.Background(), testToolCall("list_files", `{"path":"parts","glob":"*world*","recursive":true}`), task, nil)
	result := filenameLookupDecode(t, text)
	sort.Strings(result.Matches)
	want := []string{"parts/records/world169.json", "parts/子图/WORLD170.STEP"}
	if !result.Complete || !reflect.DeepEqual(result.Matches, want) || result.SkippedSymlinks != 1 || result.SkippedPolicy != 1 {
		t.Fatalf("wrong scoped name-only result: %+v", result)
	}
	after, _ := os.Stat(large)
	if len(task.readSnapshots) != 0 || len(task.Files) != 0 || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("filename lookup established a write baseline or modified content")
	}
	ordinary := a.executeToolCall(context.Background(), testToolCall("list_files", `{"path":"parts"}`), task, nil)
	if !strings.Contains(ordinary, "unrelated.txt") || !strings.Contains(ordinary, "子图/") || strings.HasPrefix(ordinary, "{") {
		t.Fatalf("ordinary listing compatibility changed: %s", ordinary)
	}
	flat := filenameLookupDecode(t, a.executeToolCall(context.Background(), testToolCall("list_files", `{"path":"parts","glob":"*world*"}`), task, nil))
	if !flat.Complete || len(flat.Matches) != 0 {
		t.Fatalf("nonrecursive lookup escaped its scope: %+v", flat)
	}
}

func TestFilenameLookupRejectsPathsAndArguments(t *testing.T) {
	a := testApp(t)
	for _, args := range []map[string]any{
		{"path": "../outside", "glob": "*"}, {"path": "/tmp", "glob": "*"}, {"path": ".git", "glob": "*"},
		{"glob": "../*"}, {"glob": "bad\\name"}, {"glob": "["}, {"glob": ""}, {"glob": true},
		{"glob": "*", "path": true}, {"glob": "*", "recursive": "yes"}, {"glob": "*", "maxResults": float64(0)},
		{"glob": "*", "maxResults": float64(201)}, {"glob": "*", "maxResults": 2.5},
	} {
		if _, err := lookupFilenames(context.Background(), a.workspace, args); err == nil {
			t.Fatalf("accepted invalid query: %v", args)
		}
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(a.workPath, "linked")); err != nil {
		t.Fatal(err)
	}
	if _, err := lookupFilenames(context.Background(), a.workspace, map[string]any{"path": "linked", "glob": "*", "recursive": true}); err == nil {
		t.Fatal("followed a symlink base")
	}
}

func TestFilenameLookupBudgetsAndCancellationAreExplicit(t *testing.T) {
	a := testApp(t)
	for _, name := range []string{"one.step", "two.step", "deep/next/leaf.step"} {
		f := filepath.Join(a.workPath, name)
		if err := os.MkdirAll(filepath.Dir(f), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name   string
		args   map[string]any
		budget filenameLookupBudget
		reason string
	}{
		{"result", map[string]any{"glob": "*.step", "maxResults": float64(1)}, filenameLookupBudget{100, 10, 1024}, "result_budget"},
		{"entry", map[string]any{"glob": "*", "recursive": true}, filenameLookupBudget{1, 10, 1024}, "entry_budget"},
		{"depth", map[string]any{"glob": "*", "recursive": true}, filenameLookupBudget{100, 0, 1024}, "depth_budget"},
		{"output", map[string]any{"glob": "*"}, filenameLookupBudget{100, 10, 2}, "output_budget"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text, err := lookupFilenamesWithBudget(context.Background(), a.workspace, tc.args, tc.budget)
			if err != nil {
				t.Fatal(err)
			}
			r := filenameLookupDecode(t, text)
			if r.Complete || !strings.Contains(strings.Join(r.Reasons, ","), tc.reason) {
				t.Fatalf("truncation was hidden: %+v", r)
			}
		})
	}
	text, err := lookupFilenames(context.Background(), a.workspace, map[string]any{"glob": "*.step", "maxResults": float64(2)})
	if err != nil {
		t.Fatal(err)
	}
	if r := filenameLookupDecode(t, text); !r.Complete || len(r.Matches) != 2 {
		t.Fatalf("exact result limit falsely truncated: %+v", r)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	text, err = lookupFilenames(ctx, a.workspace, map[string]any{"glob": "*", "recursive": true})
	if err != nil {
		t.Fatal(err)
	}
	if r := filenameLookupDecode(t, text); r.Complete || r.VisitedEntries != 0 || len(r.Matches) != 0 {
		t.Fatalf("cancelled search claimed absence: %+v", r)
	}
}

func TestFilenameLookupReferenceVisibilityAndSSHDoNotFallback(t *testing.T) {
	a, _, task := proposalReadFixture(t)
	if err := os.WriteFile(filepath.Join(a.reference.Name(), "source-world.step"), []byte{0, 0}, 0600); err != nil {
		t.Fatal(err)
	}
	a.sourceRegistry.Sources = append(a.sourceRegistry.Sources, Source{ID: "lookup-ref", Name: "read-only test", Type: "local", Enabled: true})
	call := testToolCall("list_files", `{"source":"lookup-ref","glob":"*world*"}`)
	r := filenameLookupDecode(t, a.executeToolCall(context.Background(), call, task, nil))
	if !r.Complete || len(r.Matches) != 1 || r.Matches[0] != "source-world.step" {
		t.Fatalf("registered reference not scoped: %+v", r)
	}
	a.sourceRegistry.Sources[len(a.sourceRegistry.Sources)-1].AIHiddenWorkspaces = []string{a.wsID()}
	if out := a.executeToolCall(context.Background(), call, task, nil); !strings.Contains(out, "disabled") {
		t.Fatalf("hidden reference bypassed: %s", out)
	}
	task.WorkspaceMode = "ssh"
	if out := a.executeToolCall(context.Background(), testToolCall("list_files", `{"glob":"*"}`), task, nil); !strings.Contains(out, "local workspaces only") {
		t.Fatalf("SSH used local fallback: %s", out)
	}
}
