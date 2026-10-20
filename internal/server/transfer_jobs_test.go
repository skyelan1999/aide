package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

func transferTestChunk(index int64, size int64) []byte {
	return bytes.Repeat([]byte{byte(index*17 + 3)}, int(size))
}
func transferTestPut(a *App, id string, index int64, b []byte) *httptest.ResponseRecorder {
	r := httptest.NewRequest("PUT", "/api/file/transfers/"+id+"/chunks/"+fmtTransferIndex(index), bytes.NewReader(b))
	r.Header.Set("Authorization", "Bearer "+a.token)
	r.Header.Set("X-Chunk-SHA256", hash(b))
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	return w
}
func fmtTransferIndex(n int64) string { return strconv.FormatInt(n, 10) }
func testTransferRestart(a *App) {
	a.transferMu.Lock()
	m := a.transferManager
	a.transferMu.Unlock()
	m.close()
	a.transferMu.Lock()
	a.transferManager = nil
	a.transferMu.Unlock()
}
func testTransferWait(t *testing.T, m *transferManager, j *transferJob) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if e := m.wait(ctx, j); e != nil {
		t.Fatal(e)
	}
}
func testFileDigest(t *testing.T, name string) string {
	t.Helper()
	f, e := os.Open(name)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		t.Fatal(e)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func TestLargeTransferUploadConcurrentJournalRestart(t *testing.T) {
	a := testApp(t)
	m := a.transfers()
	size := int64(65<<20) + 37
	j, e := m.create(context.Background(), transferCreate{Operation: "upload", Name: "large/upload.bin", Size: size})
	if e != nil {
		t.Fatal(e)
	}
	// Sparse, out-of-order acknowledgement; no server-local payload spool exists.
	for _, n := range []int64{0, 2, 16} {
		requireStatus(t, transferTestPut(a, j.ID, n, transferTestChunk(n, min(transferChunkSize, size-n*transferChunkSize))), 200)
	}
	requireStatus(t, transferTestPut(a, j.ID, 0, transferTestChunk(0, transferChunkSize)), 200)
	requireStatus(t, transferTestPut(a, j.ID, 0, bytes.Repeat([]byte{99}, int(transferChunkSize))), 409)
	f, e := os.OpenFile(filepath.Join(m.jobDir(j), "chunks.log"), os.O_WRONLY|os.O_APPEND, 0600)
	if e != nil {
		t.Fatal(e)
	}
	_, _ = f.WriteString(`{"p":`)
	f.Close()
	testTransferRestart(a)
	m = a.transfers()
	j = m.jobs[j.ID]
	if len(j.UploadChunks) != 3 {
		t.Fatalf("recovered %d chunks", len(j.UploadChunks))
	}
	if j.Status != "paused" {
		t.Fatal(j.Status)
	}
	requireStatus(t, request(a, "POST", "/api/file/transfers/"+j.ID+"/resume", map[string]any{}), 200)
	var wg sync.WaitGroup
	errors := make(chan int, 20)
	semaphore := make(chan struct{}, 4)
	for n := int64(0); n*transferChunkSize < size; n++ {
		if n == 0 || n == 2 || n == 16 {
			continue
		}
		wg.Add(1)
		go func(n int64) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			w := transferTestPut(a, j.ID, n, transferTestChunk(n, min(transferChunkSize, size-n*transferChunkSize)))
			if w.Code != 200 {
				errors <- w.Code
			}
		}(n)
	}
	wg.Wait()
	close(errors)
	for code := range errors {
		t.Fatalf("chunk status %d", code)
	}
	requireStatus(t, request(a, "POST", "/api/file/transfers/"+j.ID+"/commit", map[string]any{}), 200)
	testTransferWait(t, m, j)
	h := sha256.New()
	for n := int64(0); n*transferChunkSize < size; n++ {
		_, _ = h.Write(transferTestChunk(n, min(transferChunkSize, size-n*transferChunkSize)))
	}
	if got := testFileDigest(t, filepath.Join(a.workPath, "large/upload.bin")); got != hex.EncodeToString(h.Sum(nil)) {
		t.Fatal("large upload changed bytes")
	}
	entries, _ := os.ReadDir(m.jobDir(j))
	for _, entry := range entries {
		if entry.Name() != "job.json" && entry.Name() != "chunks.log" {
			t.Fatalf("unexpected payload spool %s", entry.Name())
		}
	}
	// Removing a completed record never removes the published user's file.
	requireStatus(t, request(a, "DELETE", "/api/file/transfers/"+j.ID, nil), 200)
	if _, e = os.Stat(filepath.Join(a.workPath, "large/upload.bin")); e != nil {
		t.Fatal(e)
	}
}

