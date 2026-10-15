package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Opt-in browser host: only synthetic temporary App history is exposed, and
// only the three history routes are proxied. No live configuration or models.
func TestKnowledgeConclusionTopicsLiveBrowser(t *testing.T) {
	if os.Getenv("AIDE_TOPIC_BROWSER_FIXTURE") != "1" {
		t.Skip("opt-in local browser fixture")
	}
	fixture := knowledgeTopicFixture
	if os.Getenv("AIDE_TOPIC_BODY_FIXTURE") == "1" {
		fixture = knowledgeBodyFixture
	}
	if os.Getenv("AIDE_TOPIC_REFERENCE_FIXTURE") == "1" {
		fixture = knowledgeReferenceFixture
	}
	a, history, workspace := fixture(t)
	for _, name := range []string{"topic.md", "basis.txt"} {
		if err := os.Remove(filepath.Join(a.workspace.Name(), name)); err != nil {
			t.Fatal(err)
		}
	}
	markup, err := assets.ReadFile("web/starmap.html")
	if err != nil {
		t.Fatal(err)
	}
	source := string(markup)
	start := strings.Index(source, "<details id=\"knowledge-timeline\">")
	if start < 0 {
		t.Fatal("timeline markup not found")
	}
	end := strings.Index(source[start:], "<details class=\"logbook-diagnostics\">") + start
	if end < start {
		t.Fatal("timeline markup not found")
	}
	fragment := strings.Replace(source[start:end], "<details id=\"knowledge-timeline\">", "<details id=\"knowledge-timeline\" open>", 1)
	page := `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><script src="/settings-init.js"></script><link rel="stylesheet" href="/themes/light/tokens.css"><link rel="stylesheet" href="/themes/dark/tokens.css"><link rel="stylesheet" href="/starmap.css"><title>人工主题 · 临时App验收</title><style>body{overflow:auto}#limits{position:static;max-width:900px;width:calc(100% - 32px);margin:16px auto;box-sizing:border-box}</style></head><body><details id="limits" open><summary>临时App真实API · 人工主题验收</summary><p>仅合成资料，不连接生产或真实模型。</p>` + fragment + `<button id="fixture-expire">模拟旧观察淘汰</button><button id="fixture-finish">结束临时验收</button></details><script src="/starmap-topics.js"></script><script src="/starmap-timeline.js"></script><script src="/fixture/client.js"></script></body></html>`
	client := fmt.Sprintf(`const request=async(path,options={})=>{const r=await fetch('/fixture/api'+path,{...options,headers:{'Content-Type':'application/json'}});const d=await r.json();if(!r.ok)throw Error(d.error||'读取失败');return d;};const timeline=window.AideKnowledgeTimeline.create({request});timeline.sync({workspace:%q,code:false,revision:'1'});document.getElementById('fixture-finish').onclick=()=>{document.getElementById('fixture-finish').textContent='临时验收已结束';fetch('/fixture/finish',{method:'POST'});};`, workspace)
	client += fmt.Sprintf(`document.getElementById('fixture-expire').onclick=async()=>{const r=await fetch('/fixture/expire',{method:'POST'});if(r.ok){document.getElementById('fixture-expire').disabled=true;timeline.sync({workspace:%q,code:false,revision:'2'});}};`, workspace)
	done := make(chan struct{})
	var once sync.Once
	mux := http.NewServeMux()
	handler := a.Handler()
	mux.HandleFunc("/fixture/api/", func(w http.ResponseWriter, r *http.Request) {
		tail := strings.TrimPrefix(r.URL.Path, "/fixture/api")
		allowed := r.Method == "GET" && (tail == "/timeline" || tail == "/timeline/topics") || r.Method == "POST" && tail == "/timeline/compare" || r.Method == "PUT" && tail == "/timeline/topics"
		if !allowed {
			http.NotFound(w, r)
			return
		}
		cp := r.Clone(r.Context())
		cp.URL.Path = "/api/knowledge-map" + tail
		cp.Header.Set("Authorization", "Bearer "+a.token)
		handler.ServeHTTP(w, cp)
	})
	mux.HandleFunc("POST /fixture/finish", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		once.Do(func() { close(done) })
	})
	mux.HandleFunc("POST /fixture/expire", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		file := knowledgeTimeFile(a.dataPath, workspace, false)
		h, e := loadKnowledgeTime(file, workspace, false, knowledgeTimeKey(a.token))
		if e != nil || h.Dropped > 0 {
			http.Error(w, "fixture already expired", 409)
			return
		}
		last := h.Snapshots[len(h.Snapshots)-1]
		g := last.Graph
		g.Nodes = append([]knowledgeNode(nil), g.Nodes...)
		g.Nodes[0].Modified = "fixture-third-observation"
		h, _, e = appendKnowledgeTimeAI(h, g, last.AI, time.Now())
		if e == nil {
			h.Snapshots = h.Snapshots[1:]
			h.Dropped++
			e = saveKnowledgeTime(file, h, knowledgeTimeKey(a.token))
		}
		if e != nil {
			http.Error(w, "fixture retention failed", 500)
			return
		}
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/fixture/client.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		_, _ = w.Write([]byte(client))
	})
	static := map[string]string{"/settings-init.js": "web/settings-init.js", "/themes/light/tokens.css": "web/themes/light/tokens.css", "/themes/dark/tokens.css": "web/themes/dark/tokens.css", "/starmap.css": "web/starmap.css", "/starmap-topics.js": "web/starmap-topics.js", "/starmap-timeline.js": "web/starmap-timeline.js"}
	for route, name := range static {
		mux.HandleFunc(route, func(w http.ResponseWriter, r *http.Request) {
			b, e := assets.ReadFile(name)
			if e != nil {
				http.NotFound(w, r)
				return
			}
			if strings.HasSuffix(name, ".js") {
				w.Header().Set("Content-Type", "text/javascript")
			} else {
				w.Header().Set("Content-Type", "text/css")
			}
			_, _ = w.Write(b)
		})
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/fixture/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(page))
	})
	listener, err := net.Listen("tcp", ":18787")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	defer server.Close()
	go server.Serve(listener)
	t.Log("TOPIC_FIXTURE_READY http://127.0.0.1:18787/fixture/")
	select {
	case <-done:
	case <-time.After(8 * time.Minute):
		t.Fatal("browser fixture lease expired")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
	store, err := loadKnowledgeTopics(knowledgeTopicFile(a.dataPath, workspace, false), workspace, false, knowledgeTopicKey(a.token))
	if os.Getenv("AIDE_TOPIC_REFERENCE_FIXTURE") == "1" {
		if err != nil || len(store.Topics) != 0 {
			t.Fatal("reference browser unexpectedly wrote topics", err)
		}
		current, e := loadKnowledgeTime(knowledgeTimeFile(a.dataPath, workspace, false), workspace, false, knowledgeTimeKey(a.token))
		if e != nil || len(current.Snapshots) != 2 {
			t.Fatal("reference browser history", e)
		}
		out := compareKnowledgeTime(current.Snapshots[0], current.Snapshots[1]).Conclusions
		if len(out.Changes) != 0 || len(out.Evidence) != 2 {
			t.Fatal("reference browser evidence count")
		}
		for _, row := range out.Evidence {
			if !row.ReferencesChanged || !row.ContentChanged || row.Unknown {
				t.Fatal("reference browser difference incomplete")
			}
		}
		proof, _ := json.Marshal(map[string]any{"conclusions": out, "temporaryApp": true, "sourceFilesRemovedBeforeUI": true, "liveProvider": false})
		t.Log("REFERENCE_BROWSER_PROOF=" + string(proof))
		return
	}
	if os.Getenv("AIDE_TOPIC_BODY_FIXTURE") == "1" {
		if err != nil || len(store.Topics) != 1 || store.Revision != 2 || len(store.Topics[0].Members) != 2 {
			t.Fatal("body browser did not save whole chunk then exact tail", err, store.Revision)
		}
		members := store.Topics[0].Members
		if members[0].Candidate.Quote.Text != "历史尾段甲😀" || members[1].Candidate.Quote.Text != "历史尾段乙😀" {
			t.Fatal("body browser selections differ from exact tails")
		}
		current, err := loadKnowledgeTime(knowledgeTimeFile(a.dataPath, workspace, false), workspace, false, knowledgeTimeKey(a.token))
		if err != nil || current.Version != 3 || current.Dropped != 0 || len(current.Snapshots) != 2 {
			t.Fatal("body browser history mismatch", err)
		}
		out := compareKnowledgeTime(current.Snapshots[0], current.Snapshots[1]).Conclusions
		appendKnowledgeManualTopics(&out, current.Snapshots[0], current.Snapshots[1], store)
		if len(out.Manual) != 1 || out.Manual[0].State != "selected_excerpts_compared" || !out.Manual[0].TextChanged || len(out.Changes) != 0 {
			t.Fatal("body browser comparison mismatch")
		}
		proof, _ := json.Marshal(map[string]any{"revision": store.Revision, "topics": store.Topics, "manual": out.Manual, "liveProvider": false, "temporaryApp": true, "sourceFilesRemovedBeforeUI": true, "historyFormat": current.Version})
		t.Log("BODY_BROWSER_PROOF=" + string(proof))
		return
	}
	if err != nil || len(store.Topics) < 2 || store.Revision < 4 {
		t.Fatal("browser did not complete two topics plus disable/reactivate", err, store.Revision)
	}
	current, err := loadKnowledgeTime(knowledgeTimeFile(a.dataPath, workspace, false), workspace, false, knowledgeTimeKey(a.token))
	if err != nil || current.Dropped != 1 {
		t.Fatal("browser did not exercise retention fixture", err)
	}
	out := compareKnowledgeTime(current.Snapshots[0], current.Snapshots[1]).Conclusions
	appendKnowledgeManualTopics(&out, current.Snapshots[0], current.Snapshots[1], store)
	if len(out.Manual) != 2 {
		t.Fatal("expected both active browser topics", out.Manual)
	}
	for _, r := range out.Manual {
		if r.State != "not_aligned_for_pair" || r.TextChanged {
			t.Fatal("expired observation compared as fact", r)
		}
	}
	proof, _ := json.Marshal(map[string]any{"revision": store.Revision, "topics": store.Topics, "expiredViews": knowledgeTopicsView(store, current, current.Snapshots[0].ID, current.Snapshots[1].ID), "manual": out.Manual, "liveProvider": false, "temporaryApp": true, "sourceFilesRemovedBeforeUI": true, "initialSnapshots": []string{history.Snapshots[0].ID, history.Snapshots[1].ID}})
	t.Log("TOPIC_BROWSER_PROOF=" + string(proof))
}
