package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

func pdfFakeFontTools(t *testing.T, list, match string) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{"pdffonts": list, "fc-match": match} {
		script := "#!/usr/bin/python3\nimport sys,time\n" + body + "\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
}

func TestPDFFontInspectionRequiresActualFamilyAndPreservesInput(t *testing.T) {
	if canonicalPDFFont("新宋体") != "nsimsun" || canonicalPDFFont("宋体") != "simsun" {
		t.Fatal("distinct font families conflated")
	}
	list := "Embedded CID TrueType Identity-H yes no yes 1 0\nSimSun TrueType WinAnsi no no no 2 0\nSimSun CID TrueType GBK-EUC-H no no no 3 0\n"
	pdfFakeFontTools(t, "print("+fmtPythonString(list)+")", "assert sys.argv[-1] == ':family=simsun'\nprint('SimSun,宋体')")
	a := testApp(t)
	input := []byte("%PDF-1.4\noriginal")
	dwgWrite(t, a, "font.pdf", input)
	u := "/api/file/pdf-preview?path=font.pdf&format=fonts"
	w := request(a, "GET", u, nil)
	var result pdfFontInspection
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	if !result.PreferOriginalFonts || result.EmbeddedFonts != 1 || len(result.MissingFonts) != 1 || !result.MissingFonts[0].Available {
		t.Fatal(result)
	}
	after, _ := a.workspace.ReadFile("font.pdf")
	if !bytes.Equal(input, after) {
		t.Fatal("font inspection modified original")
	}
	unauthorized := httptest.NewRecorder()
	a.Handler().ServeHTTP(unauthorized, httptest.NewRequest("GET", u, nil))
	if unauthorized.Code != 401 {
		t.Fatal(unauthorized.Code)
	}
	pdfFakeFontTools(t, "print("+fmtPythonString(list)+")", "print('WenQuanYi Zen Hei')")
	data, err := inspectPDFFonts(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if json.Unmarshal(data, &result) != nil || result.PreferOriginalFonts || result.MissingFonts[0].Available {
		t.Fatal("fallback family called original", string(data))
	}
}

func TestPDFFontInspectionBoundsAndCancellation(t *testing.T) {
	var list strings.Builder
	for n := 0; n < 129; n++ {
		fmt.Fprintf(&list, "Font%d TrueType WinAnsi no no no %d 0\n", n, n)
	}
	if _, err := parsePDFFontList([]byte(list.String())); err == nil {
		t.Fatal("font budget bypass")
	}
	pdfFakeFontTools(t, "time.sleep(30)", "print('SimSun')")
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := inspectPDFFonts(ctx, []byte("%PDF-1.4\noriginal")); err == nil || time.Since(start) > 3*time.Second {
		t.Fatal("font inspection not cancelled", err)
	}
}

func TestPDFFontConfigUsesEscapedPrivateDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "fonts&<local>")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AIDE_CAD_FONT_DIR", dir)
	tmp := t.TempDir()
	env, err := pdfFontEnvironment(tmp)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(tmp, "fonts.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "fonts&amp;&lt;local&gt;") || !strings.Contains(strings.Join(env, "\n"), "FONTCONFIG_FILE=") {
		t.Fatal("unescaped font directory", string(data))
	}
}

func TestPDFOriginalFontRuntime(t *testing.T) {
	path := os.Getenv("AIDE_PDF_FONT_TEST_FILE")
	if path == "" {
		t.Skip("set AIDE_PDF_FONT_TEST_FILE in the configured original-font runtime")
	}
	input, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := inspectPDFFonts(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	var result pdfFontInspection
	if json.Unmarshal(data, &result) != nil || !result.PreferOriginalFonts {
		t.Fatal("original font not selected", string(data))
	}
	if _, err = rasterPDFPage(context.Background(), input, 1, 1024); err != nil {
		t.Fatal(err)
	}
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
