package server

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestKnowledgeRemoteProgressiveCursor(t *testing.T) {
	ar := knowledgeArea{root: "source", source: "remote", name: "remote", kind: "sftp", scope: "w", origin: "origin"}
	p := defaultKnowledgeIndexPolicy()
	p.ScopePaths = map[string][]string{"remote": {"selected"}}
	p.PriorityPaths = map[string][]string{"remote": {"selected/important.md"}}
	listing := map[string][]map[string]any{".": {{"name": "selected", "dir": true}, {"name": "excluded.md", "size": int64(5)}}, "selected": {}}
	for i := 0; i < 5; i++ {
		listing["selected"] = append(listing["selected"], map[string]any{"name": fmt.Sprintf("file-%d.md", i), "size": int64(5)})
	}
	listing["selected"] = append(listing["selected"], map[string]any{"name": "important.md", "size": int64(5)})
	read := func(p string, _ int64) ([]byte, error) { return []byte(p), nil }
	list := func(p string) ([]map[string]any, error) { return append([]map[string]any(nil), listing[p]...), nil }
	d := newKnowledgeRemoteDiscovery()
	step := func() knowledgeRemoteResult {
		t.Helper()
		candidate := d.clone()
		r, err := candidate.step(context.Background(), ar, p, false, 2, 1, list, read)
		if err != nil {
			t.Fatal(err)
		}
		d = candidate
		return r
	}
	r := step()
	if r.coverage.TotalKnown || r.coverage.Pending == 0 {
		t.Fatalf("premature complete: %+v", r.coverage)
	}
	r = step()
	if r.coverage.Files != 2 {
		t.Fatalf("first file batch: %+v", r.coverage)
	}
	found := false
	for _, n := range r.nodes {
		if n.Path == "selected/important.md" {
			found = true
		}
		if n.Path == "excluded.md" {
			t.Fatal("scope leak")
		}
	}
	if !found {
		t.Fatal("priority not first batch")
	}
	for !d.Complete {
		r = step()
	}
	if r.coverage.Files != 6 || !r.coverage.TotalKnown {
		t.Fatalf("not accumulated: %+v", r.coverage)
	}
	listing["selected"] = listing["selected"][:5]
	for i := 0; i < 5; i++ {
		r = step()
		if d.Complete {
			break
		}
	}
	if r.coverage.Files != 5 || !r.coverage.TotalKnown || r.coverage.Cycle != 2 {
		t.Fatalf("refresh deletion: %+v", r.coverage)
	}
	before := d.clone()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	candidate := d.clone()
	if _, err := candidate.step(ctx, ar, p, false, 2, 1, list, read); err == nil {
		t.Fatal("cancel accepted")
	}
	if len(d.Published) != len(before.Published) || d.Cycle != before.Cycle {
		t.Fatal("candidate mutated published state")
	}
}

func TestKnowledgeRemoteProgressiveCodeBudget(t *testing.T) {
	ar := knowledgeArea{root: "workspace", scope: "test", kind: "ssh"}
	p := defaultKnowledgeIndexPolicy()
	d := newKnowledgeRemoteDiscovery()
	body := strings.Repeat("x", 256*1024)
	phase := 0
	list := func(string) ([]map[string]any, error) {
		entries := make([]map[string]any, 17)
		for i := range entries {
			entries[i] = map[string]any{"name": fmt.Sprintf("phase-%d-%02d.go", phase, i), "size": int64(len(body))}
		}
		return entries, nil
	}
	read := func(string, int64) ([]byte, error) { return []byte(body), nil }
	for phase = 0; phase < 2; phase++ {
		for i := 0; i < 20; i++ {
			r, err := d.step(context.Background(), ar, p, true, 1, 1, list, read)
			if err != nil {
				t.Fatal(err)
			}
			for _, files := range []map[string]knowledgeCodeFile{d.Code, d.CurrentCode} {
				bytes := 0
				for _, f := range files {
					bytes += len(f.Text)
				}
				if bytes > 4<<20 || len(files) > 80 {
					t.Fatal("code cache exceeded checkpoint budget")
				}
			}
			if err := validateKnowledgeRemoteCursor(d.clone(), ar); err != nil {
				t.Fatalf("unrestorable bounded cursor: %v", err)
			}
			if d.Complete {
				if r.coverage.TotalKnown || !d.Reasons["code-budget"] {
					t.Fatal("code limit concealed")
				}
				if len(d.Published) != 18 {
					t.Fatal("completed metadata catalogue retained missing old files")
				}
				for name := range d.Published {
					if name != "." && !strings.HasPrefix(name, fmt.Sprintf("phase-%d-", phase)) {
						t.Fatal("old code-limited generation survived enumeration")
					}
				}
				break
			}
			if i == 19 {
				t.Fatal("cycle did not finish")
			}
		}
	}
}

