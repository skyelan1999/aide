package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func pdfFakeRaster(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	img := image.NewRGBA(image.Rect(0, 0, 10, 20))
	img.Set(2, 2, color.RGBA{0, 0, 0, 255})
	var b bytes.Buffer
	png.Encode(&b, img)
	script := "#!/usr/bin/python3\nimport pathlib,sys,base64,time\nout=pathlib.Path(sys.argv[-1]+'.png')\n" + body + "\nout.write_bytes(base64.b64decode('" + base64.StdEncoding.EncodeToString(b.Bytes()) + "'))\n"
	if err := os.WriteFile(filepath.Join(dir, "pdftoppm"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
}

func TestPDFPreviewAuthorizationBoundsAndReadonly(t *testing.T) {
	a := testApp(t)
	pdfFakeRaster(t, "")
	input := []byte("%PDF-1.4\noriginal")
	dwgWrite(t, a, "page.pdf", input)
	u := "/api/file/pdf-preview?path=page.pdf&page=1&size=1024"
	w := request(a, "GET", u, nil)
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, err := png.Decode(bytes.NewReader(w.Body.Bytes())); err != nil {
		t.Fatal(err)
	}
	b, _ := a.workspace.ReadFile("page.pdf")
	if !bytes.Equal(b, input) {
		t.Fatal("original modified")
	}
	for _, query := range []string{"path=../page.pdf&page=1&size=1024", "path=page.dwg&page=1&size=1024", "path=page.pdf&page=0&size=1024", "path=page.pdf&page=2001&size=1024", "path=page.pdf&page=1&size=4097"} {
		if w := request(a, "GET", "/api/file/pdf-preview?"+query, nil); w.Code != 400 {
			t.Fatal(query, w.Code)
		}
	}
	if err := os.Symlink("page.pdf", filepath.Join(a.workPath, "link.pdf")); err != nil {
		t.Fatal(err)
	}
	if w := request(a, "GET", "/api/file/pdf-preview?path=link.pdf&page=1&size=1024", nil); w.Code != 400 {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("GET", u, nil))
	if w.Code != 401 {
		t.Fatal("unauthorized raster", w.Code)
	}
	dwgPreviewSlots <- struct{}{}
	dwgPreviewSlots <- struct{}{}
	w = request(a, "GET", u, nil)
	<-dwgPreviewSlots
	<-dwgPreviewSlots
	if w.Code != 429 {
		t.Fatal(w.Code)
	}
}

func TestPDFPreviewCancellationAndMalformedOutput(t *testing.T) {
	input := []byte("%PDF-1.4\noriginal")
	pdfFakeRaster(t, "time.sleep(30)")
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := rasterPDFPage(ctx, input, 1, 1024); err == nil || time.Since(start) > 3*time.Second {
		t.Fatal("uncancelled raster", err)
	}
	pdfFakeRaster(t, "out.write_bytes(b'not PNG');sys.exit(0)")
	if _, err := rasterPDFPage(context.Background(), input, 1, 1024); err == nil {
		t.Fatal("malformed PNG accepted")
	}
	pdfFakeRaster(t, "out.write_bytes(b'x'*(16*1024*1024));sys.exit(0)")
	if _, err := rasterPDFPage(context.Background(), input, 1, 1024); err == nil {
		t.Fatal("unbounded PNG accepted")
	}
}

func TestPDFPreviewSourceRevokedDuringRaster(t *testing.T) {
	a := testApp(t)
	marker := filepath.Join(t.TempDir(), "started")
	pdfFakeRaster(t, "pathlib.Path("+fmtPythonString(marker)+").write_text('started');time.sleep(.2)")
	dwgWrite(t, a, "page.pdf", []byte("%PDF-1.4\noriginal"))
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() { result <- request(a, "GET", "/api/file/pdf-preview?path=page.pdf&page=1&size=1024", nil) }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("raster not started")
		}
		time.Sleep(time.Millisecond)
	}
	a.mu.Lock()
	a.wsRevision++
	a.mu.Unlock()
	if w := <-result; w.Code != 409 {
		t.Fatal("stale raster accepted", w.Code)
	}
}
