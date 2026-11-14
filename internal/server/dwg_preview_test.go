package server

import (
	"bytes"
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

const dwgTestDXF = "0\nSECTION\n2\nENTITIES\n0\nLINE\n8\n0\n10\n0\n20\n0\n11\n10\n21\n20\n0\nENDSEC\n0\nEOF\n"

func dwgTestInput() []byte { return append([]byte("AC1015"), make([]byte, 100)...) }
func dwgFakeConverter(t *testing.T, body string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "dwg2dxf")
	if err := os.WriteFile(p, []byte("#!/usr/bin/python3\nimport sys, pathlib, time, os\nout=pathlib.Path(sys.argv[sys.argv.index('-o')+1])\n"+body), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AIDE_DWG_CONVERTER", p)
}
func dwgWrite(t *testing.T, a *App, name string, data []byte) {
	t.Helper()
	if err := a.workspace.WriteFile(name, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestDWGPreviewReadonlyAndLimits(t *testing.T) {
	a := testApp(t)
	dwgFakeConverter(t, "out.write_text("+fmtPythonString(dwgTestDXF)+")\n")
	dwgWrite(t, a, "drawing.dwg", dwgTestInput())
	w := request(a, "GET", "/api/file/dwg-preview?path=drawing.dwg", nil)
	if w.Code != 200 || w.Body.String() != dwgTestDXF || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
	b, _ := a.workspace.ReadFile("drawing.dwg")
	if !bytes.Equal(b, dwgTestInput()) {
		t.Fatal("preview changed original")
	}
	for _, p := range []string{"../drawing.dwg", "drawing.dxf", "/drawing.dwg"} {
		if w := request(a, "GET", "/api/file/dwg-preview?path="+p, nil); w.Code != 400 {
			t.Fatal(p, w.Code)
		}
	}
	dwgWrite(t, a, "broken.dwg", []byte("not a drawing"))
	if w := request(a, "GET", "/api/file/dwg-preview?path=broken.dwg", nil); w.Code != 422 {
		t.Fatal(w.Code)
	}
	f, err := a.workspace.OpenFile("large.dwg", os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	f.Truncate(dwgInputLimit + 1)
	f.Close()
	if w := request(a, "GET", "/api/file/dwg-preview?path=large.dwg", nil); w.Code != 413 {
		t.Fatal(w.Code)
	}
}

func fmtPythonString(s string) string {
	return "\"" + strings.ReplaceAll(strings.ReplaceAll(s, "\\", "\\\\"), "\n", "\\n") + "\""
}

func TestDWGPreviewRejectsSpecialAndStaleSources(t *testing.T) {
	a := testApp(t)
	dwgFakeConverter(t, "time.sleep(0.2)\nout.write_text("+fmtPythonString(dwgTestDXF)+")\n")
	dwgWrite(t, a, "drawing.dwg", dwgTestInput())
	if err := os.Symlink("drawing.dwg", filepath.Join(a.workPath, "link.dwg")); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(a.workPath, "fifo.dwg"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"link.dwg", "fifo.dwg"} {
		if w := request(a, "GET", "/api/file/dwg-preview?path="+p, nil); w.Code != 400 {
			t.Fatal(p, w.Code)
		}
	}
	// Change identity while the subprocess is running, then reject its result.
	marker := filepath.Join(t.TempDir(), "started")
	dwgFakeConverter(t, "pathlib.Path("+fmtPythonString(marker)+").write_text('started')\ntime.sleep(0.2)\nout.write_text("+fmtPythonString(dwgTestDXF)+")\n")
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() { result <- request(a, "GET", "/api/file/dwg-preview?path=drawing.dwg", nil) }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("converter did not start")
		}
		time.Sleep(time.Millisecond)
	}
	a.mu.Lock()
	a.wsRevision++
	a.mu.Unlock()
	if w := <-result; w.Code != 409 {
		t.Fatal("stale preview accepted", w.Code, w.Body.String())
	}

}

func TestDWGPreviewConverterFailureCancelAndOutputBounds(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"failed", "sys.exit(1)\n"},
		{"incomplete", "out.write_text('0\\nSECTION\\n')\n"},
		{"oversized", "out.write_bytes(b'x'*(4*1024*1024+1))\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dwgFakeConverter(t, tc.body)
			if _, err := convertDWG(context.Background(), dwgTestInput()); err == nil {
				t.Fatal("invalid result accepted")
			}
		})
	}
	t.Setenv("AIDE_DWG_CONVERTER", filepath.Join(t.TempDir(), "absent"))
	if _, err := convertDWG(context.Background(), dwgTestInput()); err == nil {
		t.Fatal("missing component accepted")
	}
	dwgFakeConverter(t, "time.sleep(30)\n")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := convertDWG(ctx, dwgTestInput()); err == nil || time.Since(start) > 3*time.Second {
		t.Fatal("cancel did not stop converter", err)
	}
}

