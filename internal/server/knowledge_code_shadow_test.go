package server

import (
	"context"
	"fmt"
	"reflect"
	"testing"
)

func TestKnowledgeCodeLexicalBindings(t *testing.T) {
	for _, test := range []struct {
		name, path, text string
		calls            map[string]string
		unresolved       int
	}{
		{"js-parameter", "main.js", "function target(){}\nfunction entry(target){target()}\nfunction positive(){target()}\n", map[string]string{"positive": "target"}, 1},
		{"python-parameter", "main.py", "def target(): pass\ndef entry(target):\n target()\ndef positive():\n target()\n", map[string]string{"positive": "target"}, 1},
		{"js-block", "main.js", "function target(){}\nfunction entry(){ {let target;target()} target() }\n", map[string]string{"entry": "target"}, 1},
		{"js-var-hoisting", "main.js", "function target(){}\nfunction entry(){target();if(true){var target}}\n", nil, 1},
		{"js-captured-parameter", "main.js", "function target(){}\nfunction entry(target){function inner(){target()}}\n", nil, 1},
		{"python-local-before-assignment", "main.py", "def target(): pass\ndef entry():\n target()\n target=unknown\n", nil, 1},
		{"python-captured-parameter", "main.py", "def target(): pass\ndef entry(target):\n def inner():\n  target()\n", nil, 1},
		{"python-class-not-closure", "main.py", "def target(): pass\nclass C:\n def target(self): pass\n def entry(self):\n  target()\n", map[string]string{"C.entry": "target"}, 0},
		{"python-global", "main.py", "def target(): pass\ndef outer():\n def target(): pass\n def entry():\n  global target\n  target()\n", map[string]string{"outer.entry": "target"}, 0},
		{"python-nonlocal", "main.py", "def target(): pass\ndef outer():\n def target(): pass\n def entry():\n  nonlocal target\n  target()\n", map[string]string{"outer.entry": "outer.target"}, 0},
		{"python-comprehension", "main.py", "def target(): pass\ndef entry(values):\n [target() for target in values]\n target()\n", map[string]string{"entry": "target"}, 1},
		{"js-assigned-function", "main.js", "const target=()=>{};\nfunction entry(){target()}\n", map[string]string{"entry": "target"}, 0},
		{"python-assigned-lambda", "main.py", "target=lambda: None\ndef entry():\n target()\n", map[string]string{"entry": "anonymous@1:1"}, 0},
		{"js-var-redeclaration", "main.js", "var target;function target(){}\nfunction entry(){target()}\n", map[string]string{"entry": "target"}, 0},
		{"js-destructured-parameter", "main.js", "function target(){}\nfunction entry({target}){target()}\n", nil, 1},
		{"js-default-parameter-shadow", "main.js", "function target(){}\nfunction entry(target=target()){}\n", nil, 1},
		{"js-this-method", "main.js", "class C{target(){} entry(){this.target()}}\n", map[string]string{"C.entry": "C.target"}, 0},
		{"python-global-assignment", "main.py", "def target(): pass\ndef entry():\n global target\n target=unknown\n target()\n", nil, 1},
		{"python-nonlocal-forward-assignment", "main.py", "def target(): pass\ndef outer():\n def entry():\n  nonlocal target\n  target=unknown\n  target()\n def target(): pass\n", nil, 1},
		{"python-comprehension-walrus", "main.py", "def target(): pass\ndef entry(values):\n [(target:=value) for value in values]\n target()\n", nil, 1},
		{"python-pattern-capture", "main.py", "def target(): pass\ndef entry(value):\n match value:\n  case {'target': target}:\n   target()\n", nil, 1},
		{"python-default-in-enclosing-scope", "main.py", "def target(): pass\ndef entry(target=target()): pass\n", map[string]string{"": "target"}, 0},
		{"python-self-method", "main.py", "class C:\n def target(self): pass\n def entry(self):\n  self.target()\n", map[string]string{"C.entry": "C.target"}, 0},
		{"python-self-reassigned", "main.py", "class C:\n def target(self): pass\n def entry(self):\n  self=unknown\n  self.target()\n", nil, 1},
		{"js-body-function-overrides-parameter", "main.js", "function target(){}\nfunction entry(target){target();function target(){}}\n", map[string]string{"entry": "entry.target"}, 0},
		{"js-default-cannot-see-body-function", "main.js", "function target(){}\nfunction entry(target=target()){function target(){}}\n", nil, 1},
		{"js-assigned-class", "main.js", "const Target=class {};\nfunction entry(){new Target()}\n", map[string]string{"entry": "Target"}, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := knowledgeCodeFile{ID: "F", Node: knowledgeNode{ID: "F", Path: test.path, Root: "workspace"}, Text: test.text}
			graph := knowledgeGraph{Nodes: []knowledgeNode{file.Node}}
			expandKnowledgeCode(context.Background(), &graph, []knowledgeCodeFile{file})
			names := map[string]string{}
			for _, n := range graph.Nodes {
				names[n.ID] = n.Name
			}
			count := 0
			for _, edge := range graph.Edges {
				if edge.Kind != "call_candidate" {
					continue
				}
				if want, ok := test.calls[names[edge.From]]; !ok || names[edge.To] != want || edge.Confidence != "name_candidate" {
					t.Fatalf("incorrect lexical call %s -> %s: %+v", names[edge.From], names[edge.To], edge)
				}
				count++
			}
			if count != len(test.calls) || graph.Code.Unresolved != test.unresolved {
				t.Fatalf("calls=%d want=%d unresolved=%d want=%d diagnostics=%v", count, len(test.calls), graph.Code.Unresolved, test.unresolved, graph.Code.Diagnostics)
			}
		})
	}
}

