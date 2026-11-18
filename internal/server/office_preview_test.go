package server

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOfficePreviewStreamsSnapshotAndRechecksSource(t *testing.T) {
	a := testApp(t)
	old := officePreviewConvert
	defer func() { officePreviewConvert = old }()
	calls := 0
	officePreviewConvert = func(ctx context.Context, input, dir string) ([]byte, error) {
		calls++
		file, err := os.Open(input)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || info.Size() <= 64<<20 {
			t.Fatalf("large input was not streamed to disk: %v %v", info, err)
		}
		f, err := os.Create(filepath.Join(dir, "input.pdf"))
		if err != nil {
			return nil, err
		}
		defer f.Close()
		if _, err = f.WriteString("%PDF-1.4\nfixture"); err != nil {
			return nil, err
		}
		if err = f.Truncate(64 << 20); err != nil {
			return nil, err
		}
		return []byte("{\"pages\":1,\"sheets\":[]}"), nil
	}
	dir := a.reference.Name()
	name := filepath.Join(dir, "book.xlsx")
	file, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.Seek(65<<20, 0); err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	writer.SetOffset(65 << 20)
	part, err := writer.Create("xl/workbook.xml")
	if err != nil {
		t.Fatal(err)
	}
	part.Write([]byte("<workbook/>"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	file.Close()
	a.sourceRegistry.Sources = append(a.sourceRegistry.Sources, Source{ID: "office-preview-ref", Name: "Preview", Type: "local", Enabled: true})
	base := "/api/office/preview?path=book.xlsx&source=office-preview-ref"
	meta := request(a, "GET", base+"&format=meta", nil)
	requireStatus(t, meta, 200)
	version := meta.Header().Get("X-Aide-Office-Version")
	if version == "" {
		t.Fatal("missing preview snapshot")
	}
	url := base + "&format=pdf&version=" + version
	r := httptest.NewRequest("GET", url, nil)
	r.Header.Set("Authorization", "Bearer "+a.token)
	r.Header.Set("Range", "bytes=0-4")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusPartialContent || w.Body.String() != "%PDF-" || w.Header().Get("Content-Range") != "bytes 0-4/67108864" || calls != 1 {
		t.Fatal("PDF range reused the disk snapshot incorrectly", w.Code, w.Body.String(), calls)
	}
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("GET", url, nil))
	requireStatus(t, w, 401)
	requireStatus(t, request(a, "GET", base+"&format=pdf&version=unknown", nil), 409)
	requireStatus(t, request(a, "GET", strings.Replace(url, "book.xlsx", "other.xlsx", 1), nil), 409)
	a.mu.Lock()
	a.sourceRegistry.Sources[len(a.sourceRegistry.Sources)-1].Enabled = false
	a.mu.Unlock()
	requireStatus(t, request(a, "GET", url, nil), 409)
	info, err := os.Stat(name)
	if err != nil || info.Size() <= 64<<20 {
		t.Fatal("original input missing or changed", err)
	}
}

func TestOfficePreviewLimitsAndCancellation(t *testing.T) {
	a := testApp(t)
	for _, q := range []string{"path=../book.xlsx&format=pdf", "path=book.rtf&format=pdf", "path=book.xlsx&format=other", "path=book.xlsx&format=page&page=0&size=1024", "path=book.xlsx&format=page&page=1&size=4097"} {
		requireStatus(t, request(a, "GET", "/api/office/preview?"+q, nil), 400)
	}
	dwgWrite(t, a, "bad.ppt", []byte("not a compound file"))
	requireStatus(t, request(a, "GET", "/api/office/preview?path=bad.ppt&format=pdf", nil), 400)
	os.Symlink("bad.ppt", filepath.Join(a.workPath, "link.ppt"))
	requireStatus(t, request(a, "GET", "/api/office/preview?path=link.ppt&format=pdf", nil), 400)
	officePreviewSlots <- struct{}{}
	officePreviewSlots <- struct{}{}
	w := request(a, "GET", "/api/office/preview?path=bad.ppt&format=pdf", nil)
	<-officePreviewSlots
	<-officePreviewSlots
	requireStatus(t, w, 429)

	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "soffice"), []byte("#!/bin/sh\nsleep 30\n"), 0700)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := convertOfficePreview(ctx, filepath.Join(dir, "input.xlsx"), dir)
	if err == nil || time.Since(start) > 4*time.Second {
		t.Fatal("converter not cancelled", err)
	}
}

func TestOfficePreviewRealRenderer(t *testing.T) {
	if os.Getenv("AIDE_OFFICE_PREVIEW_REAL") != "1" {
		t.Skip("requires candidate Office renderer")
	}
	for _, kind := range []string{"xlsx", "pptx", "docx"} {
		spec := map[string]any{"slides": []any{map[string]any{"title": "Preview 预览", "body": "Line one\n第二行"}}}
		switch kind {
		case "xlsx":
			spec = map[string]any{"sheets": []any{map[string]any{"name": "数据", "rows": []any{[]any{"标题", "Line one\n第二行"}, []any{"数值", 42}}}}}
		case "docx":
			spec = map[string]any{"blocks": []any{map[string]any{"type": "paragraph", "text": "Preview 预览\n第二行"}}}
		}
		input, err := officeCreateBytes(kind, spec)
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		name := filepath.Join(dir, "input."+kind)
		if err := os.WriteFile(name, input, 0600); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
		meta, err := convertOfficePreview(ctx, name, dir)
		cancel()
		if err != nil || !bytes.Contains(meta, []byte("\"pages\": 1")) {
			t.Fatal(kind, err, string(meta))
		}
	}
}