func TestDWGPreviewBusyAndAuthentication(t *testing.T) {
	a := testApp(t)
	dwgPreviewSlots <- struct{}{}
	dwgPreviewSlots <- struct{}{}
	w := request(a, "GET", "/api/file/dwg-preview?path=drawing.dwg", nil)
	<-dwgPreviewSlots
	<-dwgPreviewSlots
	if w.Code != 429 {
		t.Fatal(w.Code)
	}
	r := httptest.NewRequest("GET", "/api/file/dwg-preview?path=drawing.dwg", nil)
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("unauthenticated converter access", w.Code)
	}
}

// Run in the standard image where the real pinned CLI is installed. A fixture
// path is explicit so portable unit tests do not pretend to validate DWG parsing.
func TestDWGPreviewRealConverter(t *testing.T) {
	fixture := os.Getenv("AIDE_DWG_TEST_FILE")
	if fixture == "" {
		t.Skip("set AIDE_DWG_TEST_FILE for real LibreDWG integration")
	}
	b, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	result, err := convertDWG(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}
	entities := strings.Split(strings.ReplaceAll(string(result), "\r", ""), "\nENTITIES\n")
	if len(entities) != 2 {
		t.Fatal("missing ENTITIES section")
	}
	geometry := strings.Split(entities[1], "ENDSEC")[0]
	if !strings.Contains(geometry, "\nARC\n") && !strings.Contains(geometry, "\nLINE\n") && !strings.Contains(geometry, "\nCIRCLE\n") {
		t.Fatal("real drawing lost geometry")
	}
}

func TestDWGPreviewReadonlyReferenceAndRevocation(t *testing.T) {
	a := testApp(t)
	dwgFakeConverter(t, "out.write_text("+fmtPythonString(dwgTestDXF)+")\n")
	dir := a.reference.Name()
	os.WriteFile(filepath.Join(dir, "reference.dwg"), dwgTestInput(), 0400)
	a.mu.Lock()
	a.sourceRegistry.Sources = append(a.sourceRegistry.Sources, Source{ID: "drawing-ref", Name: "readonly drawing", Type: "local", Enabled: true, Config: SourceConfig{Path: "/context"}})
	a.mu.Unlock()
	w := request(a, "GET", "/api/file/dwg-preview?source=drawing-ref&path=reference.dwg", nil)
	if w.Code != 200 {
		t.Fatal("read-only source not previewable", w.Code, w.Body.String())
	}
	a.mu.Lock()
	for i := range a.sourceRegistry.Sources {
		if a.sourceRegistry.Sources[i].ID == "drawing-ref" {
			a.sourceRegistry.Sources[i].Enabled = false
		}
	}
	a.mu.Unlock()
	if w = request(a, "GET", "/api/file/dwg-preview?source=drawing-ref&path=reference.dwg", nil); w.Code == 200 {
		t.Fatal("disabled reference accepted")
	}
	b, _ := os.ReadFile(filepath.Join(dir, "reference.dwg"))
	if !bytes.Equal(b, dwgTestInput()) {
		t.Fatal("reference modified")
	}
}
