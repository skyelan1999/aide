package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestKnowledgeListingPreservesLargeDirectory(t *testing.T) {
	for _, unix := range []bool{false, true} {
		var b strings.Builder
		for i := 0; i < 2105; i++ {
			if unix {
				fmt.Fprintf(&b, "-rw-r--r-- 1 u g 5 Oct 9 12:00 file-%04d.md\n", i)
			} else {
				fmt.Fprintf(&b, "file-%04d.md\n", i)
			}
		}
		entries := knowledgeFTPListing(b.String(), ".")
		if len(entries) != 2105 {
			t.Fatalf("unix=%v silently truncated to %d", unix, len(entries))
		}
		if unix && len(parseSFTPList(b.String(), ".")) != 2000 {
			t.Fatal("interactive listing limit changed")
		}
	}
	if entries := knowledgeFTPListing("../escape\n.\n..\nsub/\ngood.md\n", "."); len(entries) != 2 {
		t.Fatal("unsafe listing entries", entries)
	}
}

// Exercise the configured adapter, memo and encrypted checkpoint using a
// deterministic curl subprocess. This is not a live FTP/TLS server acceptance.
func TestKnowledgeFTPProgressiveAdapter(t *testing.T) {
	for _, kind := range []string{"ftp", "ftps"} {
		t.Run(kind, func(t *testing.T) {
			a := testApp(t)
			stub := filepath.Join(t.TempDir(), "curl")
			script := `#!/bin/sh
url=""
for arg in "$@"; do case "$arg" in ftp*) url="$arg";; esac; done
case "$url" in
 */selected/)
  echo important.md
  i=0; while [ "$i" -lt 5 ]; do echo "file-$i.md"; i=$((i+1)); done;;
 */) echo selected/; echo excluded.md;;
 *) echo adapter-content;;
esac
`
			if err := os.WriteFile(stub, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			a.curlBin = stub
			requireStatus(t, request(a, "PUT", "/api/sources", map[string]any{"sources": []any{sourceBody("remote", "remote", kind, map[string]any{"url": kind + "://example.test/pub"}, false)}}), 200)
			p := defaultKnowledgeIndexPolicy()
			p.ScopePaths = map[string][]string{"remote": {"selected"}}
			p.PriorityPaths = map[string][]string{"remote": {"selected/important.md"}}
			v := indexPolicyResponse(t, a)
			requireStatus(t, request(a, "PUT", "/api/knowledge-map/index-policy", map[string]any{"policy": p, "workspaceId": v["workspaceId"], "revision": v["revision"]}), 200)
			area := func(app *App) knowledgeArea {
				app.mu.Lock()
				defer app.mu.Unlock()
				for _, ar := range app.knowledgeAreasLocked(app.wsID(), app.workspace.Name(), app.reference.Name()) {
					if ar.source == "remote" {
						return ar
					}
				}
				t.Fatal("missing source")
				return knowledgeArea{}
			}
			ar := area(a)
			step := func(app *App, ar knowledgeArea) knowledgeRemoteResult {
				app.knowledgeUpdates.remoteMu.Lock()
				for _, memo := range app.knowledgeUpdates.remote {
					memo.checked = time.Time{}
				}
				app.knowledgeUpdates.remoteMu.Unlock()
				return app.knowledgeRemoteCached(context.Background(), ar, false, 2, 1)
			}
			step(a, ar)
			r := step(a, ar)
			if !r.coverage.Progressive || r.coverage.Files != 2 || r.coverage.TotalKnown {
				t.Fatalf("no bounded progression: %+v %s", r.coverage, r.message)
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
				t.Fatal("priority absent")
			}
			restarted, err := New(a.workPath, a.refPath, a.dataPath)
			if err != nil {
				t.Fatal(err)
			}
			defer restarted.Close()
			restarted.curlBin = stub
			ar = area(restarted)
			r = step(restarted, ar)
			if r.coverage.Files <= 2 {
				t.Fatalf("checkpoint did not resume: %+v %s", r.coverage, r.message)
			}
			for i := 0; i < 5 && !r.coverage.TotalKnown; i++ {
				r = step(restarted, ar)
			}
			if r.coverage.Files != 6 || !r.coverage.TotalKnown {
				t.Fatalf("incomplete catalogue: %+v %s", r.coverage, r.message)
			}
		})
	}
}
