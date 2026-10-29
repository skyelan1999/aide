package server

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
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

func issue69Archive(t *testing.T, format string, entries []string) []byte {
	t.Helper()
	var raw bytes.Buffer
	if format == "zip" {
		zw := zip.NewWriter(&raw)
		for _, name := range entries {
			w, err := zw.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasSuffix(name, "/") {
				_, _ = io.WriteString(w, "fixture")
			}
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
	} else {
		tw := tar.NewWriter(&raw)
		for _, name := range entries {
			hdr := &tar.Header{Name: name, Mode: 0644, Typeflag: tar.TypeReg, Size: 7}
			if strings.HasSuffix(name, "/") {
				hdr.Typeflag, hdr.Size = tar.TypeDir, 0
			}
			if err := tw.WriteHeader(hdr); err != nil {
				t.Fatal(err)
			}
			if hdr.Size > 0 {
				_, _ = io.WriteString(tw, "fixture")
			}
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return raw.Bytes()
}

func TestIssue69ArchiveRejectsFileParentConflictsBeforeExtraction(t *testing.T) {
	for _, format := range []string{"zip", "tar"} {
		for n, entries := range [][]string{
			{"a", "a/b.txt"},
			{"a/b.txt", "a"},
			{"a/b.txt", "a/"}, // Valid explicit directory after its child.
			{"a", "a-unrelated", "a/b.txt"},
			{`a\b.txt`, "a"},
		} {
			t.Run(fmt.Sprintf("%s/%d", format, n), func(t *testing.T) {
				data := issue69Archive(t, format, entries)
				_, err := readArchiveFilesForPath(data, "fixture."+format)
				if n == 2 {
					if err != nil {
						t.Fatalf("valid late directory rejected: %v", err)
					}
					return
				}
				if err == nil {
					t.Fatal("file used as a parent directory was accepted")
				}
				a := testApp(t)
				name := "conflict." + format
				if err := a.workspace.WriteFile(name, data, 0600); err != nil {
					t.Fatal(err)
				}
				response := request(a, "POST", "/api/file/extract", map[string]string{"root": "workspace", "path": name})
				requireStatus(t, response, 400)
				items, err := os.ReadDir(a.workPath)
				if err != nil {
					t.Fatal(err)
				}
				for _, item := range items {
					if item.Name() == "conflict" || strings.HasPrefix(item.Name(), ".aide-unpack-") {
						t.Fatalf("rejected layout left output behind: %v", items)
					}
				}
			})
		}
	}
}

func TestIssue69FailedWorkspaceExtractionCleansStage(t *testing.T) {
	for _, format := range []string{"zip", "tar"} {
		t.Run(format, func(t *testing.T) {
			a := testApp(t)
			name := "failure." + format
			data := issue69Archive(t, format, []string{"good.txt", strings.Repeat("x", 256) + "/bad.txt"})
			if err := a.workspace.WriteFile(name, data, 0600); err != nil {
				t.Fatal(err)
			}
			response := request(a, "POST", "/api/file/extract", map[string]string{"root": "workspace", "path": name})
			if response.Code < 400 {
				t.Fatalf("unwritable archive path succeeded: %s", response.Body.String())
			}
			items, err := os.ReadDir(a.workPath)
			if err != nil {
				t.Fatal(err)
			}
			for _, item := range items {
				if item.Name() == "failure" || strings.HasPrefix(item.Name(), ".aide-unpack-") {
					t.Fatalf("failed extraction retained its private stage: %v", items)
				}
			}
		})
	}
}

func TestIssue69ArchivePublicationPreservesExistingDestination(t *testing.T) {
	a := testApp(t)
	if err := a.workspace.Mkdir("existing", 0755); err != nil {
		t.Fatal(err)
	}
	if err := a.workspace.WriteFile("existing/keep.txt", []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	fs, err := a.streamTransferFS(context.Background(), "workspace", true)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	if err := writeExtractedArchive(fs, "existing", []extractedArchiveFile{{path: "new.txt", data: []byte("new")}}); !errors.Is(err, os.ErrExist) {
		t.Fatalf("publication conflict = %v", err)
	}
	data, err := a.workspace.ReadFile("existing/keep.txt")
	if err != nil || string(data) != "original" {
		t.Fatal("existing archive destination changed", err)
	}
	items, err := os.ReadDir(a.workPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if strings.HasPrefix(item.Name(), ".aide-unpack-") {
			t.Fatal("failed publication left a staging directory")
		}
	}
}

func TestWorkspaceArchiveFormatsRoundTrip(t *testing.T) {
	a := testApp(t)
	for i, format := range []string{"zip", "tar", "tar.gz", "tar.bz2", "tar.xz"} {
		source := fmt.Sprintf("format-%d.txt", i)
		if err := a.workspace.WriteFile(source, []byte("archive round trip: "+format), 0644); err != nil {
			t.Fatal(err)
		}
		w := request(a, "POST", "/api/file/archive", map[string]string{"root": "workspace", "path": source, "format": format})
		requireStatus(t, w, 200)
		var created map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
			t.Fatal(err)
		}
		w = request(a, "POST", "/api/file/extract", map[string]string{"root": "workspace", "path": created["path"]})
		requireStatus(t, w, 200)
		var extracted map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &extracted); err != nil {
			t.Fatal(err)
		}
		got, err := a.workspace.ReadFile(path.Join(extracted["path"], source))
		if err != nil || string(got) != "archive round trip: "+format {
			t.Fatalf("%s round trip = %q, %v", format, got, err)
		}
	}
}

func TestTarExtractionRejectsTraversalAndLinks(t *testing.T) {
	for _, tc := range []struct {
		name string
		typ  byte
	}{
		{name: "../escape.txt", typ: tar.TypeReg},
		{name: "link", typ: tar.TypeSymlink},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var raw bytes.Buffer
			tw := tar.NewWriter(&raw)
			h := &tar.Header{Name: tc.name, Typeflag: tc.typ, Mode: 0644, Size: 3, Linkname: "../outside"}
			if err := tw.WriteHeader(h); err != nil {
				t.Fatal(err)
			}
			if tc.typ == tar.TypeReg {
				_, _ = tw.Write([]byte("bad"))
			}
			if err := tw.Close(); err != nil {
				t.Fatal(err)
			}
			files, err := readTarArchive(bytes.NewReader(raw.Bytes()))
			if err == nil || files != nil {
				t.Fatalf("unsafe TAR accepted: files=%#v err=%v", files, err)
			}
		})
	}
}
