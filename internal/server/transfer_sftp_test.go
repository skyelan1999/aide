package server

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/pkg/sftp"
)

type transferSFTPStdio struct{}

func (transferSFTPStdio) Read(b []byte) (int, error)  { return os.Stdin.Read(b) }
func (transferSFTPStdio) Write(b []byte) (int, error) { return os.Stdout.Write(b) }
func (transferSFTPStdio) Close() error                { return nil }
func TestTransferSFTPProcess(t *testing.T) {
	if os.Getenv("AIDE_TRANSFER_SFTP_HELPER") != "1" {
		t.Skip("subprocess protocol helper")
	}
	server, e := sftp.NewServer(transferSFTPStdio{})
	if e != nil {
		os.Exit(2)
	}
	e = server.Serve()
	if e != nil && e != io.EOF {
		os.Exit(3)
	}
	os.Exit(0)
}
func TestLargeTransferSFTPRandomAccessAndResume(t *testing.T) {
	a := testApp(t)
	remote := t.TempDir()
	bin, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	stub := filepath.Join(t.TempDir(), "owned-ssh")
	// Reuse the master connection path, but run the real SFTP protocol server
	// through an owned pipe. Authentication itself belongs to live SSH acceptance.
	script := "#!/bin/sh\nfor arg do if [ \"$arg\" = sftp ]; then exec env AIDE_TRANSFER_SFTP_HELPER=1 " + shellQuote(bin) + " -test.run='^TestTransferSFTPProcess$'; fi; done\nexit 0\n"
	if e = os.WriteFile(stub, []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
	a.sshBin = stub
	a.wsConfig.Workspace.Mode = "ssh"
	a.wsConfig.Workspace.Host = "owned.invalid"
	a.wsConfig.Workspace.Path = remote
	// Do not run a lifecycle command against an unrelated global master socket.
	t.Cleanup(func() { a.sshBin = "" })
	m := a.transfers()
	size := transferChunkSize*2 + 29
	j, e := m.create(context.Background(), transferCreate{Operation: "upload", Name: "remote.bin", Size: size})
	if e != nil {
		t.Fatal(e)
	}
	for _, n := range []int64{2, 0} {
		requireStatus(t, transferTestPut(a, j.ID, n, transferTestChunk(n, min(transferChunkSize, size-n*transferChunkSize))), 200)
	}
	testTransferRestart(a)
	m = a.transfers()
	j = m.jobs[j.ID]
	requireStatus(t, request(a, "POST", "/api/file/transfers/"+j.ID+"/resume", map[string]any{}), 200)
	requireStatus(t, transferTestPut(a, j.ID, 1, transferTestChunk(1, transferChunkSize)), 200)
	if e = m.start(j); e != nil {
		t.Fatal(e)
	}
	testTransferWait(t, m, j)
	f, e := os.Open(filepath.Join(remote, "remote.bin"))
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	for n := int64(0); n*transferChunkSize < size; n++ {
		got := make([]byte, int(min(transferChunkSize, size-n*transferChunkSize)))
		if _, e = f.ReadAt(got, n*transferChunkSize); e != nil {
			t.Fatal(e)
		}
		if hash(got) != hash(transferTestChunk(n, int64(len(got)))) {
			t.Fatal("remote chunk mismatch")
		}
	}
	// A symlink cannot turn a relative transfer into access outside its SFTP root.
	outside := t.TempDir()
	_ = os.WriteFile(filepath.Join(outside, "private"), []byte("private"), 0600)
	if e = os.Symlink(outside, filepath.Join(remote, "escape")); e != nil {
		t.Fatal(e)
	}
	fs, e := a.streamTransferFS(context.Background(), "workspace", false)
	if e != nil {
		t.Fatal(e)
	}
	defer fs.Close()
	if _, e = fs.Open("escape/private", os.O_RDONLY); e == nil {
		t.Fatal("SFTP escaped root")
	}

}
