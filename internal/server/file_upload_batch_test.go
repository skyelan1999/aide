package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

type batchPart struct{ rel, content string }

type batchResp struct {
	Results []batchFileResult `json:"results"`
	Summary struct {
		Succeeded int `json:"succeeded"`
		Failed    int `json:"failed"`
	} `json:"summary"`
}

func batchUploadParts(a *App, target string, parts []batchPart) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, p := range parts {
		fw, err := mw.CreateFormFile("files", p.rel)
		if err != nil {
			panic(err)
		}
		if _, err := io.WriteString(fw, p.content); err != nil {
			panic(err)
		}
	}
	if err := mw.Close(); err != nil {
		panic(err)
	}
	r := httptest.NewRequest("POST", target, &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	a.uploadBatchFile(w, r)
	return w
}

func decodeBatch(t *testing.T, w *httptest.ResponseRecorder) batchResp {
	t.Helper()
	var br batchResp
	if err := json.Unmarshal(w.Body.Bytes(), &br); err != nil {
		t.Fatalf("batch response not JSON: %v body=%s", err, w.Body.String())
	}
	return br
}

func readBatchFile(t *testing.T, a *App, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(a.workPath, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("expected file %q on disk: %v", rel, err)
	}
	return string(b)
}

func TestBatchUploadNestedLayout(t *testing.T) {
	a := testApp(t)
	parts := []batchPart{
		{"a.txt", "root"},
		{"sub/dir/b.md", "nested"},
		{"sub/deep/c.bin", "deep"},
	}
	w := batchUploadParts(a, "/api/file/upload-batch?path=drop", parts)
	requireStatus(t, w, 201)
	br := decodeBatch(t, w)
	if br.Summary.Succeeded != 3 || br.Summary.Failed != 0 {
		t.Fatalf("summary: %+v body=%s", br.Summary, w.Body.String())
	}
	if got := readBatchFile(t, a, "drop/a.txt"); got != "root" {
		t.Fatalf("flat file: %q", got)
	}
	if got := readBatchFile(t, a, "drop/sub/dir/b.md"); got != "nested" {
		t.Fatalf("nested file: %q", got)
	}
	if got := readBatchFile(t, a, "drop/sub/deep/c.bin"); got != "deep" {
		t.Fatalf("deep file: %q", got)
	}
	w = batchUploadParts(a, "/api/file/upload-batch", []batchPart{{"top.txt", "at-root"}})
	requireStatus(t, w, 201)
	if got := readBatchFile(t, a, "top.txt"); got != "at-root" {
		t.Fatalf("root dest: %q", got)
	}
}

func TestBatchUploadTraversalRejectedPerFile(t *testing.T) {
	a := testApp(t)
	parts := []batchPart{
		{"good.txt", "ok"},
		{"../evil.txt", "evil"},
		{"/abs.txt", "abs"},
		{"sub/../inside.txt", "inside"},
	}
	w := batchUploadParts(a, "/api/file/upload-batch?path=drop", parts)
	requireStatus(t, w, 207)
	br := decodeBatch(t, w)
	if br.Summary.Succeeded != 2 || br.Summary.Failed != 2 {
		t.Fatalf("summary: %+v body=%s", br.Summary, w.Body.String())
	}
	byPath := map[string]batchFileResult{}
	for _, r := range br.Results {
		byPath[r.Path] = r
	}
	if !byPath["drop/good.txt"].OK || !byPath["drop/inside.txt"].OK {
		t.Fatalf("valid files should succeed: %+v", br.Results)
	}
	if r, bad := byPath["../evil.txt"]; bad && r.OK {
		t.Fatalf("dotdot must be rejected: %+v", r)
	}
	if r, bad := byPath["/abs.txt"]; bad && r.OK {
		t.Fatalf("absolute must be rejected: %+v", r)
	}
	if got := readBatchFile(t, a, "drop/good.txt"); got != "ok" {
		t.Fatalf("good.txt: %q", got)
	}
	if got := readBatchFile(t, a, "drop/inside.txt"); got != "inside" {
		t.Fatalf("inside.txt: %q", got)
	}
	if _, err := os.Stat(filepath.Join(a.workPath, "evil.txt")); !os.IsNotExist(err) {
		t.Fatalf("traversal escaped workspace root: %v", err)
	}
}

