package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestKnowledgeGoTypesBindings(t *testing.T) {
	files := []goTypeFile{
		{ID: "Fa", Path: "main.go", Root: "workspace", Module: "example.test/p", Text: `package p
import "example.test/p/lib"
type A struct{}
type B struct{}
func (A) Same(){}
func (B) Same(){}
type I interface{Same()}
func entry(a A,b B,i I,f func()){a.Same();b.Same();i.Same();f();lib.Target();func(){a.Same()}()}
`},
		{ID: "Fb", Path: "lib/lib.go", Root: "workspace", Module: "example.test/p", Text: "package lib\nfunc Target(){}\n"},
	}
	r, err := checkGoTypes(files)
	if err != nil || r.Checked != 2 || len(r.Bindings) != 4 {
		t.Fatalf("%+v %v", r, err)
	}
	targets := map[string]int{}
	for _, b := range r.Bindings {
		targets[b.Target]++
		if b.Line < 1 || b.Column < 1 {
			t.Fatal(b)
		}
	}
	if targets["A.Same"] != 2 || targets["B.Same"] != 1 || targets["Target"] != 1 {
		t.Fatal(targets)
	}
	kfiles := []knowledgeCodeFile{}
	g := knowledgeGraph{}
	for _, f := range files {
		n := knowledgeNode{ID: f.ID, Path: f.Path, Root: f.Root}
		g.Nodes = append(g.Nodes, n)
		kfiles = append(kfiles, knowledgeCodeFile{ID: f.ID, Text: f.Text, Module: f.Module, Node: n})
	}
	expandKnowledgeCode(context.Background(), &g, kfiles)
	applyKnowledgeGoTypes(&g, r)
	if g.Code.TypedCalls != 4 {
		t.Fatalf("graph %+v", g.Code)
	}
	unresolved := 0
	for _, v := range g.Code.UnresolvedRelations {
		if v.Name == "i.Same" || v.Name == "<variable>.f" {
			unresolved++
		}
	}
	if unresolved != 2 {
		t.Fatal("dynamic calls incorrectly promoted", g.Code.UnresolvedRelations)
	}
	for _, diagnostic := range g.Code.Diagnostics {
		if strings.Contains(diagnostic, "a.Same（") || strings.Contains(diagnostic, "b.Same（") {
			t.Fatal("resolved call retained stale unresolved diagnostic", diagnostic)
		}
	}
	for _, e := range g.Edges {
		if e.Kind == "call_typed" && e.Confidence != "type_binding" {
			t.Fatal(e)
		}
	}
}

func TestKnowledgeGoTypesResolvedDiagnosticsByColumn(t *testing.T) {
	resolved := knowledgeUnresolvedRelation{From: "caller", File: "file", Line: 9, Column: 12, Name: "value.Compute", Kind: "call"}
	dynamic := resolved
	dynamic.Column = 33
	dynamic.Reason = "dynamic_binding"
	g := knowledgeGraph{
		Nodes: []knowledgeNode{
			{ID: "file", Kind: "file", Path: "main.go"},
			{ID: "caller", Kind: "symbol", Language: "Go", ParentFile: "file", Name: "entry"},
			{ID: "target", Kind: "symbol", Language: "Go", ParentFile: "file", Name: "Primary.Compute", Line: 5},
		},
		Code: &knowledgeCodeSummary{Unresolved: 2, UnresolvedRelations: []knowledgeUnresolvedRelation{resolved, dynamic}, Diagnostics: []string{
			knowledgeUnresolvedCallDiagnostic("main.go", resolved),
			knowledgeUnresolvedCallDiagnostic("main.go", dynamic),
			"unrelated syntax error",
		}},
	}
	applyKnowledgeGoTypes(&g, goTypeResult{Bindings: []goTypeBinding{{File: "file", Caller: "entry", Line: 9, Column: 12, TargetFile: "file", Target: "Primary.Compute", TargetLine: 5}}})
	if g.Code.TypedCalls != 1 || g.Code.Unresolved != 1 || len(g.Code.Diagnostics) != 2 {
		t.Fatalf("unexpected summary: %+v", g.Code)
	}
	if g.Code.Diagnostics[0] != "AST: "+knowledgeUnresolvedCallDiagnostic("main.go", dynamic) || g.Code.Diagnostics[1] != "AST: unrelated syntax error" {
		t.Fatal("removed unresolved call on the same line or unrelated error", g.Code.Diagnostics)
	}
}