func TestKnowledgeRemoteProgressiveCodeCount(t *testing.T) {
	ar := knowledgeArea{root: "workspace", scope: "test", kind: "ssh"}
	p := defaultKnowledgeIndexPolicy()
	d := newKnowledgeRemoteDiscovery()
	list := func(string) ([]map[string]any, error) {
		entries := make([]map[string]any, 81)
		for i := range entries {
			entries[i] = map[string]any{"name": fmt.Sprintf("code-%02d.go", i), "size": int64(1)}
		}
		return entries, nil
	}
	var r knowledgeRemoteResult
	for i := 0; i < 4 && !d.Complete; i++ {
		var err error
		r, err = d.step(context.Background(), ar, p, true, 32, 1, list, func(string, int64) ([]byte, error) { return []byte("x"), nil })
		if err != nil {
			t.Fatal(err)
		}
	}
	if !d.Complete || len(d.CurrentCode) != 80 || len(d.Code) != 80 || r.coverage.Files != 81 || r.coverage.TotalKnown || !d.Reasons["code-budget"] {
		t.Fatal("code count limit dropped file metadata or concealed partial code coverage")
	}
	if err := validateKnowledgeRemoteCursor(d.clone(), ar); err != nil {
		t.Fatal(err)
	}
}

func TestKnowledgeRemoteProgressiveReadFailure(t *testing.T) {
	ar := knowledgeArea{root: "workspace", kind: "ssh", scope: "w"}
	p := defaultKnowledgeIndexPolicy()
	d := newKnowledgeRemoteDiscovery()
	candidate := d.clone()
	_, err := candidate.step(context.Background(), ar, p, false, 2, 2, func(string) ([]map[string]any, error) {
		return []map[string]any{{"name": "a.md", "size": int64(1)}}, nil
	}, func(string, int64) ([]byte, error) { return nil, fmt.Errorf("disconnected") })
	if err == nil || len(d.Published) != 0 || d.Queue[0].Offset != 0 {
		t.Fatal("failed scan changed committed cursor")
	}
}

func TestKnowledgeRemoteProgressiveDirectoryMutation(t *testing.T) {
	ar := knowledgeArea{root: "workspace", kind: "ssh", scope: "w"}
	p := defaultKnowledgeIndexPolicy()
	entries := []map[string]any{{"name": "a.md", "size": int64(1)}, {"name": "b.md", "size": int64(1)}, {"name": "c.md", "size": int64(1)}}
	list := func(string) ([]map[string]any, error) { return append([]map[string]any(nil), entries...), nil }
	read := func(string, int64) ([]byte, error) { return []byte("x"), nil }
	d := newKnowledgeRemoteDiscovery()
	step := func() knowledgeRemoteResult {
		t.Helper()
		r, err := d.step(context.Background(), ar, p, false, 1, 1, list, read)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	step()
	entries = entries[1:]
	r := step()
	if r.coverage.TotalKnown || d.Cycle != 2 || len(d.Current) != 0 {
		t.Fatalf("changed listing did not restart candidate: %+v", r.coverage)
	}
	for i := 0; i < 4 && !d.Complete; i++ {
		r = step()
	}
	if !r.coverage.TotalKnown || r.coverage.Files != 2 {
		t.Fatalf("stable replacement cycle incomplete: %+v", r.coverage)
	}
	if _, ok := d.Published["a.md"]; ok {
		t.Fatal("removed file survived completed cycle")
	}
}

func TestKnowledgeRemoteProgressiveLimits(t *testing.T) {
	for _, reason := range []string{"depth-budget", "catalog-limit"} {
		t.Run(reason, func(t *testing.T) {
			ar := knowledgeArea{root: "workspace", kind: "ssh", scope: "w"}
			p := defaultKnowledgeIndexPolicy()
			p.FileBudget, p.DirectoryBudget, p.DepthBudget = 1, 1, 0
			list := func(string) ([]map[string]any, error) {
				if reason == "depth-budget" {
					return []map[string]any{{"name": "deep", "dir": true}}, nil
				}
				entries := make([]map[string]any, 1100)
				for i := range entries {
					entries[i] = map[string]any{"name": fmt.Sprintf("f-%04d.bin", i), "size": int64(1)}
				}
				return entries, nil
			}
			d := newKnowledgeRemoteDiscovery()
			var r knowledgeRemoteResult
			for i := 0; i < 40 && !d.Complete; i++ {
				var err error
				r, err = d.step(context.Background(), ar, p, false, 10000, 2, list, func(string, int64) ([]byte, error) { t.Fatal("binary/deep file read"); return nil, nil })
				if err != nil {
					t.Fatal(err)
				}
			}
			if !d.Complete || r.coverage.TotalKnown || r.state != "partial" || !d.Reasons[reason] {
				t.Fatalf("limit reported as complete coverage: %+v", r.coverage)
			}
			if len(d.Published) > r.coverage.CatalogLimit || r.coverage.FileBudget != 32 {
				t.Fatal("unbounded catalogue or file batch")
			}
		})
	}
}
