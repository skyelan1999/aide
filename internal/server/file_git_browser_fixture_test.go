package server

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// Opt-in synthetic fixture for native-browser acceptance. It never mounts user
// workspaces or models. Publish port 18618 to loopback only when running it.
func TestFileGitHistoryBrowserFixture(t *testing.T) {
	if os.Getenv("AIDE_GIT_HISTORY_BROWSER_FIXTURE") != "1" {
		t.Skip("opt-in synthetic browser fixture")
	}
	a := testApp(t)
	enableMarkdownHistory(t, a)
	dir := filepath.Join(a.workspace.Name(), "repo")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	gitHistoryTestRun(t, dir, "init", "-q")
	for _, name := range []string{"note.txt", "page.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("title\nfirst committed line\nunchanged tail\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	gitHistoryTestRun(t, dir, "add", ".")
	gitHistoryTestRun(t, dir, "commit", "-qm", "First file revision")
	for _, name := range []string{"note.txt", "page.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("title\nsecond committed line\nunchanged tail\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	gitHistoryTestRun(t, dir, "add", ".")
	gitHistoryTestRun(t, dir, "commit", "-qm", "Second file revision")
	for _, name := range []string{"note.txt", "page.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("title\nstaged line\nunchanged tail\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	gitHistoryTestRun(t, dir, "add", ".")
	for _, name := range []string{"note.txt", "page.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("title\ndisk line\nunchanged tail\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(a.workspace.Name(), "standalone.txt"), []byte("not a Git repo"), 0600); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/file/git-history", a.fileGitHistoryAPI)
	mux.HandleFunc("GET /api/file/history", a.markdownHistoryAPI)
	mux.HandleFunc("GET /fixture", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "../../scripts/fixtures/git-history.html")
	})
	mux.HandleFunc("GET /fixture-config.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		fmt.Fprintf(w, "window.fixtureWorkspace=%q;", a.wsID())
	})
	mux.Handle("GET /", http.FileServer(http.Dir("web")))
	t.Log("Synthetic Git browser fixture listening on :18618; expose localhost only")
	server := &http.Server{Addr: ":18618", Handler: mux}
	t.Cleanup(func() { _ = server.Close() })
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		t.Fatal(err)
	}
}
