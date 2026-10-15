package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Called only by the opt-in real protocol suite. Data and credentials belong
// to its disposable server; no user workspace or configured host is involved.
func assertKnowledgeFTPLiveProgressive(t *testing.T, host string) {
	for _, kind := range []string{"ftp", "ftps"} {
		t.Run(kind, func(t *testing.T) {
			a := testApp(t)
			seed := "index-seed"
			config := map[string]any{"host": host, "port": 22, "username": "fixture", "auth": "password", "path": "/srv/refs"}
			requireStatus(t, request(a, "PUT", "/api/sources", map[string]any{"sources": []any{sourceBody(seed, seed, "sftp", config, true)}, "secrets": map[string]any{seed: map[string]any{"password": "fixture-pass"}}}), 200)
			marker := "live-index-literal-" + kind
			for i := 0; i < 6; i++ {
				name := fmt.Sprintf("nested/live-file-%d.md", i)
				if i == 0 {
					name = "nested/live-priority.md"
				}
				body := map[string]string{"source": seed, "path": name, "content": "# Remote fixture\n" + marker + "\n"}
				previous := request(a, "GET", "/api/file?source="+seed+"&path="+url.QueryEscape(name), nil)
				if previous.Code == 200 {
					var file struct {
						Hash string `json:"hash"`
					}
					if err := json.Unmarshal(previous.Body.Bytes(), &file); err != nil || file.Hash == "" {
						t.Fatal("existing seed file has no revision hash", err)
					}
					body["hash"] = file.Hash
				}
				// A missing source file is currently reported as 400. The guarded
				// PUT below must still succeed; unreadable existing files cannot
				// be overwritten without their hash.
				requireStatus(t, request(a, "PUT", "/api/file", body), 200)
			}
			id := "live-index-" + kind
			requireStatus(t, request(a, "PUT", "/api/sources", map[string]any{"sources": []any{sourceBody(id, kind, kind, map[string]any{"url": "ftp://" + host, "username": "fixture"}, false)}, "secrets": map[string]any{id: map[string]any{"password": "fixture-pass"}}}), 200)
			policy := defaultKnowledgeIndexPolicy()
			policy.ScopePaths = map[string][]string{id: {"nested"}}
			policy.PriorityPaths = map[string][]string{id: {"nested/live-priority.md"}}
			version := indexPolicyResponse(t, a)
			requireStatus(t, request(a, "PUT", "/api/knowledge-map/index-policy", map[string]any{"policy": policy, "workspaceId": version["workspaceId"], "revision": version["revision"]}), 200)
			area := func(app *App) knowledgeArea {
				app.mu.Lock()
				defer app.mu.Unlock()
				for _, ar := range app.knowledgeAreasLocked(app.wsID(), app.workspace.Name(), app.reference.Name()) {
					if ar.source == id {
						return ar
					}
				}
				t.Fatal("source missing after load")
				return knowledgeArea{}
			}
			step := func(ctx context.Context, app *App) knowledgeRemoteResult {
				app.knowledgeUpdates.remoteMu.Lock()
				for _, memo := range app.knowledgeUpdates.remote {
					memo.checked = time.Time{}
				}
				app.knowledgeUpdates.remoteMu.Unlock()
				return app.knowledgeRemoteCached(ctx, area(app), false, 2, 1)
			}
			paths := func(r knowledgeRemoteResult) map[string]bool {
				out := map[string]bool{}
				for _, n := range r.nodes {
					if n.Kind != "file" {
						continue
					}
					if !strings.HasPrefix(n.Path, "nested/") || n.Source != id {
						t.Fatalf("source/scope leak: %+v", n)
					}
					out[n.Path] = true
				}
				return out
			}
			step(context.Background(), a) // Discover root; do not treat it as complete.
			r := step(context.Background(), a)
			before := paths(r)
			if r.coverage == nil || !r.coverage.Progressive || r.coverage.TotalKnown || len(before) == 0 || len(before) > 2 || !before["nested/live-priority.md"] {
				t.Fatalf("missing bounded priority publication: %+v paths=%v", r.coverage, before)
			}
			// New App loads sources, secrets, policy and encrypted cursor from disk.
			// This is an App reload, not a separate OS process or browser restart.
			reloaded, err := New(a.workPath, a.refPath, a.dataPath)
			if err != nil {
				t.Fatal(err)
			}
			defer reloaded.Close()
			r = step(context.Background(), reloaded)
			after := paths(r)
			if len(after) <= len(before) || len(after)-len(before) > 2 {
				t.Fatalf("checkpoint did not resume bounded progress: %d -> %d: %+v", len(before), len(after), r.coverage)
			}
			for p := range before {
				if !after[p] {
					t.Fatal("published node lost after reload", p)
				}
			}
			cancelled, cancel := context.WithCancel(context.Background())
			cancel()
			failed := step(cancelled, reloaded)
			if failed.state != "unavailable" || failed.coverage == nil || failed.coverage.TotalKnown || len(paths(failed)) != len(after) {
				t.Fatalf("cancellation lost published data or claimed completion: %+v", failed.coverage)
			}
			for batch := 0; batch < 12 && !r.coverage.TotalKnown; batch++ {
				r = step(context.Background(), reloaded)
				current := paths(r)
				if len(current) < len(after) || len(current)-len(after) > 2 {
					t.Fatalf("non-monotonic or unbounded batch: %d -> %d", len(after), len(current))
				}
				after = current
			}
			if !r.coverage.TotalKnown || r.coverage.Pending != 0 || len(after) != 7 {
				t.Fatalf("selected catalogue incomplete: %+v paths=%v", r.coverage, after)
			}
			graph := knowledgeGraph{Workspace: knowledgeID(reloaded.wsID(), "scope", "", "."), Nodes: r.nodes}
			found, err := reloaded.retrieveDocuments(context.Background(), graph, documentRequest{Query: marker, Mode: "original", Workspace: graph.Workspace, Source: id})
			if err != nil {
				t.Fatal(err)
			}
			if found.Engine != "literal-source-text" || len(found.Hits) != 6 || found.NextCursor != "" {
				t.Fatalf("literal remote retrieval incomplete: %+v", found)
			}
			seen := map[string]bool{}
			for _, hit := range found.Hits {
				if hit.File.Source != id || !after[hit.File.Path] || seen[hit.File.Path] || hit.Locator == "" || hit.Hash != hash([]byte("# Remote fixture\n"+marker+"\n")) || !strings.Contains(hit.Text, marker) {
					t.Fatalf("source evidence missing: %+v", hit)
				}
				seen[hit.File.Path] = true
			}
			if kind == "ftps" {
				addresses, err := net.LookupIP(host)
				if err != nil {
					t.Fatal(err)
				}
				var ip string
				for _, address := range addresses {
					if address.To4() != nil {
						ip = address.String()
						break
					}
				}
				if ip == "" {
					t.Fatal("fixture IPv4 address missing")
				}
				// Same real service and credentials, but its certificate only names
				// the fixture hostname. Faster scheduling must still reject this IP.
				requireStatus(t, request(reloaded, "PUT", "/api/sources", map[string]any{"sources": []any{sourceBody(id, kind, kind, map[string]any{"url": "ftp://" + ip, "username": "fixture"}, false)}}), 200)
				if body, err := reloaded.knowledgeAreaRead(context.Background(), area(reloaded), "nested/live-priority.md", 64*1024); err == nil || len(body) != 0 {
					t.Fatal("FTPS certificate hostname mismatch was accepted")
				}
			}
			t.Logf("PASS real %s: partial priority publication, disk App reload, cancellation retention, scoped catalogue7, literal source hits6", kind)
		})
	}
}
