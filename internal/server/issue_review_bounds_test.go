package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIssue72ConfigExportRejectsBadBodies(t *testing.T) {
	a := testApp(t)
	for _, body := range []string{`{"includeSecrets":true,`, `{} {}`, `{"ignored":"` + strings.Repeat("x", 1<<20) + `"}`} {
		r := httptest.NewRequest("POST", "/api/config/export", strings.NewReader(body))
		w := httptest.NewRecorder()
		a.exportConfigBackup(w, r)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("bad body len=%d status=%d", len(body), w.Code)
		}
	}
	for _, body := range []string{"", `{}`, `{"includeSecrets":false}`} {
		w := httptest.NewRecorder()
		a.exportConfigBackup(w, httptest.NewRequest("POST", "/api/config/export", strings.NewReader(body)))
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
func TestIssue70SQLitePaginationBounds(t *testing.T) {
	a := testApp(t)
	for _, q := range []string{"limit=-1", "limit=0", "limit=1001", "limit=abc", "offset=-1", "offset=abc", "offset=9999999999999999999999999"} {
		w := request(a, "GET", "/api/sqlite/data?root=workspace&path=absent.db&table=records&"+q, nil)
		if w.Code != 400 || !strings.Contains(w.Body.String(), "pagination") {
			t.Fatalf("%s status=%d body=%s", q, w.Code, w.Body.String())
		}
	}
	db := filepath.Join(a.workPath, "paging.db")
	if out, err := exec.Command("python3", "-c", `import sqlite3,sys
c=sqlite3.connect(sys.argv[1]);c.execute('create table records(n integer)');c.executemany('insert into records values (?)',[(i,) for i in range(1200)]);c.commit()`, db).CombinedOutput(); err != nil {
		t.Fatalf("sqlite fixture: %v %s", err, out)
	}
	defer os.Remove(db)
	for _, q := range []struct {
		query        string
		count, first int
	}{{"", 100, 0}, {"&limit=1000", 1000, 0}, {"&limit=2&offset=10", 2, 10}} {
		w := request(a, "GET", "/api/sqlite/data?root=workspace&path=paging.db&table=records"+q.query, nil)
		requireStatus(t, w, 200)
		var got struct {
			Rows  []map[string]int `json:"rows"`
			Total int              `json:"total"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if len(got.Rows) != q.count || got.Rows[0]["n"] != q.first || got.Total != 1200 {
			t.Fatalf("%s: %+v", q.query, got)
		}
	}
}
func TestIssue62BriefingSortsInstantsBeforeTruncation(t *testing.T) {
	a := testApp(t)
	s := &Session{ID: "s", Runs: []*Task{{ID: "run", Prompt: "[project-name:Project X]", WorkspaceID: a.wsID()}}}
	for i := 0; i < 100; i++ {
		s.Messages = append(s.Messages, Message{Role: "user", Content: "older", SourceRunID: "run", CreatedAt: "2026-10-24T02:00:00+08:00"})
	}
	s.Messages = append(s.Messages, Message{Role: "user", Content: "newer", SourceRunID: "run", CreatedAt: "2026-10-23T20:00:00Z"})
	a.sessions = map[string]*Session{"s": s}
	start, _ := time.Parse(time.RFC3339, "2026-10-23T00:00:00Z")
	rows, omitted := a.projectMessagesLocked("Project X", "", "", start, start.Add(48*time.Hour))
	if len(rows) != 100 {
		t.Fatalf("rows=%d want100 omitted=%d", len(rows), omitted)
	}
	if omitted != 1 || rows[0].Content != "newer" || rows[0].At != "2026-10-23T20:00:00Z" {
		t.Fatalf("incorrect chronological truncation: count=%d omitted=%d first=%+v", len(rows), omitted, rows[0])
	}
}

func TestIssue67VaultPersistenceFailureRollsBack(t *testing.T) {
	a := testApp(t)
	if err := a.storeModelAPIKeyPlaintextLocked("old-synthetic-key"); err != nil {
		t.Fatal(err)
	}
	good := a.vault.path
	a.vault.path = t.TempDir() // rename onto a directory fails even as root
	for _, body := range []map[string]any{{"apiKey": "new-synthetic-key"}, {"clearKey": true}} {
		w := request(a, "PUT", "/api/settings", body)
		requireStatus(t, w, 500)
		plain, err := a.vault.Get(VaultIDModelAPIKey)
		if err != nil {
			t.Fatal(err)
		}
		if string(plain) != "old-synthetic-key" {
			t.Fatal("failed write changed active credential")
		}
		zeroBytes(plain)
	}
	a.vault.path = good
	disk, err := newSecretVault(a.dataPath)
	if err != nil {
		t.Fatal(err)
	}
	if err = disk.Load(); err != nil {
		t.Fatal(err)
	}
	disk.Unlock(a.accessTokenVaultKey())
	defer disk.Lock()
	plain, err := disk.Get(VaultIDModelAPIKey)
	if err != nil {
		t.Fatal(err)
	}
	defer zeroBytes(plain)
	if string(plain) != "old-synthetic-key" {
		t.Fatal("durable prior key changed")
	}
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{"clearKey": true}), 200)
	if a.vault.Has(VaultIDModelAPIKey) {
		t.Fatal("successful deletion retained credential")
	}
}
func TestIssue105SessionPersistenceRejectsUnsafeIDs(t *testing.T) {
	a := testApp(t)
	for _, id := range []string{"", "x/../../../../escaped", `x\..\escaped`, "C:evil", "bad\x00id", "../x", strings.Repeat("x", 257)} {
		if err := a.save(&Session{ID: id}); err == nil {
			t.Fatalf("accepted %q", id)
		}
		if p := SessionPath(a.dataPath, id, "active"); p != "" {
			t.Fatalf("unsafe path returned: %q", p)
		}
	}
	for _, id := range []string{"abc123", "legacy-id_123"} {
		if err := a.save(&Session{ID: id}); err != nil {
			t.Fatal(err)
		}
	}
}
func TestIssue105StartupSkipsInvalidSessionWithoutDeletingIt(t *testing.T) {
	root := t.TempDir()
	work, ref, data := filepath.Join(root, "work"), filepath.Join(root, "ref"), filepath.Join(root, "data")
	for _, dir := range []string{work, ref, SessionsActiveDir(data)} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	raw := []byte(`{"id":"x/../../../../escaped","title":"synthetic invalid session"}`)
	fixture := filepath.Join(SessionsActiveDir(data), "session-safe.json")
	if err := os.WriteFile(fixture, raw, 0600); err != nil {
		t.Fatal(err)
	}
	a, err := New(work, ref, data)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if _, ok := a.sessions["x/../../../../escaped"]; ok {
		t.Fatal("invalid session loaded")
	}
	preserved, err := os.ReadFile(fixture)
	if err != nil || string(preserved) != string(raw) {
		t.Fatal("invalid input was deleted or overwritten", err)
	}
	if _, err := os.Stat(filepath.Join(root, "escaped.json")); !os.IsNotExist(err) {
		t.Fatal("escaped write exists", err)
	}
}

func TestIssue65PrereleaseOrdering(t *testing.T) {
	for _, x := range []struct {
		old, next string
		down      bool
	}{{"1.2.0-beta.2", "1.2.0-beta.1", true}, {"1.2.0-rc.10", "1.2.0-rc.2", true}, {"1.2.0-rc.2", "1.2.0-rc.10", false}, {"1.2.0-rc.1", "1.2.0", false}, {"1.2.0", "1.2.0-rc.1", true}, {"1.2.0+old", "1.2.0+new", false}, {"0.1.17.5-RC12", "0.1.17.5-RC11", true}} {
		if got := pluginVersionDowngrade(x.old, x.next); got != x.down {
			t.Fatalf("%+v got=%t", x, got)
		}
	}
}
func TestIssue74RejectsRemoteWorkspaceBatchInjection(t *testing.T) {
	a := testApp(t)
	before := a.wsConfig.Workspace.Path
	for _, path := range []string{"/tmp/x\nrm y", "/tmp/x\rput y", "/tmp/x\x00"} {
		w := request(a, "PUT", "/api/workspace-config", map[string]any{"workspace": map[string]any{"mode": "ssh", "path": path, "auth": "none", "host": "localhost", "username": "user"}})
		if w.Code != 400 || a.wsConfig.Workspace.Path != before {
			t.Fatalf("unsafe path accepted or state changed: %d %s", w.Code, w.Body.String())
		}
	}
}
func TestIssue81FailedDirectoryRepairIsNotHealthy(t *testing.T) {
	data := t.TempDir()
	if err := EnsureDirs(data); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(SessionsActiveDir(data)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(SessionsActiveDir(data), []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	rep := SelfHeal(data, VerifyIntegrity(data))
	if rep.Status == IntegrityOK {
		t.Fatal("failed repair reports healthy", rep)
	}
	if err := os.Remove(SessionsActiveDir(data)); err != nil {
		t.Fatal(err)
	}
	repaired := SelfHeal(data, VerifyIntegrity(data))
	if repaired.Status == IntegrityCorrupted {
		t.Fatal("repaired directory remains corrupted", repaired)
	}
}
func TestIssue96RejectsFractionalRevision(t *testing.T) {
	a, _, d := pwFixture(t)
	for _, rev := range []any{0.9, "0", nil, -0.1} {
		if _, err := a.pwControlLocked(map[string]any{"action": "save", "definition": d, "revision": rev}); err == nil {
			t.Fatalf("accepted revision %v", rev)
		}
	}
	if _, err := a.pwControlLocked(map[string]any{"action": "save", "definition": d, "revision": float64(0)}); err != nil {
		t.Fatal(err)
	}
}
func TestIssue95BoundsAggregateProviderStream(t *testing.T) {
	for _, field := range []string{"content", "tool_calls"} {
		t.Run(field, func(t *testing.T) {
			fragment := strings.Repeat("x", 128<<10)
			delta := map[string]any{"content": fragment}
			if field == "tool_calls" {
				delta = map[string]any{"tool_calls": []any{map[string]any{"index": 0, "function": map[string]any{"arguments": fragment}}}}
			}
			chunk, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": delta}}})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				for i := 0; i < 80; i++ {
					if _, err := w.Write(append(append([]byte("data: "), chunk...), []byte("\n\n")...)); err != nil {
						return
					}
				}
			}))
			defer srv.Close()
			text, calls, _, _, err := completeStream(context.Background(), Settings{BaseURL: srv.URL, Model: "synthetic"}, []Message{{Role: "user", Content: "test"}}, ProfileParams{}, nil, nil, nil, nil)
			if err == nil || !strings.Contains(err.Error(), "byte limit") || len(text) > 8<<20 || len(calls) != 0 {
				t.Fatalf("not bounded: bytes=%d calls=%d err=%v", len(text), len(calls), err)
			}
		})
	}
}

func TestIssue111ImportCannotReportFailedSecretWritesAsSuccess(t *testing.T) {
	for _, kind := range []string{"source", "legacy-workspace", "vault"} {
		t.Run(kind, func(t *testing.T) {
			a := testApp(t)
			bk := configBackup{Format: configBackupFormat, FormatVersion: configBackupVersion, Settings: json.RawMessage(`{}`), IncludeSecrets: true}
			switch kind {
			case "source":
				bk.SourcesSecrets = json.RawMessage(`{"secrets":{}}`)
				if err := os.Mkdir(SourcesSecretsPath(a.dataPath), 0700); err != nil {
					t.Fatal(err)
				}
			case "legacy-workspace":
				bk.WorkspaceSecrets = json.RawMessage(`{"password":"synthetic"}`)
				if err := os.Mkdir(WorkspaceSecretsPath(a.dataPath), 0700); err != nil {
					t.Fatal(err)
				}
			case "vault":
				bk.WorkspaceSecrets = json.RawMessage(`{"version":1,"entries":[{"id":"synthetic-import","type":"ssh-password"}]}`)
				a.vault.path = t.TempDir()
			}
			requireStatus(t, request(a, "POST", "/api/config/import", map[string]any{"backup": bk, "importSecrets": true}), 500)
			if kind == "vault" && a.vault.Has("synthetic-import") {
				t.Fatal("failed import retained unsaved entry")
			}
		})
	}
}
