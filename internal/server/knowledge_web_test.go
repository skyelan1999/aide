package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestKnowledgeWebParserTransport(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	text := "throw new Error('source must not execute');\nasync function save(){return fetch('/api/save', {method:'POST'});}\ndocument.getElementById('save').addEventListener('click',save);\nfunction dynamic(url){fetch(url);}\n"
	units, err := codeParseBatch(ctx, "JavaScript", []knowledgeCodeFile{{ID: "F", Text: text}})
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 1 {
		t.Fatal(units)
	}
	u := units[0]
	if len(u.Diagnostics) != 0 || len(u.Events) != 1 || len(u.Requests) != 1 || len(u.UnresolvedWeb) != 1 {
		t.Fatalf("source records lost in parser transport: %+v", u)
	}
	e, r := u.Events[0], u.Requests[0]
	if e.Target != "save" || e.Callback != "save" || e.Line != 3 || e.Column != 1 || e.Event != "click" {
		t.Fatalf("event identity/position lost: %+v", e)
	}
	if r.Caller != e.Callback || r.URL != "/api/save" || r.Method != "POST" || r.Line != 2 || r.Column < 1 {
		t.Fatalf("request identity/position lost: %+v", r)
	}
	if u.UnresolvedWeb[0].Reason != "dynamic_url" || u.UnresolvedWeb[0].Line != 4 {
		t.Fatal(u.UnresolvedWeb)
	}
}

func TestKnowledgeWebHTMLParserTransport(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	units, err := codeParseBatch(ctx, "HTML", []knowledgeCodeFile{{ID: "H", Text: "<button id='save'>保存</button>\n<script src='./app.js'></script>"}})
	if err != nil || len(units) != 1 {
		t.Fatalf("%+v %v", units, err)
	}
	u := units[0]
	if len(u.Diagnostics) != 0 || len(u.Symbols) != 1 || len(u.Scripts) != 1 || u.Scripts[0].Path != "./app.js" || u.Symbols[0].Key != "button#save" {
		t.Fatalf("HTML transport lost identity: %+v", u)
	}
}

func TestKnowledgeWebGoRouteDeclarations(t *testing.T) {
	source := `package main
import http "net/http"
type App struct{}
func (a *App) save(w http.ResponseWriter,r *http.Request){}
func handler(w http.ResponseWriter,r *http.Request){}
func (a *App) routes(){
 mux:=http.NewServeMux()
 mux.HandleFunc("POST /api/save", a.save)
 http.HandleFunc("GET /api/read", handler)
 mux.HandleFunc("/api/inline",func(w http.ResponseWriter,r *http.Request){})
}

func bind(mux *http.ServeMux){mux.HandleFunc("GET /api/bound",handler)}
`
	u := codeGo(knowledgeCodeFile{ID: "G", Node: knowledgeNode{Path: "server.go"}, Text: source})
	if len(u.Routes) != 4 {
		t.Fatalf("lost source route declaration: %+v", u)
	}
	for i, want := range []string{"App.save", "handler", "App.routes.anonymous@10:31", "handler"} {
		r := u.Routes[i]
		if i == 2 {
			if !strings.HasPrefix(r.Handler, "App.routes.anonymous@10:") {
				t.Fatal(r)
			}
		} else if r.Handler != want {
			t.Fatalf("route %d: %+v", i, r)
		}
		if r.Line < 8 || r.Column < 1 {
			t.Fatal(r)
		}
	}
	if u.Routes[0].Method != "POST" || u.Routes[0].Path != "/api/save" || u.Routes[1].Pattern != "GET /api/read" {
		t.Fatal(u.Routes)
	}
	for _, text := range []string{
		`func bad(){mux:=http.NewServeMux();mux=custom;mux.HandleFunc("/fake",handler)}`,
		`func bad(http *Custom){http.HandleFunc("/fake",handler)}`,
		`func bad(){custom.HandleFunc("/fake",handler)}`,
		`func bad(){mux:=http.NewServeMux();mux.HandleFunc(path,handler)}`,
		`func bad(callback func(http.ResponseWriter,*http.Request)){http.HandleFunc("/fake",callback)}`,
	} {
		x := codeGo(knowledgeCodeFile{ID: "G", Node: knowledgeNode{Path: "server.go"}, Text: "package main\nimport http \"net/http\"\nfunc handler(w http.ResponseWriter,r *http.Request){}\n" + text})
		resolved := 0
		for _, route := range x.Routes {
			if route.Handler != "" {
				resolved++
			}
		}
		if resolved != 0 || len(x.UnresolvedWeb) == 0 {
			t.Fatalf("invented or silently lost route: %+v", x)
		}
	}
}

