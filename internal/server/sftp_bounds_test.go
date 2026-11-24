package server

import (
	"bytes"
	"context"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func issue79SFTPFixture(t *testing.T, source bool) (*App, Source, string) {
	t.Helper()
	a := testApp(t)
	remote := t.TempDir()
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	a.sshBin = filepath.Join(t.TempDir(), "owned-ssh")
	script := "#!/bin/sh\nfor arg do if [ \"$arg\" = sftp ]; then exec env AIDE_TRANSFER_SFTP_HELPER=1 " + shellQuote(bin) + " -test.run='^TestTransferSFTPProcess$'; fi; done\nexit 0\n"
	if err := os.WriteFile(a.sshBin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.sshBin = "" })
	a.wsConfig.Workspace.Mode = "ssh"
	a.wsConfig.Workspace.Host = "owned.invalid"
	a.wsConfig.Workspace.Path = remote
	// The old path uses an owned CLI fixture; the repaired path speaks the actual
	// SFTP protocol to the helper above. Both read the same real temporary files.
	a.sftpBin = filepath.Join(t.TempDir(), "owned-sftp")
	cli := "#!/usr/bin/python3\nimport shlex,shutil,sys\nfor line in sys.stdin:\n a=shlex.split(line)\n if a and a[0]=='get':shutil.copyfile(a[1],a[2])\n"
	if err := os.WriteFile(a.sftpBin, []byte(cli), 0700); err != nil {
		t.Fatal(err)
	}
	src := Source{ID: "bounded-sftp", Type: "sftp", Enabled: true, Config: SourceConfig{Host: "owned.invalid", Username: "fixture", Auth: "none", Path: remote}}
	if source {
		a.sourceRegistry.Sources = append(a.sourceRegistry.Sources, src)
	}
	return a, src, remote
}
func TestIssue79SFTPReadBoundsAndBinaryControl(t *testing.T) {
	for _, source := range []bool{false, true} {
		name := "workspace"
		if source {
			name = "source"
		}
		t.Run(name, func(t *testing.T) {
			a, src, remote := issue79SFTPFixture(t, source)
			read := func(p string) ([]byte, error) {
				if source {
					return a.sftpReadSource(src, p)
				}
				return a.sftpRead(filepath.Join(remote, p))
			}
			large := filepath.Join(remote, "oversized.bin")
			f, err := os.Create(large)
			if err != nil {
				t.Fatal(err)
			}
			err = f.Truncate(72 << 20)
			_ = f.Close()
			if err != nil {
				t.Fatal(err)
			}
			temp := t.TempDir()
			t.Setenv("TMPDIR", temp)
			if b, err := read("oversized.bin"); err != nil || len(b) != 72<<20 {
				t.Fatalf("large remote file rejected or truncated: %d bytes %v", len(b), err)
			}
			files, err := os.ReadDir(temp)
			if err != nil || len(files) != 0 {
				t.Fatalf("raw SFTP read left a local spool: %v %v", files, err)
			}
			want := []byte{0, 255, 'o', 'k', '\n'}
			if err := os.WriteFile(filepath.Join(remote, "binary control.bin"), want, 0600); err != nil {
				t.Fatal(err)
			}
			got, err := read("binary control.bin")
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("bounded binary read changed bytes: %v %v", got, err)
			}
			if _, err := read("missing.bin"); !isSFTPNotExistErr(err) {
				t.Fatalf("missing-file compatibility lost: %v", err)
			}
		})
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

func TestIssue79SFTPStreamWriteLarge(t *testing.T) {
	a, _, _ := issue79SFTPFixture(t, true)
	for i := range a.sourceRegistry.Sources {
		if a.sourceRegistry.Sources[i].ID == "bounded-sftp" {
			a.sourceRegistry.Sources[i].RW = true
		}
	}
	fs, err := a.streamTransferFS(context.Background(), "bounded-sftp", true)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	const size = int64(65 << 20)
	input := io.MultiReader(bytes.NewReader([]byte("head")), io.LimitReader(zeroReader{}, size-8), bytes.NewReader([]byte("tail")))
	if err = writeStreamToTransferFS(fs, "large-write.bin", input); err != nil {
		t.Fatal(err)
	}
	f, err := fs.Open("large-write.bin", os.O_RDONLY)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.Size() != size {
		t.Fatalf("streamed size = %v, err=%v", info, err)
	}
	tail := make([]byte, 4)
	if _, err = f.ReadAt(tail, size-4); err != nil || string(tail) != "tail" {
		t.Fatalf("streamed tail = %q, err=%v", tail, err)
	}
}

func TestIssue79SFTPRawPreviewStreamsLargeRange(t *testing.T) {
	a, _, remote := issue79SFTPFixture(t, true)
	f, err := os.Create(filepath.Join(remote, "range.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(65 << 20); err != nil {
		t.Fatal(err)
	}
	if _, err = f.WriteAt([]byte("stream-prefix"), 0); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	req := httptest.NewRequest("GET", "/api/file/raw?source=bounded-sftp&path=range.bin", nil)
	req.Header.Set("Range", "bytes=0-12")
	w := httptest.NewRecorder()
	a.readFileRaw(w, req)
	requireStatus(t, w, 206)
	if w.Body.String() != "stream-prefix" {
		t.Fatalf("streamed range mismatch: %q", w.Body.String())
	}
}

type issue79StaleStatFile struct {
	*os.File
	info os.FileInfo
}

func (f issue79StaleStatFile) Stat() (os.FileInfo, error) { return f.info, nil }

func TestIssue79SFTPReadHandlesStaleStatWithoutTruncation(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "growing.bin"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(72 << 20); err != nil {
		t.Fatal(err)
	}
	data, err := readBoundedSFTPFile(issue79StaleStatFile{f, info})
	if err != nil || len(data) != 72<<20 {
		t.Fatalf("large file was not read in full: %d %v", len(data), err)
	}
	position, err := f.Seek(0, 1)
	if err != nil || position != 72<<20 {
		t.Fatalf("read did not consume the full file: %d %v", position, err)
	}
}
func TestIssue79BatchOutputProcess(t *testing.T) {
	mode := os.Getenv("AIDE_SFTP_OUTPUT_HELPER")
	if mode == "" {
		t.Skip("owned subprocess helper")
	}
	output := os.Stdout
	if mode == "stderr" {
		output = os.Stderr
	}
	block := bytes.Repeat([]byte("x"), 32<<10)
	for n := 0; n < 512; n++ {
		if _, err := output.Write(block); err != nil {
			os.Exit(4)
		}
	}
	if err := os.WriteFile(os.Getenv("AIDE_SFTP_OUTPUT_FINISHED"), []byte("done"), 0600); err != nil {
		os.Exit(5)
	}
	os.Exit(0)
}
func TestIssue79SFTPBatchOutputBoundedDuringCapture(t *testing.T) {
	for _, mode := range []string{"stdout", "stderr"} {
		t.Run(mode, func(t *testing.T) {
			a := testApp(t)
			bin, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(t.TempDir(), "completed")
			a.sftpBin = filepath.Join(t.TempDir(), "sftp")
			script := "#!/bin/sh\ncat >/dev/null\nexec env AIDE_SFTP_OUTPUT_HELPER=" + shellQuote(mode) + " AIDE_SFTP_OUTPUT_FINISHED=" + shellQuote(marker) + " " + shellQuote(bin) + " -test.run='^TestIssue79BatchOutputProcess$'\n"
			if err := os.WriteFile(a.sftpBin, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			out, err := a.runSFTPBatch(context.Background(), nil, "ls -l\n")
			if err == nil || len(out) > 4<<20 {
				t.Fatalf("unbounded %s capture: bytes=%d err=%v", mode, len(out), err)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatal("producer completed despite capture limit", err)
			}
		})
	}
}
