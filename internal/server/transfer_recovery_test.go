package server

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLargeTransferNonFileReferencesRemainIsolated(t *testing.T) {
	a := testApp(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("remote reference")) }))
	defer server.Close()
	requireStatus(t, request(a, "PUT", "/api/sources", map[string]any{"sources": []any{sourceBody("web", "Website", "link", map[string]any{"url": server.URL}, false)}}), 200)
	_ = a.reference.WriteFile("resource.txt", []byte("local must not leak"), 0600)
	if _, e := a.streamTransferFS(context.Background(), "web", false); e == nil {
		t.Fatal("non-file driver mapped to local reference root")
	}
	w := request(a, "GET", "/api/file/download?source=web&path=resource.txt", nil)
	requireStatus(t, w, 200)
	if w.Body.String() != "remote reference" {
		t.Fatal(w.Body.String())
	}
	requireStatus(t, request(a, "POST", "/api/file/transfer", fileTransferRequest{Operation: "copy", Source: "web", Destination: "workspace", Paths: []string{"resource.txt"}}), 200)
	b, e := a.workspace.ReadFile("resource.txt")
	if e != nil || string(b) != "remote reference" {
		t.Fatalf("legacy reference copy: %s %v", b, e)
	}
}

func TestLargeTransferSparse64BitRange(t *testing.T) {
	a := testApp(t)
	f, e := a.workspace.OpenFile("five-gib.bin", os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		t.Fatal(e)
	}
	const offset = int64(5) << 30
	const tail = "large offset"
	if e = f.Truncate(offset + int64(len(tail))); e != nil {
		t.Fatal(e)
	}
	if _, e = f.WriteAt([]byte(tail), offset); e != nil {
		t.Fatal(e)
	}
	f.Close()
	r := httptest.NewRequest("GET", "/api/file/stream?root=workspace&path=five-gib.bin", nil)
	r.Header.Set("Authorization", "Bearer "+a.token)
	r.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	requireStatus(t, w, 206)
	if w.Body.String() != tail {
		t.Fatal("64-bit range offset truncated")
	}
	if w.Header().Get("X-Transfer-Size") != fmt.Sprint(offset+int64(len(tail))) {
		t.Fatal("64-bit size truncated")
	}
}

func TestLargeTransferShutdownRejectsLateChunks(t *testing.T) {
	a := testApp(t)
	m := a.transfers()
	j, e := m.create(context.Background(), transferCreate{Operation: "upload", Name: "late.bin", Size: 17})
	if e != nil {
		t.Fatal(e)
	}
	m.close()
	requireStatus(t, transferTestPut(a, j.ID, 0, transferTestChunk(0, 17)), 409)
	if len(j.UploadChunks) != 0 {
		t.Fatal("closed manager accepted a late chunk")
	}
	testTransferRestart(a)
	m = a.transfers()
	j = m.jobs[j.ID]
	requireStatus(t, request(a, "POST", "/api/file/transfers/"+j.ID+"/resume", map[string]any{}), 200)
	requireStatus(t, transferTestPut(a, j.ID, 0, transferTestChunk(0, 17)), 200)
	if e = m.start(j); e != nil {
		t.Fatal(e)
	}
	testTransferWait(t, m, j)
}