func TestCleanBatchRelStrict(t *testing.T) {
	cases := map[string]bool{
		"a.txt":           true,
		"dir/a.txt":       true,
		`..\win.txt`:      false,
		`dir\..\..\x`:     false,
		"..\x00evil.txt":  false,
		"line\nbreak.txt": false,
		"/etc/passwd":     false,
		"a/../../escape":  false,
		".git/config":     false,
	}
	for name, wantOK := range cases {
		_, err := cleanBatchRel(name)
		if wantOK && err != nil {
			t.Fatalf("cleanBatchRel(%q) unexpected error: %v", name, err)
		}
		if !wantOK && err == nil {
			t.Fatalf("cleanBatchRel(%q) expected rejection", name)
		}
	}
}

func TestBatchUploadConflictDoesNotAbort(t *testing.T) {
	a := testApp(t)
	if err := os.WriteFile(filepath.Join(a.workPath, "existing.txt"), []byte("orig"), 0644); err != nil {
		t.Fatal(err)
	}
	parts := []batchPart{
		{"existing.txt", "replacement"},
		{"fresh.txt", "new"},
	}
	w := batchUploadParts(a, "/api/file/upload-batch", parts)
	requireStatus(t, w, 207)
	br := decodeBatch(t, w)
	if br.Summary.Succeeded != 1 || br.Summary.Failed != 1 {
		t.Fatalf("summary: %+v body=%s", br.Summary, w.Body.String())
	}
	byPath := map[string]batchFileResult{}
	for _, r := range br.Results {
		byPath[r.Path] = r
	}
	if byPath["existing.txt"].OK || byPath["existing.txt"].Error != "\u540c\u540d\u6587\u4ef6\u5df2\u5b58\u5728" {
		t.Fatalf("conflict result: %+v", byPath["existing.txt"])
	}
	if !byPath["fresh.txt"].OK {
		t.Fatalf("sibling should succeed: %+v", byPath["fresh.txt"])
	}
	if got := readBatchFile(t, a, "fresh.txt"); got != "new" {
		t.Fatalf("fresh.txt: %q", got)
	}
	if got := readBatchFile(t, a, "existing.txt"); got != "orig" {
		t.Fatalf("existing file overwritten: %q", got)
	}
}

func TestBatchUploadHasNoFileCountCap(t *testing.T) {
	a := testApp(t)
	var parts []batchPart
	for i := 0; i < 1001; i++ {
		parts = append(parts, batchPart{fmt.Sprintf("file-%04d.txt", i), "x"})
	}
	w := batchUploadParts(a, "/api/file/upload-batch", parts)
	requireStatus(t, w, 201)
	br := decodeBatch(t, w)
	if br.Summary.Succeeded != 1001 || br.Summary.Failed != 0 {
		t.Fatalf("summary: %+v body=%s", br.Summary, w.Body.String())
	}
}

func TestBatchUploadHasNoTotalSizeCap(t *testing.T) {
	a := testApp(t)
	const size = int64(257 << 20)
	reader, writer := io.Pipe()
	mw := multipart.NewWriter(writer)
	go func() {
		part, err := mw.CreateFormFile("files", "large.bin")
		if err == nil {
			_, err = io.CopyN(part, zeroReader{}, size)
		}
		if closeErr := mw.Close(); err == nil {
			err = closeErr
		}
		_ = writer.CloseWithError(err)
	}()
	r := httptest.NewRequest("POST", "/api/file/upload-batch", reader)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	a.uploadBatchFile(w, r)
	requireStatus(t, w, 201)
	br := decodeBatch(t, w)
	if br.Summary.Succeeded != 1 || br.Summary.Failed != 0 {
		t.Fatalf("large upload summary: %+v body=%s", br.Summary, w.Body.String())
	}
	info, err := os.Stat(filepath.Join(a.workPath, "large.bin"))
	if err != nil || info.Size() != size {
		t.Fatalf("large upload size = %v, err=%v", info, err)
	}
}

