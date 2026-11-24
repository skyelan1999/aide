package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"testing"
)

func writeLargeArchiveFixture(t *testing.T, dir, name string) (string, int64, string) {
	t.Helper()
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.New()
	block := make([]byte, 1<<20)
	for i := range block {
		block[i] = byte(i*31 + i/251)
	}
	const size = int64(65 << 20)
	for written := int64(0); written < size; {
		n := int64(len(block))
		if remaining := size - written; n > remaining {
			n = remaining
		}
		if _, err = f.Write(block[:n]); err != nil {
			_ = f.Close()
			t.Fatal(err)
		}
		_, _ = h.Write(block[:n])
		written += n
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	return f.Name(), size, fmt.Sprintf("%x", h.Sum(nil))
}

func fileDigest(t *testing.T, name string) string {
	t.Helper()
	f, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func TestArchiveLargeFileRoundTripFormats(t *testing.T) {
	input, size, wantDigest := writeLargeArchiveFixture(t, t.TempDir(), "large.bin")
	for _, format := range []string{"zip", "tar", "tar.gz", "tar.bz2", "tar.xz"} {
		t.Run(format, func(t *testing.T) {
			output, err := os.CreateTemp(t.TempDir(), "archive-*")
			if err != nil {
				t.Fatal(err)
			}
			items := []archiveItem{{name: "large.bin", filePath: input, size: size}}
			if err = encodeArchiveFormatTo(context.Background(), items, format, output); err != nil {
				_ = output.Close()
				t.Fatal(err)
			}
			if err = output.Close(); err != nil {
				t.Fatal(err)
			}
			encoded, err := os.Open(output.Name())
			if err != nil {
				t.Fatal(err)
			}
			info, err := encoded.Stat()
			if err != nil {
				t.Fatal(err)
			}
			files, err := readArchiveFilesFromReader(context.Background(), encoded, info.Size(), "large."+format)
			closeErr := encoded.Close()
			if err != nil || closeErr != nil {
				t.Fatalf("decode failed: %v %v", err, closeErr)
			}
			defer cleanupExtractedArchive(files)
			if len(files) != 1 || files[0].path != "large.bin" || files[0].size != size {
				t.Fatalf("unexpected extracted metadata: %+v", files)
			}
			if got := fileDigest(t, files[0].filePath); got != wantDigest {
				t.Fatalf("large file digest mismatch: got %s want %s", got, wantDigest)
			}
		})
	}
}

type cancelArchiveWriter struct {
	cancel context.CancelFunc
}

func (w cancelArchiveWriter) Write(p []byte) (int, error) {
	w.cancel()
	return len(p), nil
}

func TestArchiveContextCancellationDoesNotPublish(t *testing.T) {
	payload := make([]byte, 1<<20)
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"zip", "tar"} {
		t.Run(format, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			err := encodeArchiveFormatTo(ctx, []archiveItem{{name: "payload.bin", data: payload, size: int64(len(payload))}}, format, cancelArchiveWriter{cancel: cancel})
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("encode error = %v", err)
			}
		})
	}

	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	fs := &transferFS{root: root}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = writeStreamToTransferFSContext(ctx, fs, "published.bin", bytes.NewReader([]byte("data"))); !errors.Is(err, context.Canceled) {
		t.Fatalf("publish error = %v", err)
	}
	if err = writeExtractedArchiveContext(ctx, fs, "extracted", []extractedArchiveFile{{path: "payload.txt", data: []byte("data")}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("extract error = %v", err)
	}
	entries, err := os.ReadDir(root.Name())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("cancellation left %d filesystem entries", len(entries))
	}
}

func TestArchiveEntryCountAndDepthHaveNoFixedCaps(t *testing.T) {
	items := make([]archiveItem, 0, 1001)
	for i := 0; i < 1001; i++ {
		items = append(items, archiveItem{name: fmt.Sprintf("many/%04d.txt", i), data: []byte("x"), size: 1})
	}
	encoded, err := encodeArchive(items)
	if err != nil {
		t.Fatal(err)
	}
	files, err := readArchiveFiles(encoded)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanupExtractedArchive(files)
	if len(files) != 1001 {
		t.Fatalf("entry count = %d", len(files))
	}

	a := testApp(t)
	deep := "deep"
	for i := 0; i < 40; i++ {
		deep = path.Join(deep, fmt.Sprintf("level-%02d", i))
	}
	if err = os.MkdirAll(filepath.Join(a.workPath, deep), 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(a.workPath, deep, "leaf.txt"), []byte("deep"), 0600); err != nil {
		t.Fatal(err)
	}
	collected, err := a.collectArchive("deep", Source{}, false, true)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanupArchiveItems(collected)
	if len(collected) != 42 {
		t.Fatalf("deep archive items = %d", len(collected))
	}
}

func TestWorkspaceArchiveLargeFileAndNoOverwrite(t *testing.T) {
	a := testApp(t)
	name := "large-workspace.bin"
	input, _, wantDigest := writeLargeArchiveFixture(t, a.workPath, name)
	w := request(a, "POST", "/api/file/archive", map[string]any{"root": "workspace", "path": name, "format": "zip"})
	requireStatus(t, w, 200)
	w = request(a, "POST", "/api/file/archive", map[string]any{"root": "workspace", "path": name, "format": "zip"})
	requireStatus(t, w, 409)
	if err := os.Remove(input); err != nil {
		t.Fatal(err)
	}
	w = request(a, "POST", "/api/file/extract", map[string]any{"root": "workspace", "path": name + ".zip"})
	requireStatus(t, w, 200)
	var extracted map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &extracted); err != nil {
		t.Fatal(err)
	}
	if got := fileDigest(t, filepath.Join(a.workPath, path.Join(extracted["path"], name))); got != wantDigest {
		t.Fatalf("workspace round trip digest mismatch: got %s want %s", got, wantDigest)
	}
}
