package server

import (
	"archive/zip"
	"bytes"
	"io"
	"net/url"
	"strings"
	"testing"
)

func makeZIP(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, content); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestWorkspaceDownloadAndArchive(t *testing.T) {
	a := testApp(t)
	if err := a.workspace.MkdirAll("reports", 0755); err != nil {
		t.Fatal(err)
	}
	if err := a.workspace.WriteFile("reports/one.txt", []byte("one"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := a.workspace.WriteFile("reports/two.txt", []byte("two"), 0644); err != nil {
		t.Fatal(err)
	}

	direct := request(a, "GET", "/api/file/download?root=workspace&path=reports%2Fone.txt", nil)
	requireStatus(t, direct, 200)
	if got := direct.Header().Get("Content-Disposition"); !strings.Contains(got, "one.txt") {
		t.Fatalf("content disposition = %q", got)
	}
	if got := direct.Body.String(); got != "one" {
		t.Fatalf("download = %q", got)
	}

	w := request(a, "GET", "/api/file/download?root=workspace&path=reports", nil)
	requireStatus(t, w, 200)
	zr, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		r, e := f.Open()
		if e != nil {
			t.Fatal(e)
		}
		b, _ := io.ReadAll(r)
		r.Close()
		got[f.Name] = string(b)
	}
	if got["reports/one.txt"] != "one" || got["reports/two.txt"] != "two" {
		t.Fatalf("archive entries = %#v", got)
	}
}

func TestReferenceDownloadAndSafeExtraction(t *testing.T) {
	a := testApp(t)
	if err := a.reference.WriteFile("reference.txt", []byte("reference"), 0644); err != nil {
		t.Fatal(err)
	}
	w := request(a, "GET", "/api/file/download?source=context&path=reference.txt", nil)
	requireStatus(t, w, 200)
	if w.Body.String() != "reference" {
		t.Fatalf("reference download = %q", w.Body.String())
	}

	if err := a.workspace.WriteFile("bundle.zip", makeZIP(t, map[string]string{"nested/readme.txt": "safe"}), 0644); err != nil {
		t.Fatal(err)
	}
	w = request(a, "POST", "/api/file/extract", map[string]string{"root": "workspace", "path": "bundle.zip"})
	requireStatus(t, w, 200)
	b, err := a.workspace.ReadFile("bundle/nested/readme.txt")
	if err != nil || string(b) != "safe" {
		t.Fatalf("extracted = %q, %v", b, err)
	}
	w = request(a, "POST", "/api/file/extract", map[string]string{"root": "workspace", "path": "bundle.zip"})
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"path":"bundle (2)"`) {
		t.Fatalf("repeat extraction path = %s, want a new numbered folder", w.Body.String())
	}
	b, err = a.workspace.ReadFile("bundle/nested/readme.txt")
	if err != nil || string(b) != "safe" {
		t.Fatalf("existing extraction target was altered: %q, %v", b, err)
	}
	b, err = a.workspace.ReadFile("bundle (2)/nested/readme.txt")
	if err != nil || string(b) != "safe" {
		t.Fatalf("numbered extraction = %q, %v", b, err)
	}
	if err := a.workspace.WriteFile("windows-style.zip", makeZIP(t, map[string]string{".\\": "", "./nested\\notes.txt": "portable"}), 0644); err != nil {
		t.Fatal(err)
	}
	w = request(a, "POST", "/api/file/extract", map[string]string{"root": "workspace", "path": "windows-style.zip"})
	requireStatus(t, w, 200)
	b, err = a.workspace.ReadFile("windows-style/nested/notes.txt")
	if err != nil || string(b) != "portable" {
		t.Fatalf("normalized archive extraction = %q, %v", b, err)
	}

	if err := a.workspace.WriteFile("bad.zip", makeZIP(t, map[string]string{"../escape.txt": "bad"}), 0644); err != nil {
		t.Fatal(err)
	}
	w = request(a, "POST", "/api/file/extract", map[string]string{"root": "workspace", "path": "bad.zip"})
	requireStatus(t, w, 400)
	if _, err := a.workspace.Stat("bad"); err == nil {
		t.Fatal("rejected archive left an extraction directory")
	}
	if _, err := a.workspace.Stat("escape.txt"); err == nil {
		t.Fatal("zip path traversal wrote a file")
	}

	w = request(a, "GET", "/api/file/download?root=workspace&path="+url.QueryEscape("../escape.txt"), nil)
	requireStatus(t, w, 400)
}

func TestReadArchiveFilesRejectsPortablePathTraversal(t *testing.T) {
	for _, name := range []string{"../escape.txt", "folder/../../escape.txt", `/absolute.txt`, `C:\outside.txt`, `\\server\share\outside.txt`} {
		t.Run(name, func(t *testing.T) {
			if _, err := readArchiveFiles(makeZIP(t, map[string]string{name: "nope"})); err == nil {
				t.Fatalf("unsafe archive path %q was accepted", name)
			}
		})
	}
}

func TestReadArchiveFilesDeduplicatesNormalizedPaths(t *testing.T) {
	data := makeZIP(t, map[string]string{"folder/file.txt": "one", `folder\file.txt`: "two"})
	if _, err := readArchiveFiles(data); err == nil || !strings.Contains(err.Error(), "重复路径") {
		t.Fatalf("canonical duplicate paths: got %v, want duplicate-path error", err)
	}
}