func TestBatchUploadTruncatedNextPartIsReported(t *testing.T) {
	a := testApp(t)
	const boundary = "header-truncation-boundary"
	body := []byte("--" + boundary + "\r\n" +
		"Content-Disposition: form-data; name=\"files\"; filename=\"first.txt\"\r\n" +
		"Content-Type: application/octet-stream\r\n\r\nfirst\r\n" +
		"--" + boundary + "\r\n" +
		"Content-Disposition: form-data; name=\"files\"; filename=\"second.txt\"\r\n" +
		"Content-Type: application/octet-stream\r\n\r\nsecond\r\n" +
		"--" + boundary + "--\r\n")
	closing := bytes.LastIndex(body, []byte("--"+boundary+"--"))
	if closing < 0 {
		t.Fatal("closing boundary not found")
	}
	// Omit the closing boundary after the second file's content. The first
	// complete file remains committed while the truncated sibling is reported.
	body = body[:closing]
	r := httptest.NewRequest("POST", "/api/file/upload-batch", bytes.NewReader(body))
	r.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
	w := httptest.NewRecorder()
	a.uploadBatchFile(w, r)
	if w.Code != 207 {
		t.Fatalf("status=%d, want 207 partial success: %s", w.Code, w.Body.String())
	}
	result := decodeBatch(t, w)
	if result.Summary.Succeeded != 1 || result.Summary.Failed != 1 {
		t.Fatalf("summary=%+v, want one success and one parse failure", result.Summary)
	}
	if got := readBatchFile(t, a, "first.txt"); got != "first" {
		t.Fatalf("first part was not committed: %q", got)
	}
}

func TestBatchUploadSourceRules(t *testing.T) {
	a := testApp(t)
	refs := filepath.Join(a.workPath, "refs")
	if err := os.MkdirAll(refs, 0755); err != nil {
		t.Fatal(err)
	}
	w := batchUploadParts(a, "/api/file/upload-batch?source=nope", []batchPart{{"x.txt", "x"}})
	requireStatus(t, w, 400)
	requireStatus(t, request(a, "PUT", "/api/sources", map[string]any{
		"sources": []any{sourceBody("rw", "RW", "local", map[string]any{"path": "refs"}, true)},
	}), 200)
	w = batchUploadParts(a, "/api/file/upload-batch?source=rw&path=inbox",
		[]batchPart{{"a.txt", "src-a"}, {"sub/b.md", "src-b"}})
	requireStatus(t, w, 201)
	if b, err := os.ReadFile(filepath.Join(refs, "inbox", "a.txt")); err != nil || string(b) != "src-a" {
		t.Fatalf("source flat: %q %v", b, err)
	}
	if b, err := os.ReadFile(filepath.Join(refs, "inbox", "sub", "b.md")); err != nil || string(b) != "src-b" {
		t.Fatalf("source nested: %q %v", b, err)
	}
	requireStatus(t, request(a, "PUT", "/api/sources", map[string]any{
		"sources": []any{sourceBody("rw", "RW", "local", map[string]any{"path": "refs"}, false)},
	}), 200)
	w = batchUploadParts(a, "/api/file/upload-batch?source=rw", []batchPart{{"blocked.txt", "x"}})
	requireStatus(t, w, 403)
}

func TestBatchUploadEmpty(t *testing.T) {
	a := testApp(t)
	w := batchUploadParts(a, "/api/file/upload-batch", nil)
	requireStatus(t, w, 400)
}
