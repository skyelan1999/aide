package server

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
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
	copyJob, e := m.create(context.Background(), transferCreate{Operation: "copy", Source: "workspace", Destination: "owned-local", Paths: []string{"remote.bin"}})
	if e != nil {
		t.Fatal(e)
	}
	if e = m.start(copyJob); e != nil {
		t.Fatal(e)
	}
	testTransferWait(t, m, copyJob)
	if testFileDigest(t, remote) != testFileDigest(t, filepath.Join(a.reference.Name(), "remote.bin")) {
		t.Fatal("SSH to local checksum")
	}
	moveJob, e := m.create(context.Background(), transferCreate{Operation: "move", Source: "owned-local", Destination: "workspace", DestinationPath: "moved", Paths: []string{"remote.bin"}})
	if e != nil {
		t.Fatal(e)
	}
	if e = m.start(moveJob); e != nil {
		t.Fatal(e)
	}
	testTransferWait(t, m, moveJob)
	if testFileDigest(t, remote) != testFileDigest(t, filepath.Join(root, "remote/moved/remote.bin")) {
		t.Fatal("local to SSH checksum")
	}
	if _, e = os.Stat(filepath.Join(a.reference.Name(), "remote.bin")); !os.IsNotExist(e) {
		t.Fatal("move source remains")
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
