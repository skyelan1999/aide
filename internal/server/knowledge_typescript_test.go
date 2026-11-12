package server

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestIssue801TypeScriptTSXSymbolsAndRelativeCalls(t *testing.T) {
	files := []knowledgeCodeFile{
		{ID: "worker", Node: knowledgeNode{ID: "worker", Path: "worker.ts", Root: "workspace"}, Text: "export function StarLinkTypeScriptWorker(value: number): number { return value + 1; }\n"},
		{ID: "caller", Node: knowledgeNode{ID: "caller", Path: "caller.ts", Root: "workspace"}, Text: "import { StarLinkTypeScriptWorker } from './worker';\nexport function StarLinkTypeScriptCaller(value: number): number { return StarLinkTypeScriptWorker(value); }\n"},
		{ID: "component", Node: knowledgeNode{ID: "component", Path: "component.tsx", Root: "workspace"}, Text: "type Props = {name: string};\nexport function TypedComponent(props: Props) {return <span>{props.name}</span>;}\n"},
	}
	g := knowledgeGraph{}
	for _, file := range files {
		g.Nodes = append(g.Nodes, file.Node)
	}
	expandKnowledgeCode(context.Background(), &g, files)
	found := map[string]string{}
	for _, n := range g.Nodes {
		if n.Kind == "symbol" {
			found[n.Name] = n.ID
			if n.Language != "TypeScript" {
				t.Fatal("language missing", n)
			}
			if n.Line < 1 || n.ParentFile == "" {
				t.Fatal("source position missing", n)
			}
		}
	}
	for _, name := range []string{"StarLinkTypeScriptWorker", "StarLinkTypeScriptCaller", "TypedComponent"} {
		if found[name] == "" {
			t.Fatalf("missing %s: %+v", name, g.Code)
		}
	}
	var matched bool
	for _, edge := range g.Edges {
		if edge.From == found["StarLinkTypeScriptCaller"] && edge.To == found["StarLinkTypeScriptWorker"] {
			if edge.Kind != "call_candidate" || edge.Confidence != "name_candidate" {
				t.Fatal("static edge promoted", edge)
			}
			matched = true
		}
	}
	if !matched {
		t.Fatalf("relative imported call missing: %+v", g.Edges)
	}
	if !strings.Contains(strings.Join(g.Code.Diagnostics, "\n"), "JSX/TSX") {
		t.Fatal("JSX dispatch limit not disclosed")
	}
}

func TestTypeScriptSourceOnlySyntaxFailureAndAmbiguity(t *testing.T) {
	files := []knowledgeCodeFile{
		{ID: "bad", Node: knowledgeNode{ID: "bad", Path: "bad.ts"}, Text: "export function broken(value: ) {}"},
		{ID: "namespace", Node: knowledgeNode{ID: "namespace", Path: "namespace.ts"}, Text: "namespace Hidden { export function Run(){} }"},
		{ID: "types", Node: knowledgeNode{ID: "types", Path: "types.ts"}, Text: "import type {Danger} from './worker';\ndeclare function declared():void;\nexport function runtime(){ return 1; }"},
	}
	units, err := codeParseBatch(context.Background(), "JavaScript", files)
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 3 || len(units[0].Diagnostics) == 0 || len(units[0].Symbols) != 0 || len(units[1].Diagnostics) == 0 || len(units[1].Symbols) != 0 {
		t.Fatalf("unsupported syntax did not fail closed: %+v", units)
	}
	if len(units[2].Imports) != 0 || len(units[2].Calls) != 0 || len(units[2].Symbols) != 1 || units[2].Symbols[0].Name != "runtime" {
		t.Fatalf("type-only declarations became runtime evidence: %+v", units[2])
	}
}

func TestTypeScriptNeverExecutesProjectSource(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "must-not-exist")
	text := "require('node:fs').writeFileSync(" + strconv.Quote(marker) + ",'executed');export function Safe(value:number):number{return value;}"
	units, err := codeParseBatch(context.Background(), "JavaScript", []knowledgeCodeFile{{ID: "untrusted", Text: text, Node: knowledgeNode{Path: "untrusted.ts"}}})
	if err != nil || len(units) != 1 || len(units[0].Symbols) != 1 {
		t.Fatalf("source parser unavailable: %+v %v", units, err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("project source was executed", err)
	}
}