func TestLargeTransferArchiveStreamsAndRanges(t *testing.T) {
	a := testApp(t)
	if e := os.Mkdir(filepath.Join(a.workPath, "archive"), 0755); e != nil {
		t.Fatal(e)
	}
	f, e := os.Create(filepath.Join(a.workPath, "archive/big.bin"))
	if e != nil {
		t.Fatal(e)
	}
	_ = f.Truncate(66<<20 + 31)
	_, _ = f.WriteAt([]byte("archive-content"), 65<<20)
	f.Close()
	_ = os.WriteFile(filepath.Join(a.workPath, "archive/small.txt"), []byte("small"), 0600)
	_ = os.Mkdir(filepath.Join(a.workPath, "archive/.git"), 0755)
	_ = os.WriteFile(filepath.Join(a.workPath, "archive/.git/private"), []byte("hidden"), 0600)
	m := a.transfers()
	j, e := m.create(context.Background(), transferCreate{Operation: "download", Paths: []string{"archive"}})
	if e != nil {
		t.Fatal(e)
	}
	if e = m.start(j); e != nil {
		t.Fatal(e)
	}
	testTransferWait(t, m, j)
	archive, e := zip.OpenReader(filepath.Join(m.jobDir(j), "download.zip"))
	if e != nil {
		t.Fatal(e)
	}
	defer archive.Close()
	found := false
	for _, entry := range archive.File {
		if entry.Name == "archive/.git/private" {
			t.Fatal("protected entry leaked")
		}
		if entry.Name == "archive/big.bin" {
			if entry.UncompressedSize64 != 66<<20+31 {
				t.Fatal(entry.UncompressedSize64)
			}
			r, e := entry.Open()
			if e != nil {
				t.Fatal(e)
			}
			if _, e = io.Copy(io.Discard, r); e != nil {
				t.Fatal("ZIP CRC verification", e)
			}
			r.Close()
			found = true
		}
	}
	if !found {
		t.Fatal("large ZIP entry absent")
	}
	request := httptest.NewRequest("GET", "/api/file/stream?job="+j.ID, nil)
	request.Header.Set("Authorization", "Bearer "+a.token)
	request.Header.Set("Range", "bytes=0-3")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, request)
	requireStatus(t, w, 206)
	if w.Body.String() != "PK\x03\x04" {
		t.Fatal("archive range bytes")
	}
}

func TestIssue64ArchivePreservesDistinctSelectedPaths(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
		paths []string
		want  map[string]string
	}{
		{
			name:  "same file basenames",
			files: map[string]string{"alpha/config": "alpha", "beta/config": "beta"},
			paths: []string{"alpha/config", "beta/config"},
			want:  map[string]string{"alpha/config": "alpha", "beta/config": "beta"},
		},
		{
			name:  "same directory basenames",
			files: map[string]string{"alpha/logs/device.txt": "alpha", "beta/logs/device.txt": "beta"},
			paths: []string{"alpha/logs", "beta/logs"},
			want:  map[string]string{"alpha/logs/device.txt": "alpha", "beta/logs/device.txt": "beta"},
		},
		{
			name:  "file and directory basename",
			files: map[string]string{"alpha/device": "file", "beta/device/log": "directory"},
			paths: []string{"alpha/device", "beta/device"},
			want:  map[string]string{"alpha/device": "file", "beta/device/log": "directory"},
		},
		{
			name:  "preserve all parents when one pair collides",
			files: map[string]string{"a/same": "a", "b/same": "b", "elsewhere/a": "third"},
			paths: []string{"elsewhere/a", "b/same", "a/same"},
			want:  map[string]string{"a/same": "a", "b/same": "b", "elsewhere/a": "third"},
		},
		{
			name:  "distinct basenames retain existing flat layout",
			files: map[string]string{"alpha/a.txt": "a", "beta/b.txt": "b"},
			paths: []string{"alpha/a.txt", "beta/b.txt"},
			want:  map[string]string{"a.txt": "a", "b.txt": "b"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := testApp(t)
			for name, content := range tc.files {
				file := filepath.Join(a.workPath, name)
				if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file, []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			m := a.transfers()
			j, err := m.create(context.Background(), transferCreate{Operation: "download", Paths: tc.paths})
			if err != nil {
				t.Fatal(err)
			}
			if err = m.start(j); err != nil {
				t.Fatal(err)
			}
			testTransferWait(t, m, j)
			archive, err := zip.OpenReader(filepath.Join(m.jobDir(j), "download.zip"))
			if err != nil {
				t.Fatal(err)
			}
			defer archive.Close()
			seen := map[string]bool{}
			files := 0
			for _, entry := range archive.File {
				if seen[entry.Name] {
					t.Fatalf("ambiguous duplicate ZIP entry: %q", entry.Name)
				}
				seen[entry.Name] = true
				if entry.FileInfo().IsDir() {
					continue
				}
				files++
				r, err := entry.Open()
				if err != nil {
					t.Fatal(err)
				}
				content, err := io.ReadAll(r)
				r.Close()
				if want, ok := tc.want[entry.Name]; err != nil || !ok || string(content) != want {
					t.Fatalf("ZIP entry %q = %q, error=%v; expected paths %v", entry.Name, content, err, tc.want)
				}
			}
			if files != len(tc.want) {
				t.Fatalf("archive has %d files, want %d", files, len(tc.want))
			}
		})
	}
}

