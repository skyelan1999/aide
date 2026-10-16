package server

import (
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStepLargeStreamingAndRange(t *testing.T) {
	a := testApp(t)
	file, err := os.Create(filepath.Join(a.workPath, "large.STP"))
	if err != nil {
		t.Fatal(err)
	}
	if err = file.Truncate(205 << 20); err != nil {
		t.Fatal(err)
	}
	if _, err = file.WriteAt([]byte("ISO-10303-21;"), 0); err != nil {
		t.Fatal(err)
	}
	file.Close()
	req := httptest.NewRequest("GET", "/api/file/raw?root=workspace&path=large.STP", nil)
	req.Header.Set("Range", "bytes=0-12")
	w := httptest.NewRecorder()
	a.readFileRaw(w, req)
	requireStatus(t, w, 206)
	if w.Body.String() != "ISO-10303-21;" || w.Header().Get("Content-Type") != "application/step" {
		t.Fatalf("range mismatch: %s", w.Body.String())
	}
	if !strings.HasPrefix(w.Header().Get("Content-Range"), "bytes 0-12/") {
		t.Fatal(w.Header())
	}
	// The generic byte reader and other viewers keep their existing small cap.
	if _, err := readRawBytes(a.workspace, "large.STP"); err == nil {
		t.Fatal("generic read unexpectedly unbounded")
	}
	if rawViewerLimit("image.png") != maxRawFile {
		t.Fatal("unrelated cap changed")
	}
}

func TestStepStreamingSourceAndIdentity(t *testing.T) {
	a := testApp(t)
	if err := os.WriteFile(filepath.Join(a.reference.Name(), "model.step"), []byte("STEP-source"), 0600); err != nil {
		t.Fatal(err)
	}
	a.sourceRegistry.Sources = append(a.sourceRegistry.Sources, Source{ID: "cad-source", Type: "local", Enabled: true, RW: false})
	w := request(a, "GET", "/api/file/raw?source=cad-source&path=model.step", nil)
	requireStatus(t, w, 200)
	if w.Body.String() != "STEP-source" {
		t.Fatal(w.Body.String())
	}
	w = request(a, "GET", "/api/file/raw?source=cad-source&path=model.step&knowledgeOrigin=stale&knowledgeWorkspace=stale", nil)
	requireStatus(t, w, 400)
	a.sourceRegistry.Sources[len(a.sourceRegistry.Sources)-1].Enabled = false
	requireStatus(t, request(a, "GET", "/api/file/raw?source=cad-source&path=model.step", nil), 400)
}

func TestStepStreamingLimitsAndPathSafety(t *testing.T) {
	a := testApp(t)
	f, err := os.Create(filepath.Join(a.workPath, "over.step"))
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(maxStepPreview + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	requireStatus(t, request(a, "GET", "/api/file/raw?path=over.step", nil), 400)
	if err = os.Symlink("over.step", filepath.Join(a.workPath, "link.step")); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"link.step", "../escape.step", ".aide-data/private.step", "/etc/secret.step"} {
		requireStatus(t, request(a, "GET", "/api/file/raw?path="+url.QueryEscape(p), nil), 400)
	}
	requireStatus(t, request(a, "GET", "/api/file/raw?path=over.step&knowledgeOrigin=stale", nil), 400)
}

func TestStepRemoteConcurrencyBound(t *testing.T) {
	a := testApp(t)
	a.sourceRegistry.Sources = append(a.sourceRegistry.Sources, Source{ID: "cad-mcp", Type: "mcp", Enabled: true})
	stepRemotePreviewGate <- struct{}{}
	defer func() { <-stepRemotePreviewGate }()
	requireStatus(t, request(a, "GET", "/api/file/raw?source=cad-mcp&path=model.step", nil), 429)
}

func TestStepNamedPipeRejectedBeforeOpen(t *testing.T) {
	if _, err := exec.LookPath("mkfifo"); err != nil {
		t.Skip("mkfifo unavailable")
	}
	a := testApp(t)
	file := filepath.Join(a.workPath, "blocking.step")
	if err := exec.Command("mkfifo", file).Run(); err != nil {
		t.Fatal(err)
	}
	done := make(chan int, 1)
	go func() {
		w := httptest.NewRecorder()
		a.readFileRaw(w, httptest.NewRequest("GET", "/api/file/raw?path=blocking.step", nil))
		done <- w.Code
	}()
	select {
	case status := <-done:
		if status != 400 {
			t.Fatalf("status=%d", status)
		}
	case <-time.After(time.Second):
		// A regression must not strand an Open syscall after the test fails.
		if writer, err := os.OpenFile(file, os.O_WRONLY, 0600); err == nil {
			writer.Close()
		}
		t.Fatal("named pipe blocked the STEP reader")
	}
}