func TestLargeTransferUploadCorruptionRepairAndCancel(t *testing.T) {
	a := testApp(t)
	m := a.transfers()
	j, e := m.create(context.Background(), transferCreate{Operation: "upload", Name: "repair.bin", Size: transferChunkSize + 17})
	if e != nil {
		t.Fatal(e)
	}
	requireStatus(t, transferTestPut(a, j.ID, 0, transferTestChunk(0, transferChunkSize)), 200)
	requireStatus(t, transferTestPut(a, j.ID, 1, transferTestChunk(1, 17)), 200)
	stage := filepath.Join(a.workPath, j.Tops[0].Stage)
	f, e := os.OpenFile(stage, os.O_RDWR, 0600)
	if e != nil {
		t.Fatal(e)
	}
	_, _ = f.WriteAt([]byte{99}, 0)
	f.Close()
	if e = m.start(j); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if e = m.wait(ctx, j); e == nil {
		t.Fatal("corruption accepted")
	}
	j.mu.Lock()
	committed := j.UploadCommitted
	acked := len(j.UploadChunks)
	j.mu.Unlock()
	if committed || acked != 1 {
		t.Fatalf("repair state committed=%v chunks=%d", committed, acked)
	}
	testTransferRestart(a)
	m = a.transfers()
	j = m.jobs[j.ID]
	if len(j.UploadChunks) != 1 {
		t.Fatal("invalidation journal not recovered")
	}
	requireStatus(t, request(a, "POST", "/api/file/transfers/"+j.ID+"/resume", map[string]any{}), 200)
	requireStatus(t, transferTestPut(a, j.ID, 0, transferTestChunk(0, transferChunkSize)), 200)
	if e = m.start(j); e != nil {
		t.Fatal(e)
	}
	testTransferWait(t, m, j)
	other, e := m.create(context.Background(), transferCreate{Operation: "upload", Name: "cancelled.bin", Size: 17})
	if e != nil {
		t.Fatal(e)
	}
	requireStatus(t, request(a, "POST", "/api/file/transfers/"+other.ID+"/cancel", map[string]any{}), 200)
	if _, e = os.Stat(filepath.Join(a.workPath, other.Tops[0].Stage)); !os.IsNotExist(e) {
		t.Fatal("cancel retained payload")
	}
	requireStatus(t, request(a, "POST", "/api/file/transfers/"+other.ID+"/resume", map[string]any{}), 409)
}