func TestKnowledgeCodeSameLineAndScopedImports(t *testing.T) {
	for _, test := range []struct {
		name  string
		files []knowledgeCodeFile
		want  map[string]string
	}{
		{"block-declarations", []knowledgeCodeFile{{ID: "F", Node: knowledgeNode{ID: "F", Path: "main.js", Root: "workspace"}, Text: "function entry(){\n {function target(){} target()}\n {function target(){} target()}\n}\n"}}, map[string]string{"entry:2": "F:2", "entry:3": "F:3"}},
		{"scoped-import-aliases", []knowledgeCodeFile{
			{ID: "F", Node: knowledgeNode{ID: "F", Path: "main.py", Root: "workspace"}, Text: "def first():\n from one import target as call\n call()\ndef second():\n from two import target as call\n call()\n"},
			{ID: "one", Node: knowledgeNode{ID: "one", Path: "one.py", Root: "workspace"}, Text: "def target(): pass\n"},
			{ID: "two", Node: knowledgeNode{ID: "two", Path: "two.py", Root: "workspace"}, Text: "def target(): pass\n"},
		}, map[string]string{"first:3": "one:1", "second:6": "two:1"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			graph := knowledgeGraph{}
			for _, f := range test.files {
				graph.Nodes = append(graph.Nodes, f.Node)
			}
			expandKnowledgeCode(context.Background(), &graph, test.files)
			nodes := map[string]knowledgeNode{}
			for _, n := range graph.Nodes {
				nodes[n.ID] = n
			}
			got := map[string]string{}
			for _, e := range graph.Edges {
				if e.Kind != "call_candidate" {
					continue
				}
				if e.Column < 1 {
					t.Fatal("call column missing", e)
				}
				from, to := nodes[e.From], nodes[e.To]
				got[fmt.Sprintf("%s:%d", from.Name, e.Line)] = fmt.Sprintf("%s:%d", to.ParentFile, to.Line)
			}
			if !reflect.DeepEqual(got, test.want) || graph.Code.Unresolved != 0 {
				t.Fatalf("got=%v want=%v unresolved=%v diagnostics=%v", got, test.want, graph.Code.Unresolved, graph.Code.Diagnostics)
			}
		})
	}
}

func TestKnowledgeCodeScopedImports(t *testing.T) {
	for _, test := range []struct {
		path, text, otherPath, otherText string
		calls                            int
		unresolved                       int
	}{
		{"main.js", "import * as lib from './lib.js';\nfunction entry(lib){lib.target()}\nfunction positive(){lib.target()}\n", "lib.js", "export function target(){}\n", 1, 1},
		{"main.py", "import lib\ndef entry(lib):\n lib.target()\ndef positive():\n lib.target()\n", "lib.py", "def target(): pass\n", 1, 1},
		{"main.py", "from lib import target\ndef entry(target):\n target()\ndef positive():\n target()\n", "lib.py", "def target(): pass\n", 1, 1},
	} {
		t.Run(test.path+test.text[:6], func(t *testing.T) {
			files := []knowledgeCodeFile{{ID: "F", Node: knowledgeNode{ID: "F", Path: test.path, Root: "workspace"}, Text: test.text}, {ID: "L", Node: knowledgeNode{ID: "L", Path: test.otherPath, Root: "workspace"}, Text: test.otherText}}
			graph := knowledgeGraph{Nodes: []knowledgeNode{files[0].Node, files[1].Node}}
			expandKnowledgeCode(context.Background(), &graph, files)
			names := map[string]string{}
			parents := map[string]string{}
			for _, n := range graph.Nodes {
				names[n.ID], parents[n.ID] = n.Name, n.ParentFile
			}
			count := 0
			for _, e := range graph.Edges {
				if e.Kind == "call_candidate" {
					if names[e.From] != "positive" || parents[e.To] != "L" || e.Confidence != "name_candidate" {
						t.Fatalf("wrong scoped import edge: %+v", e)
					}
					count++
				}
			}
			if count != test.calls || graph.Code.Unresolved != test.unresolved {
				t.Fatalf("calls=%d unresolved=%d diagnostics=%v", count, graph.Code.Unresolved, graph.Code.Diagnostics)
			}
		})
	}
}