func TestKnowledgeGoTypesFailClosed(t *testing.T) {
	for _, test := range []struct{ name, text, bad string }{
		{"syntax", "package p\nfunc target(){}\nfunc entry(){target()}\n", "package p\nfunc broken("},
		{"type", "package p\nfunc target(){}\nfunc entry(){target();missing()}\n", ""},
		{"external", "package p\nimport \"outside.test/module\"\nfunc entry(){module.Call()}\n", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			files := []goTypeFile{{ID: "F", Path: "a.go", Root: "workspace", Text: test.text}}
			if test.bad != "" {
				files = append(files, goTypeFile{ID: "bad", Path: "b.go", Root: "workspace", Text: test.bad})
			}
			r, e := checkGoTypes(files)
			if e != nil || r.Checked != 0 || len(r.Bindings) != 0 || len(r.Diagnostics) == 0 {
				t.Fatalf("%+v %v", r, e)
			}
		})
	}
	files := []goTypeFile{{ID: "Fa", Path: "a.go", Root: "workspace", Source: "one", Text: "package p\nfunc entry(){target()}"}, {ID: "Fb", Path: "b.go", Root: "workspace", Source: "two", Text: "package p\nfunc target(){}"}}
	r, e := checkGoTypes(files)
	if e != nil || len(r.Bindings) != 0 {
		t.Fatal("source isolation", r, e)
	}
}

func TestKnowledgeGoTypeHelperBounds(t *testing.T) {
	raw, _ := json.Marshal([]goTypeFile{{ID: "F", Path: "a.go", Text: "package p\nfunc target(){}\nfunc entry(){target()}"}})
	var out bytes.Buffer
	if err := RunGoTypeHelper(bytes.NewReader(raw), &out); err != nil {
		t.Fatal(err)
	}
	var r goTypeResult
	if err := json.Unmarshal(out.Bytes(), &r); err != nil || len(r.Bindings) != 1 {
		t.Fatal(err, r)
	}
	for _, raw := range []string{"nullbad", strings.Repeat(" ", 8*1024*1024+1)} {
		if RunGoTypeHelper(strings.NewReader(raw), &out) == nil {
			t.Fatal("invalid accepted")
		}
	}
	if _, e := checkGoTypes([]goTypeFile{{ID: "F", Text: strings.Repeat("x", 256*1024+1)}}); e == nil {
		t.Fatal("file bound")
	}
	var limited goTypeOutput
	if _, e := limited.Write(make([]byte, 2*1024*1024+1)); e == nil {
		t.Fatal("output bound")
	}
}

func TestKnowledgeGoTypeProcessCancellationAndEnvironment(t *testing.T) {
	slow := filepath.Join(t.TempDir(), "slow.sh")
	if e := os.WriteFile(slow, []byte("#!/bin/sh\nsleep 30\n"), 0700); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, e := runGoTypeProcess(ctx, slow, nil); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal("cancel", e)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("cancellation retained descendants")
	}
	t.Setenv("AIDE_PARENT_TEST_SECRET", "placeholder-not-a-credential")
	probe := filepath.Join(t.TempDir(), "environment.sh")
	script := "#!/bin/sh\nif [ -n \"$AIDE_PARENT_TEST_SECRET\" ]; then exit 9; fi\nprintf '%s' '{\"engine\":\"go/types-indexed-snapshot-v1\",\"bindings\":[],\"diagnostics\":[]}'\n"
	if e := os.WriteFile(probe, []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
	if _, e := runGoTypeProcess(context.Background(), probe, nil); e != nil {
		t.Fatal("environment leaked", e)
	}
}