func TestLargeTransferPauseQueueAndContextBoundary(t *testing.T) {
	a := testApp(t)
	_ = os.WriteFile(filepath.Join(a.workPath, "source.bin"), []byte("source"), 0600)
	_ = os.Mkdir(filepath.Join(a.workPath, "target"), 0755)
	m := a.transfers()
	j, e := m.create(context.Background(), transferCreate{Operation: "copy", Paths: []string{"source.bin"}, DestinationPath: "target"})
	if e != nil {
		t.Fatal(e)
	}
	m.runners <- struct{}{}
	m.runners <- struct{}{}
	if e = m.start(j); e != nil {
		t.Fatal(e)
	}
	requireStatus(t, request(a, "POST", "/api/file/transfers/"+j.ID+"/pause", map[string]any{}), 200)
	deadline := time.Now().Add(5 * time.Second)
	for {
		j.mu.Lock()
		paused := j.Status == "paused"
		j.mu.Unlock()
		if paused {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("queued task did not pause")
		}
		time.Sleep(time.Millisecond)
	}
	<-m.runners
	<-m.runners
	previous := a.wsConfig.Workspace.Path
	a.wsConfig.Workspace.Path = "changed-configuration"
	requireStatus(t, request(a, "POST", "/api/file/transfers/"+j.ID+"/resume", map[string]any{}), 409)
	a.wsConfig.Workspace.Path = previous
	requireStatus(t, request(a, "POST", "/api/file/transfers/"+j.ID+"/resume", map[string]any{"concurrency": 2}), 200)
	testTransferWait(t, m, j)
	if j.Concurrency != 2 {
		t.Fatal("concurrency selection not applied")
	}
}
func TestLargeTransferSourceChangesPreserveOriginal(t *testing.T) {
	a := testApp(t)
	source := filepath.Join(a.workPath, "source.bin")
	f, e := os.Create(source)
	if e != nil {
		t.Fatal(e)
	}
	_ = f.Truncate(transferChunkSize * 2)
	f.Close()
	_ = os.Mkdir(filepath.Join(a.workPath, "target"), 0755)
	m := a.transfers()
	j, e := m.create(context.Background(), transferCreate{Operation: "move", Paths: []string{"source.bin"}, DestinationPath: "target"})
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
	_ = stage.Truncate(j.Size)
	stage.Close()
	if e = m.copyChunk(context.Background(), j, src, dst, j.Parts[0], 0); e != nil {
		t.Fatal(e)
	}
	src.Close()
	dst.Close()
	info, _ := os.Stat(source)
	f, _ = os.OpenFile(source, os.O_RDWR, 0600)
	_, _ = f.WriteAt([]byte{7}, 0)
	f.Close()
	_ = os.Chtimes(source, info.ModTime(), info.ModTime()) // Changed bytes despite preserved length/mtime.
	if e = m.start(j); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if e = m.wait(ctx, j); e == nil {
		t.Fatal("changed acknowledged source accepted")
	}
	if _, e = os.Stat(source); e != nil {
		t.Fatal("changed original deleted")
	}
	if _, e = os.Stat(filepath.Join(a.workPath, "target/source.bin")); !os.IsNotExist(e) {
		t.Fatal("inconsistent copy published")
	}
}
