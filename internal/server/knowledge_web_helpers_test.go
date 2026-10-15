package server

import (
	"context"
	"strings"
	"testing"
)

func TestKnowledgeWebHelperPathIsolation(t *testing.T) {
	js := `const $=id=>document.getElementById(id);
async function api(path,options={}){const headers={};const response=await fetch('/api'+path,{...options,headers});return response.json();}
function save(){api('/save',{method:'POST'});}
function other(){api('/other');}
$('save').onclick=save;
$('other').onclick=other;`
	server := strings.ReplaceAll(knowledgeWebServerFixture, `mux.HandleFunc("POST /api/save",handler)`, `mux.HandleFunc("POST /api/save",handler);mux.HandleFunc("GET /api/other",otherHandler)`) + "\nfunc otherHandler(w http.ResponseWriter,r *http.Request){otherService()}\nfunc otherService(){}\n"
	g := knowledgeWebGraphFixture(t, `<button id="save">保存</button><button id="other">其他</button><script src="/app.js?v=1"></script>`, js, server, "workspace")
	ids := map[string]string{}
	for _, n := range g.Nodes {
		ids[n.Name] = n.ID
	}
	result, err := findKnowledgePath(context.Background(), g, knowledgePathRequest{From: ids["button#save"], To: ids["service"]})
	if err != nil || !result.Found || len(result.Steps) != 6 {
		t.Fatalf("helper/root script path missing: %+v %v diagnostics=%+v", result, err, g.Code.Diagnostics)
	}
	if e := result.Steps[2].Edge; e.Confidence != "source_template" || !strings.Contains(e.Evidence, "wrapper api") {
		t.Fatalf("wrapper evidence promoted/lost: %+v", e)
	}
	result, err = findKnowledgePath(context.Background(), g, knowledgePathRequest{From: ids["button#save"], To: ids["otherService"]})
	if err != nil || result.Found {
		t.Fatalf("shared API wrapper mixed call-site contexts: %+v %v", result, err)
	}
}

func TestKnowledgeWebRootScriptAmbiguity(t *testing.T) {
	page := knowledgeCodeFile{ID: "H", Node: knowledgeNode{Root: "workspace", Region: "workspace", Path: "web/index.html"}}
	files := map[string]knowledgeCodeFile{
		"J1": {ID: "J1", Node: knowledgeNode{Root: "workspace", Region: "workspace", Path: "web/app.js"}},
		"J2": {ID: "J2", Node: knowledgeNode{Root: "workspace", Region: "workspace", Path: "app.js"}},
	}
	if got := knowledgeWebScriptFile(page, "/app.js?v=1", files); got != "" {
		t.Fatal("ambiguous source root silently selected", got)
	}
	delete(files, "J2")
	if got := knowledgeWebScriptFile(page, "/app.js?v=1", files); got != "J1" {
		t.Fatal(got)
	}
	for _, src := range []string{"//external.test/app.js", "https://external.test/app.js", "file:///app.js"} {
		if got := knowledgeWebScriptFile(page, src, files); got != "" {
			t.Fatal(src, got)
		}
	}
}