const knowledgeWebServerFixture = `package main
import "net/http"
func service(){}
func handler(w http.ResponseWriter,r *http.Request){service()}
func routes(){mux:=http.NewServeMux();mux.HandleFunc("POST /api/save",handler)}
`

func TestKnowledgeWebIndexedPathAPI(t *testing.T) {
	a := testApp(t)
	files := map[string]string{
		"index.html": "<button id='save'>保存</button>\n<script src='./app.js'></script>",
		"app.js":     "async function save(){fetch('/api/save',{method:'POST'});}\ndocument.getElementById('save').onclick=save;",
		"server.go":  knowledgeWebServerFixture,
	}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(a.workPath, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	g := a.knowledgeSnapshotModeCached(context.Background(), true, nil)
	from, to := "", ""
	for _, n := range g.Nodes {
		if n.Name == "button#save" {
			from = n.ID
		}
		if n.Name == "service" {
			to = n.ID
		}
	}
	if from == "" || to == "" {
		t.Fatalf("actual index omitted endpoints: code=%+v warnings=%+v", g.Code, g.Warnings)
	}
	workspace, epoch := a.knowledgeUpdatesWorkspace()
	s := &a.knowledgeUpdates
	s.once.Do(func() { s.gate = make(chan struct{}, 1) })
	s.workspace, s.epoch = workspace, epoch
	s.views[1] = &knowledgeUpdateView{revision: "web-fixture", graph: g}
	q := knowledgePathRequest{Workspace: workspace, Revision: "web-fixture", Code: true, From: from, To: to}
	w := request(a, "POST", "/api/knowledge-map/paths", q)
	requireStatus(t, w, 200)
	var result knowledgePathResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Found || len(result.Steps) != 6 || result.Workspace != workspace || result.Revision != q.Revision {
		t.Fatalf("actual index/HTTP chain missing: %+v", result)
	}
	for i, step := range result.Steps {
		if step.Edge.Line < 1 || step.Edge.Column < 1 || result.Nodes[i].ContentHash == "" {
			t.Fatalf("lost source identity step %d: %+v", i, step)
		}
	}
	q.Revision = "stale"
	requireStatus(t, request(a, "POST", "/api/knowledge-map/paths", q), 409)
	q.Revision = "web-fixture"
	q.Code = false
	requireStatus(t, request(a, "POST", "/api/knowledge-map/paths", q), 409)
	q.Code = true
	q.Workspace = "other"
	requireStatus(t, request(a, "POST", "/api/knowledge-map/paths", q), 409)
}

func knowledgeWebGraphFixture(t *testing.T, html, js, server, serverRoot string) knowledgeGraph {
	t.Helper()
	files := []knowledgeCodeFile{
		{ID: "H", Text: html, Node: knowledgeNode{ID: "H", Kind: "file", Root: "workspace", Region: "workspace", Path: "index.html"}},
		{ID: "J", Text: js, Node: knowledgeNode{ID: "J", Kind: "file", Root: "workspace", Region: "workspace", Path: "app.js"}},
		{ID: "G", Text: server, Node: knowledgeNode{ID: "G", Kind: "file", Root: serverRoot, Region: serverRoot, Path: "server.go"}},
	}
	g := knowledgeGraph{Workspace: "fixture", Nodes: []knowledgeNode{files[0].Node, files[1].Node, files[2].Node}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	expandKnowledgeCode(ctx, &g, files)
	return g
}

func TestKnowledgeWebButtonServicePath(t *testing.T) {
	g := knowledgeWebGraphFixture(t, "<main>\n  <button id='save'>保存</button>\n<script src='./app.js'></script></main>", "async function save(){return fetch('/api/save',{method:'POST'});}\ndocument.getElementById('save').addEventListener('click',save);", knowledgeWebServerFixture, "workspace")
	from, to := "", ""
	for _, n := range g.Nodes {
		if n.Name == "button#save" {
			from = n.ID
		}
		if n.Name == "service" {
			to = n.ID
		}
	}
	result, err := findKnowledgePath(context.Background(), g, knowledgePathRequest{From: from, To: to})
	if err != nil || !result.Found || len(result.Steps) != 6 {
		t.Fatalf("missing real multi-stage chain: %+v %v graph=%+v diagnostics=%+v", result, err, g.Edges, g.Code.Diagnostics)
	}
	want := []string{"dom_event", "event_callback", "http_request", "http_route", "route_handler", "call_candidate"}
	if result.Steps[0].Edge.Line != 2 || result.Steps[0].Edge.Column != 3 {
		t.Fatalf("HTML source position lost: %+v", result.Steps[0])
	}
	for i, step := range result.Steps {
		if step.Edge.Kind != want[i] || step.Edge.Line < 1 || step.Edge.Column < 1 || step.Edge.Evidence == "" || step.Edge.Confidence == "" {
			t.Fatalf("step %d lost provenance: %+v", i, step)
		}
		if result.Nodes[i].ContentHash == "" {
			t.Fatalf("step %d lost content identity", i)
		}
	}
}

func TestKnowledgeWebRouteAndPageIsolation(t *testing.T) {
	page := "<button id='save'>x</button><script src='./app.js'></script>"
	js := "function save(){fetch('/api/save',{method:'POST'});} document.getElementById('save').onclick=save;"
	for _, tc := range []struct{ name, html, js, server, root, absent string }{
		{"unloaded script", "<button id='save'>x</button>", js, knowledgeWebServerFixture, "workspace", "dom_event"},
		{"duplicate DOM ID", page + "<div id='save'></div>", js, knowledgeWebServerFixture, "workspace", "dom_event"},
		{"external base", "<base href='https://external.test/'>" + page, js, knowledgeWebServerFixture, "workspace", "dom_event"},
		{"wrong method", page, strings.ReplaceAll(js, "'POST'", "'DELETE'"), knowledgeWebServerFixture, "workspace", "http_route"},
		{"external origin", page, strings.ReplaceAll(js, "'/api/save'", "'https://external.test/api/save'"), knowledgeWebServerFixture, "workspace", "http_route"},
		{"cross root", page, js, knowledgeWebServerFixture, "context", "http_route"},
		{"duplicate route", page, js, strings.ReplaceAll(knowledgeWebServerFixture, "mux.HandleFunc(\"POST /api/save\",handler)", "mux.HandleFunc(\"POST /api/save\",handler);mux.HandleFunc(\"POST /api/save\",handler)"), "workspace", "http_route"},
		{"unknown duplicate handler", page, js, strings.ReplaceAll(knowledgeWebServerFixture, "mux.HandleFunc(\"POST /api/save\",handler)", "mux.HandleFunc(\"POST /api/save\",handler);mux.HandleFunc(\"POST /api/save\",unknown)"), "workspace", "http_route"},
		{"dynamic registration", page, js, strings.ReplaceAll(knowledgeWebServerFixture, "mux.HandleFunc(\"POST /api/save\",handler)", "mux.HandleFunc(\"POST /api/save\",handler);mux.HandleFunc(dynamicPattern,handler)"), "workspace", "http_route"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := knowledgeWebGraphFixture(t, tc.html, tc.js, tc.server, tc.root)
			for _, e := range g.Edges {
				if e.Kind == tc.absent {
					t.Fatalf("invented %s: %+v", tc.absent, e)
				}
			}
			if g.Code.WebUnresolved == 0 {
				t.Fatal("lost unresolved evidence")
			}
		})
	}
}