func TestLargeTransferCopyResumeMoveAndConflict(t *testing.T) {
	a := testApp(t)
	m := a.transfers()
	if e := os.MkdirAll(filepath.Join(a.workPath, "destination"), 0755); e != nil {
		t.Fatal(e)
	}
	f, e := os.Create(filepath.Join(a.workPath, "source.bin"))
	if e != nil {
		t.Fatal(e)
	}
	_ = f.Truncate(70 << 20)
	_, _ = f.WriteAt([]byte("begin"), 0)
	_, _ = f.WriteAt([]byte("end"), (70<<20)-3)
	f.Close()
	expected := testFileDigest(t, filepath.Join(a.workPath, "source.bin"))
	j, e := m.create(context.Background(), transferCreate{Operation: "move", Source: "workspace", Paths: []string{"source.bin"}, Destination: "workspace", DestinationPath: "destination"})
	if e != nil {
		t.Fatal(e)
	}
	src, e := a.streamTransferFS(context.Background(), "workspace", false)
	if e != nil {
		t.Fatal(e)
	}
	dst, e := a.streamTransferFS(context.Background(), "workspace", true)
	if e != nil {
		t.Fatal(e)
	}
	stage, e := dst.Open(j.Parts[0].Target, os.O_CREATE|os.O_EXCL|os.O_RDWR)
	if e != nil {
		t.Fatal(e)
	}
	_ = stage.Truncate(j.Parts[0].Size)
	stage.Close()
	if e = m.copyChunk(context.Background(), j, src, dst, j.Parts[0], 0); e != nil {
		t.Fatal(e)
	}
	src.Close()
	dst.Close()
	testTransferRestart(a)
	m = a.transfers()
	j = m.jobs[j.ID]
	if len(j.Parts[0].Chunks) != 1 {
		t.Fatal("copy progress not recovered")
	}
	if e = m.start(j); e != nil {
		t.Fatal(e)
	}
	testTransferWait(t, m, j)
	if testFileDigest(t, filepath.Join(a.workPath, "destination/source.bin")) != expected {
		t.Fatal("move bytes mismatch")
	}
	if _, e = os.Stat(filepath.Join(a.workPath, "source.bin")); !os.IsNotExist(e) {
		t.Fatal("move retained original")
	}
	_ = os.WriteFile(filepath.Join(a.workPath, "empty.bin"), nil, 0600)
	conflict, e := m.create(context.Background(), transferCreate{Operation: "move", Paths: []string{"empty.bin"}, DestinationPath: "destination"})
	if e != nil {
		t.Fatal(e)
	}
	_ = os.WriteFile(filepath.Join(a.workPath, "destination/empty.bin"), nil, 0600)
	if e = m.start(conflict); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if e = m.wait(ctx, conflict); e == nil {
		t.Fatal("concurrent identical destination accepted")
	}
	if _, e = os.Stat(filepath.Join(a.workPath, "empty.bin")); e != nil {
		t.Fatal("conflict removed original")
	}
}

func TestLargeTransferDownloadRangesAndBoundaries(t *testing.T) {
	a := testApp(t)
	f, e := os.Create(filepath.Join(a.workPath, "range.bin"))
	if e != nil {
		t.Fatal(e)
	}
	_ = f.Truncate(80 << 20)
	_, _ = f.WriteAt([]byte("offset-data"), 70<<20)
	f.Close()
	r := httptest.NewRequest("GET", "/api/file/stream?root=workspace&path=range.bin", nil)
	r.Header.Set("Authorization", "Bearer "+a.token)
	r.Header.Set("Range", "bytes=73400320-73400330")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	requireStatus(t, w, 206)
	if w.Body.String() != "offset-data" {
		t.Fatal(w.Body.String())
	}
	tag := w.Header().Get("ETag")
	if tag == "" {
		t.Fatal("missing validator")
	}
	r = httptest.NewRequest("GET", "/api/file/stream?root=workspace&path=range.bin", nil)
	r.Header.Set("Authorization", "Bearer "+a.token)
	r.Header.Set("Range", "bytes=0-9")
	r.Header.Set("If-Match", `"stale"`)
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	requireStatus(t, w, 412)
	requireStatus(t, request(a, "POST", "/api/file/transfers", transferCreate{Operation: "copy", Source: "workspace", Paths: []string{"../range.bin"}, DestinationPath: "."}), 400)
	a.sourceRegistry.Sources = append(a.sourceRegistry.Sources, Source{ID: "readonly-transfer", Enabled: true, Type: "local", RW: false})
	requireStatus(t, request(a, "POST", "/api/file/transfers", transferCreate{Operation: "upload", Destination: "readonly-transfer", Name: "blocked", Size: 1}), 400)
	requireStatus(t, request(a, "POST", "/api/file/transfers", transferCreate{Operation: "upload", Name: "blocked", Size: 1, Workspace: "another-workspace"}), 400)
}
