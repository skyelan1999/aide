package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Called only by the opt-in isolated dual-server SSH acceptance test.
func runSSHContinuity(t *testing.T, a *App, workspace, docs, cache, independentSource string, remote func(*testing.T, string) string) {
	enableMarkdownHistory(t, a)
	t.Run("generated_document_follows_binding", func(t *testing.T) {
		result := a.createDesign("bound SSH document", "## 开发流程\n真实文档路径验收", "", "")
		if !strings.Contains(result, "已建档") || !strings.Contains(result, path.Join(docs, "designs")+"/") {
			t.Fatal(result)
		}
		entries, err := a.sftpListRemote(path.Join(docs, "designs"), "designs")
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, entry := range entries {
			name, _ := entry["name"].(string)
			if strings.HasPrefix(name, "DESIGN-") {
				b, err := a.sftpRead(path.Join(docs, "designs", name))
				if err != nil || !strings.Contains(string(b), "真实文档路径验收") {
					t.Fatalf("document readback: %q %v", b, err)
				}
				found = true
			}
		}
		if !found {
			t.Fatal("bound document absent")
		}
	})
	for _, target := range []struct{ name, source, base string }{
		{"workspace", "", workspace},
		{"system_docs", systemDocsSource, docs},
		{"independent_sftp", independentSource, "/srv/refs"},
	} {
		t.Run(target.name, func(t *testing.T) {
			dir := "continuity-" + newID()
			var independent *Source
			if target.source == independentSource {
				src, ok := a.findSource(independentSource)
				if !ok {
					t.Fatal("independent source missing")
				}
				independent = &src
				requireStatus(t, request(a, "POST", "/api/directory", directoryRequest{Root: "context", Source: target.source, Parent: ".", Name: dir}), 200)
			} else {
				remote(t, "mkdir -p "+shellQuote(path.Join(target.base, dir)))
			}
			put := func(p, text string) {
				t.Helper()
				if independent != nil {
					if err := a.sftpWriteSource(*independent, path.Join(dir, p), []byte(text)); err != nil {
						t.Fatal(err)
					}
				} else {
					remote(t, "printf '%s' "+shellQuote(text)+" > "+shellQuote(path.Join(target.base, dir, p)))
				}
			}
			read := func(p, want string) {
				t.Helper()
				var b []byte
				var err error
				if independent != nil {
					b, err = a.sftpReadSource(*independent, path.Join(dir, p))
				} else {
					b, err = a.sftpRead(path.Join(target.base, dir, p))
				}
				if err != nil || string(b) != want {
					t.Fatalf("%s: %q %v", p, b, err)
				}
			}
			put("page.md", "# Original\n![diagram](image.svg)")
			put("image.svg", "<svg>original</svg>")
			q := url.Values{"path": {path.Join(dir, "page.md")}}
			if target.source != "" {
				q.Set("source", target.source)
			}
			list := func(want int) {
				t.Helper()
				w := request(a, "GET", "/api/file/history?"+q.Encode(), nil)
				requireStatus(t, w, 200)
				var result struct {
					Total int `json:"total"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Total != want {
					t.Fatalf("history count want %d: %s (%v)", want, w.Body.String(), err)
				}
			}
			list(1)
			list(1)
			put("image.svg", "<svg>external asset edit</svg>")
			list(2)
			list(2)
			put("page.md", "new document")
			put("unrelated.txt", "keep")
			preview := func() markdownRestorePlan {
				t.Helper()
				v := url.Values{}
				for key, values := range q {
					v[key] = values
				}
				v.Set("revision", "000001")
				v.Set("assets", "1")
				w := request(a, "GET", "/api/file/history/restore?"+v.Encode(), nil)
				requireStatus(t, w, 200)
				var plan markdownRestorePlan
				if err := json.Unmarshal(w.Body.Bytes(), &plan); err != nil {
					t.Fatal(err)
				}
				if len(plan.Files) != 2 {
					t.Fatalf("body and resource expected: %+v", plan.Files)
				}
				return plan
			}
			body := func(plan markdownRestorePlan) map[string]any {
				expected := map[string]string{}
				for _, f := range plan.Files {
					expected[f.Path] = f.Before
				}
				return map[string]any{"path": q.Get("path"), "source": target.source, "workspaceId": a.wsID(), "identity": plan.Identity, "revision": "000001", "assets": true, "expected": expected}
			}
			plan := preview()
			put("image.svg", "conflict after preview")
			requireStatus(t, request(a, "POST", "/api/file/history/restore", body(plan)), 409)
			read("page.md", "new document")
			read("image.svg", "conflict after preview")
			plan = preview()
			w := request(a, "POST", "/api/file/history/restore", body(plan))
			requireStatus(t, w, 200)
			read("page.md", "# Original\n![diagram](image.svg)")
			read("image.svg", "<svg>original</svg>")
			read("unrelated.txt", "keep")
			var result struct {
				Journal string `json:"journal"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(filepath.Join(a.markdownHistoryDir(plan.Identity), "restores", result.Journal+".json"))
			if err != nil {
				t.Fatal(err)
			}
			var journal markdownRestoreJournal
			if err := json.Unmarshal(raw, &journal); err != nil {
				t.Fatal(err)
			}
			if journal.State != "completed" || journal.Pending != "" {
				t.Fatalf("journal incomplete: %+v", journal)
			}
			for _, f := range journal.Plan.Files {
				if !f.Applied || !f.Verified {
					t.Fatalf("unverified restore: %+v", f)
				}
			}
			if independent != nil {
				if _, err := a.sftpRead(path.Join(target.base, dir, "page.md")); err == nil || !isSFTPNotExistErr(err) {
					t.Fatalf("server B restore leaked to A or could not confirm absence: %v", err)
				}
			}
		})
	}
	t.Run("token_memory_remote_roundtrip", func(t *testing.T) {
		text := "偏好 中文记忆 🛰；流程 保留 SSH 同步证据"
		if result := a.writeMemory(text); result != "已写入 token 记忆。" {
			t.Fatal(result)
		}
		dir := a.projectCacheDir()
		for _, name := range []string{"memory.md", memoryTokenFile} {
			local, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			b, err := a.sftpRead(path.Join(cache, "aide", name))
			if err != nil || !bytes.Equal(b, local) {
				t.Fatalf("remote %s differs: %v", name, err)
			}
			if err := os.Remove(filepath.Join(dir, name)); err != nil {
				t.Fatal(err)
			}
		}
		if result := a.readMemory(); !strings.Contains(result, text) {
			t.Fatalf("remote read lost memory: %s", result)
		}
		store, _, err := loadMemoryTokenStore(dir)
		if err != nil || !strings.Contains(store.text(), text) || store.Tokenizer != "unicode-scalar-v1" {
			t.Fatalf("token roundtrip: %+v %v", store, err)
		}
	})
	for _, source := range []string{systemDocsSource, independentSource} {
		t.Run("source_actions_"+source, func(t *testing.T) {
			dir := "操作 中文-" + newID()
			requireStatus(t, request(a, "POST", "/api/directory", directoryRequest{Root: "context", Source: source, Parent: ".", Name: dir}), 200)
			body := func(p string) map[string]string {
				return map[string]string{"root": "context", "source": source, "path": p}
			}
			file := path.Join(dir, "原始.txt")
			requireStatus(t, request(a, "PUT", "/api/file", map[string]string{"source": source, "path": file, "content": "original bytes 中文"}), 200)
			q := url.Values{"source": {source}, "root": {"context"}, "path": {dir}}
			w := request(a, "GET", "/api/file/properties?"+q.Encode(), nil)
			requireStatus(t, w, 200)
			var properties map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &properties); err != nil {
				t.Fatal(err)
			}
			if properties["source"] != source || properties["dir"] != true || properties["rw"] != true {
				t.Fatalf("source properties: %s", w.Body.String())
			}
			renamed := dir + " 重命名"
			b := body(dir)
			b["newName"] = renamed
			requireStatus(t, request(a, "POST", "/api/file/rename", b), 200)
			requireStatus(t, request(a, "POST", "/api/file/archive", body(renamed)), 200)
			requireStatus(t, request(a, "POST", "/api/file/archive", body(renamed)), 409)
			w = request(a, "POST", "/api/file/extract", body(renamed+".zip"))
			requireStatus(t, w, 200)
			var extracted struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &extracted); err != nil {
				t.Fatal(err)
			}
			if extracted.Path == "" || extracted.Path == renamed {
				t.Fatalf("extraction must preserve original: %s", w.Body.String())
			}
			// ZIP preserves the archived directory name under the extraction root.
			q.Set("path", path.Join(extracted.Path, path.Base(renamed), "原始.txt"))
			w = request(a, "GET", "/api/file?"+q.Encode(), nil)
			requireStatus(t, w, 200)
			var restored map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &restored); err != nil || restored["content"] != "original bytes 中文" {
				t.Fatalf("extracted content: %s %v", w.Body.String(), err)
			}
			for _, p := range []string{renamed, renamed + ".zip", extracted.Path} {
				requireStatus(t, request(a, "POST", "/api/file/delete", body(p)), 200)
				q.Set("path", p)
				requireStatus(t, request(a, "GET", "/api/file/properties?"+q.Encode(), nil), 400)
			}
		})
	}
	t.Run("remote_knowledge_search_and_timeline", func(t *testing.T) {
		src, ok := a.findSource(independentSource)
		if !ok {
			t.Fatal("independent source missing")
		}
		name := "evidence-" + newID() + ".md"
		oldText := "# SSH evidence\nunique-remote-claim before"
		newText := "# SSH evidence\nunique-remote-claim after"
		if err := a.sftpWriteSource(src, name, []byte(oldText)); err != nil {
			t.Fatal(err)
		}
		// Expire the test instance's polling clocks without sleeping. Reads still
		// use real SSH and normal scan APIs; this does not prove wall-clock UI polling.
		expire := func() {
			a.knowledgeUpdates.remoteMu.Lock()
			for _, memo := range a.knowledgeUpdates.remote {
				memo.checked = time.Time{}
			}
			a.knowledgeUpdates.remoteMu.Unlock()
			for _, view := range a.knowledgeUpdates.views {
				if view != nil {
					view.checked = time.Time{}
				}
			}
		}
		scan := func() knowledgeUpdateResponse {
			t.Helper()
			expire()
			w := request(a, "GET", "/api/knowledge-map/updates", nil)
			requireStatus(t, w, 200)
			var g knowledgeUpdateResponse
			if err := json.Unmarshal(w.Body.Bytes(), &g); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, n := range g.Nodes {
				if n.Source == independentSource && n.Path == name && n.SourceType == "sftp" {
					found = true
				}
			}
			if !found {
				t.Fatalf("remote node missing; sources=%+v warnings=%v", g.Sources, g.Warnings)
			}
			return g
		}
		g := scan()
		search := func(want string) documentHit {
			t.Helper()
			w := request(a, "POST", "/api/knowledge-map/documents/search", documentRequest{Query: "unique-remote-claim", Mode: "original", Workspace: g.Workspace, Source: independentSource, Path: name})
			requireStatus(t, w, 200)
			var result documentResult
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			for _, h := range result.Hits {
				if h.File.Source == independentSource && h.File.Path == name && strings.Contains(h.Text, want) && h.Hash != "" && h.Locator != "" {
					return h
				}
			}
			t.Fatalf("no exact remote evidence: %+v", result)
			return documentHit{}
		}
		first := search("before")
		if err := a.sftpWriteSource(src, name, []byte(newText)); err != nil {
			t.Fatal(err)
		}
		requireStatus(t, request(a, "POST", "/api/knowledge-map/documents/reference", map[string]any{"id": first.File.ID, "locator": first.Locator, "offset": first.Offset, "hash": first.Hash, "workspace": g.Workspace, "origin": first.File.Origin}), 409)
		// A progressive scan may still be completing another directory. Wait for
		// actual re-observation of this file, rather than assuming one poll is a
		// full remote rescan. The test clock is accelerated, not the SSH reads.
		observed := false
		for cycle := 0; cycle < 12; cycle++ {
			g = scan()
			for _, n := range g.Nodes {
				if n.Source == independentSource && n.Path == name && n.Text == newText {
					observed = true
				}
			}
			if observed {
				break
			}
		}
		if !observed {
			t.Fatal("remote graph never re-observed edited file")
		}
		second := search("after")
		if first.Hash == second.Hash {
			t.Fatal("content hash did not change")
		}
		store, err := loadKnowledgeTime(knowledgeTimeFile(a.dataPath, g.Workspace, false), g.Workspace, false, knowledgeTimeKey(a.token))
		if err != nil || len(store.Snapshots) < 2 {
			t.Fatalf("remote timeline: %d snapshots %v", len(store.Snapshots), err)
		}
		comparison := compareKnowledgeTime(store.Snapshots[0], store.Snapshots[len(store.Snapshots)-1])
		found := false
		for _, change := range comparison.Changed {
			if change.Before.Source == independentSource && change.Before.Path == name && change.Before.Text == oldText && change.After.Text == newText {
				found = true
			}
		}
		if !found {
			t.Fatalf("remote before/after evidence absent: %+v", comparison.Changed)
		}
	})
	t.Run("remote_progressive_coverage", func(t *testing.T) {
		src, ok := a.findSource(independentSource)
		if !ok {
			t.Fatal("source missing")
		}
		dir := "progressive-" + newID()
		requireStatus(t, request(a, "POST", "/api/directory", directoryRequest{Root: "context", Source: independentSource, Parent: ".", Name: dir}), 200)
		for i := 0; i < 50; i++ {
			if err := a.sftpWriteSource(src, path.Join(dir, fmt.Sprintf("file-%03d.md", i)), []byte("real remote progressive evidence")); err != nil {
				t.Fatal(err)
			}
		}
		w := request(a, "GET", "/api/knowledge-map/index-policy", nil)
		requireStatus(t, w, 200)
		var config struct {
			Policy      knowledgeIndexPolicy `json:"policy"`
			Revision    string               `json:"revision"`
			WorkspaceID string               `json:"workspaceId"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &config); err != nil {
			t.Fatal(err)
		}
		config.Policy.FileBudget = 50
		config.Policy.DirectoryBudget = 20
		config.Policy.ScopePaths = map[string][]string{independentSource: {dir}}
		config.Policy.PriorityPaths = map[string][]string{independentSource: {path.Join(dir, "file-049.md")}}
		requireStatus(t, request(a, "PUT", "/api/knowledge-map/index-policy", config), 200)
		previous := 0
		completed := false
		partial := false
		memoReset := false
		resumeAfter := 0
		for step := 0; step < 12; step++ {
			a.knowledgeUpdates.remoteMu.Lock()
			for _, m := range a.knowledgeUpdates.remote {
				m.checked = time.Time{}
			}
			a.knowledgeUpdates.remoteMu.Unlock()
			for _, v := range a.knowledgeUpdates.views {
				if v != nil {
					v.checked = time.Time{}
				}
			}
			w := request(a, "GET", "/api/knowledge-map/updates", nil)
			requireStatus(t, w, 200)
			var g knowledgeUpdateResponse
			if err := json.Unmarshal(w.Body.Bytes(), &g); err != nil {
				t.Fatal(err)
			}
			var coverage *knowledgeCoverage
			for _, s := range g.Sources {
				if s.ID == independentSource {
					coverage = s.Coverage
				}
			}
			if coverage == nil || !coverage.Progressive {
				t.Fatalf("progressive coverage missing: %s", w.Body.String())
			}
			if coverage.Files < previous {
				t.Fatal("first cycle lost accumulated files")
			}
			if resumeAfter > 0 {
				if coverage.Files <= resumeAfter {
					t.Fatal("disk cursor did not continue after memo reset")
				}
				resumeAfter = 0
			}
			previous = coverage.Files
			foundPriority := false
			for _, n := range g.Nodes {
				if n.Source == independentSource && n.Kind == "file" {
					if !strings.HasPrefix(n.Path, dir+"/") {
						t.Fatal("scope leak", n.Path)
					}
					if n.Path == path.Join(dir, "file-049.md") {
						foundPriority = true
					}
				}
			}
			if coverage.Files > 0 && !foundPriority {
				t.Fatal("priority file not discovered first")
			}
			if !coverage.TotalKnown {
				partial = true
				if !memoReset && coverage.Files > 0 {
					a.mu.Lock()
					var currentArea knowledgeArea
					for _, area := range a.knowledgeAreasLocked(a.wsID(), a.workspace.Name(), a.reference.Name()) {
						if area.source == independentSource {
							currentArea = area
						}
					}
					a.mu.Unlock()
					if currentArea.source == "" {
						t.Fatal("current source area absent")
					}
					// Keep the real application data directory, discard only volatile
					// source memos. The next authenticated API request must resume disk.
					a.knowledgeUpdates.remoteMu.Lock()
					files, dirs := 0, 0
					for _, memo := range a.knowledgeUpdates.remote {
						if memo.origin == currentArea.origin {
							files, dirs = memo.fileLimit, memo.dirLimit
							break
						}
					}
					a.knowledgeUpdates.remote = nil
					a.knowledgeUpdates.remoteMu.Unlock()
					if files == 0 || dirs == 0 {
						t.Fatal("source budgets absent")
					}
					checkpoint := knowledgeCursorFile(a.dataPath, currentArea, false)
					before, err := os.ReadFile(checkpoint)
					if err != nil {
						t.Fatal(err)
					}
					cancelled, cancel := context.WithCancel(context.Background())
					cancel()
					preserved := a.knowledgeRemoteCached(cancelled, currentArea, false, files, dirs)
					if preserved.state != "unavailable" || preserved.coverage == nil || preserved.coverage.TotalKnown || preserved.coverage.Files != coverage.Files {
						t.Fatal("failed restored scan lost observations or claimed complete coverage")
					}
					after, err := os.ReadFile(checkpoint)
					if err != nil || !bytes.Equal(before, after) {
						t.Fatal("failed candidate overwrote checkpoint")
					}
					bin, err := os.Executable()
					if err != nil {
						t.Fatal(err)
					}
					receiptFile := filepath.Join(t.TempDir(), "restart-receipt.json")
					input, err := json.Marshal(knowledgeCursorProcessInput{a.workPath, a.refPath, a.dataPath, independentSource, receiptFile, files, dirs})
					if err != nil {
						t.Fatal(err)
					}
					processCtx, stop := context.WithTimeout(context.Background(), 30*time.Second)
					cmd := exec.CommandContext(processCtx, bin, "-test.run=^TestSSHKnowledgeCursorRestartHelper$", "-test.v")
					cmd.Env = append(os.Environ(), "AIDE_TEST_CURSOR_PROCESS="+string(input))
					output, err := cmd.CombinedOutput()
					stop()
					if err != nil {
						t.Fatalf("fresh process restart failed: %v\n%s", err, output)
					}
					var receipt knowledgeCursorProcessReceipt
					b, err := os.ReadFile(receiptFile)
					if err != nil || json.Unmarshal(b, &receipt) != nil || receipt.Before != coverage.Files || receipt.After <= receipt.Before || receipt.Origin == currentArea.origin {
						t.Fatalf("invalid process restart receipt: %+v %v", receipt, err)
					}
					t.Logf("fresh process checkpoint resumed %d -> %d files; citation identity changed; live SSH reread passed", receipt.Before, receipt.After)
					a.knowledgeUpdates.remoteMu.Lock()
					// Force this process to reload the successor checkpoint, rather than
					// replay the earlier cursor preserved by the cancellation check.
					a.knowledgeUpdates.remote = nil
					a.knowledgeUpdates.remoteMu.Unlock()
					memoReset, resumeAfter = true, coverage.Files
				}
			}
			if coverage.TotalKnown {
				if coverage.Files != 50 || coverage.Pending != 0 {
					t.Fatalf("false completion: %+v", coverage)
				}
				completed = true
				break
			}
		}
		if !partial || !completed {
			t.Fatalf("no progressive completion: partial=%v complete=%v files=%d", partial, completed, previous)
		}
	})
}
