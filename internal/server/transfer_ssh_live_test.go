package server

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

// The runner owns the server, credentials and master socket. The wrapper maps
// ALL control paths to that socket, so no product/global master is touched.
func TestLargeTransferSSHLive(t *testing.T) {
	root := os.Getenv("AIDE_TRANSFER_SSH_ROOT")
	if root == "" {
		t.Skip("run scripts/test-large-transfer-ssh.py")
	}
	port, e := strconv.Atoi(os.Getenv("AIDE_TRANSFER_SSH_PORT"))
	if e != nil {
		t.Fatal(e)
	}
	a := testApp(t)
	a.sshBin = filepath.Join(root, "ssh-wrapper")
	a.wsConfig.Workspace.Mode = "ssh"
	a.wsConfig.Workspace.Host = "127.0.0.1"
	a.wsConfig.Workspace.Username = "agent"
	a.wsConfig.Workspace.Port = port
	a.wsConfig.Workspace.Path = filepath.Join(root, "remote")
	t.Cleanup(func() { a.sshBin = "" })
	if e = os.Symlink(root, filepath.Join(root, "remote/escape")); e != nil {
		t.Fatal(e)
	}
	pathCheck, e := a.streamTransferFS(context.Background(), "workspace", false)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = pathCheck.Open("escape/host", os.O_RDONLY); e == nil {
		pathCheck.Close()
		t.Fatal("OpenSSH REALPATH fast path followed a symlink")
	}
	pathCheck.Close()
	a.sourceRegistry.Sources = append(a.sourceRegistry.Sources, Source{ID: "owned-local", Type: "local", Enabled: true, RW: true})
	m := a.transfers()
	size := int64(65<<20) + 37
	j, e := m.create(context.Background(), transferCreate{Operation: "upload", Name: "remote.bin", Size: size})
	if e != nil {
		t.Fatal(e)
	}
	requireStatus(t, transferTestPut(a, j.ID, 0, transferTestChunk(0, transferChunkSize)), 200)
	testTransferRestart(a)
	m = a.transfers()
	j = m.jobs[j.ID]
	requireStatus(t, request(a, "POST", "/api/file/transfers/"+j.ID+"/resume", map[string]any{}), 200)
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 4)
	errors := make(chan string, 20)
	for n := int64(1); n*transferChunkSize < size; n++ {
		wg.Add(1)
		go func(n int64) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			w := transferTestPut(a, j.ID, n, transferTestChunk(n, min(transferChunkSize, size-n*transferChunkSize)))
			if w.Code != 200 {
				errors <- w.Body.String()
			}
		}(n)
	}
	wg.Wait()
	close(errors)
	for e := range errors {
		t.Fatal(e)
	}
	if e = m.start(j); e != nil {
		t.Fatal(e)
	}
	testTransferWait(t, m, j)
	remote := filepath.Join(root, "remote/remote.bin")
	sshToSSH, e := m.create(context.Background(), transferCreate{Operation: "copy", Source: "workspace", Destination: "workspace", DestinationPath: "ssh-copy", Paths: []string{"remote.bin"}, Concurrency: 4})
	if e != nil {
		t.Fatal(e)
	}
	started := time.Now()
	if e = m.start(sshToSSH); e != nil {
		t.Fatal(e)
	}
	testTransferWait(t, m, sshToSSH)
	t.Logf("SSH-to-SSH 65 MiB: %.1f MiB/s", float64(size)/(1<<20)/time.Since(started).Seconds())
	if testFileDigest(t, remote) != testFileDigest(t, filepath.Join(root, "remote/ssh-copy/remote.bin")) {
		t.Fatal("SSH to SSH checksum")
	}
	started = time.Now()
	copyJob, e := m.create(context.Background(), transferCreate{Operation: "copy", Source: "workspace", Destination: "owned-local", Paths: []string{"remote.bin"}})
	if e != nil {
		t.Fatal(e)
	}
	if e = m.start(copyJob); e != nil {
		t.Fatal(e)
	}
	testTransferWait(t, m, copyJob)
	t.Logf("SSH-to-local 65 MiB: %.1f MiB/s", float64(size)/(1<<20)/time.Since(started).Seconds())
	if testFileDigest(t, remote) != testFileDigest(t, filepath.Join(a.reference.Name(), "remote.bin")) {
		t.Fatal("SSH to local checksum")
	}
	started = time.Now()
	moveJob, e := m.create(context.Background(), transferCreate{Operation: "move", Source: "owned-local", Destination: "workspace", DestinationPath: "moved", Paths: []string{"remote.bin"}})
	if e != nil {
		t.Fatal(e)
	}
	if e = m.start(moveJob); e != nil {
		t.Fatal(e)
	}
	testTransferWait(t, m, moveJob)
	t.Logf("local-to-SSH 65 MiB: %.1f MiB/s", float64(size)/(1<<20)/time.Since(started).Seconds())
	if testFileDigest(t, remote) != testFileDigest(t, filepath.Join(root, "remote/moved/remote.bin")) {
		t.Fatal("local to SSH checksum")
	}
	if _, e = os.Stat(filepath.Join(a.reference.Name(), "remote.bin")); !os.IsNotExist(e) {
		t.Fatal("move source remains")
	}
	// A small-file tree stresses fixed per-file protocol round trips rather than
	// bulk throughput. The manifest is disposable and isolated with this fixture.
	smallRoot := filepath.Join(root, "remote/small-tree")
	if e = os.MkdirAll(smallRoot, 0700); e != nil {
		t.Fatal(e)
	}
	for n := 0; n < 100; n++ {
		if e = os.WriteFile(filepath.Join(smallRoot, fmt.Sprintf("f-%03d.txt", n)), []byte("small-file-transfer\n"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	started = time.Now()
	smallJob, e := m.create(context.Background(), transferCreate{Operation: "copy", Source: "workspace", Destination: "owned-local", Paths: []string{"small-tree"}, Concurrency: 4})
	if e != nil {
		t.Fatal(e)
	}
	if e = m.start(smallJob); e != nil {
		t.Fatal(e)
	}
	testTransferWait(t, m, smallJob)
	t.Logf("SSH-to-local 100 small files: %.1f files/s", 100/time.Since(started).Seconds())
	for n := 0; n < 100; n++ {
		got, readErr := os.ReadFile(filepath.Join(a.reference.Name(), "small-tree", fmt.Sprintf("f-%03d.txt", n)))
		if readErr != nil || string(got) != "small-file-transfer\n" {
			t.Fatalf("small file %d: %v", n, readErr)
		}
	}
	started = time.Now()
	downloadJob, e := m.create(context.Background(), transferCreate{Operation: "download", Source: "workspace", Paths: []string{"moved/remote.bin"}, RawDownload: true})
	if e != nil {
		t.Fatal(e)
	}
	if e = m.start(downloadJob); e != nil {
		t.Fatal(e)
	}
	testTransferWait(t, m, downloadJob)
	t.Logf("SSH raw download 65 MiB: %.1f MiB/s", float64(size)/(1<<20)/time.Since(started).Seconds())
	if testFileDigest(t, remote) != testFileDigest(t, filepath.Join(m.jobDir(downloadJob), "download.file")) {
		t.Fatal("SSH raw download checksum")
	}
	r := httptest.NewRequest("GET", "/api/file/stream?root=workspace&path=moved/remote.bin", nil)
	r.Header.Set("Authorization", "Bearer "+a.token)
	r.Header.Set("Range", "bytes=68157440-68157476")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	requireStatus(t, w, 206)
	if hash(w.Body.Bytes()) != hash(transferTestChunk(16, 37)) {
		t.Fatal("SSH range bytes")
	}
}
