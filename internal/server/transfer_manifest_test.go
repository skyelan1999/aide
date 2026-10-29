package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pkg/sftp"
)

// Exercise admission through the real filesystem: the top-level selection is
// tiny but its recursive manifest exceeds the entry or encoded-byte budget.
func TestIssue71RecursiveManifestAdmissionBounds(t *testing.T) {
	for _, kind := range []string{"entries", "encoded-bytes"} {
		t.Run(kind, func(t *testing.T) {
			a := testApp(t)
			rel := "tree"
			count := 100000
			if kind == "encoded-bytes" {
				rel += "/" + strings.Repeat(strings.Repeat("x", 180)+"/", 10) + "leaf"
				count = 5000
			}
			dir := filepath.Join(a.workPath, filepath.FromSlash(rel))
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < count; i++ {
				f, err := os.Create(filepath.Join(dir, fmt.Sprintf("f-%06d", i)))
				if err != nil {
					t.Fatal(err)
				}
				if err := f.Close(); err != nil {
					t.Fatal(err)
				}
			}
			m := a.transfers()
			j, err := m.create(context.Background(), transferCreate{Operation: "download", Paths: []string{"tree"}})
			if err == nil {
				t.Fatalf("accepted unbounded %s manifest with %d entries", kind, len(j.Parts))
			}
			expected := "100000"
			if kind == "encoded-bytes" {
				expected = "16MiB"
			}
			if !strings.Contains(err.Error(), expected) {
				t.Fatalf("wrong admission error: %v", err)
			}
			if len(m.jobs) != 0 {
				t.Fatal("rejected manifest registered a job")
			}
			saved, err := os.ReadDir(m.dir)
			if err != nil || len(saved) != 0 {
				t.Fatalf("rejected manifest persisted job data: %v, %v", saved, err)
			}
			if err := os.WriteFile(filepath.Join(a.workPath, "small.txt"), []byte("control"), 0600); err != nil {
				t.Fatal(err)
			}
			small, err := m.create(context.Background(), transferCreate{Operation: "download", Paths: []string{"small.txt"}})
			if err != nil || len(small.Parts) != 1 {
				t.Fatalf("bounded control rejected: %v", err)
			}
		})
	}
}

func TestIssue71DirectoryEnumerationStopsAndCancels(t *testing.T) {
	for _, remote := range []bool{false, true} {
		t.Run(fmt.Sprintf("sftp=%v", remote), func(t *testing.T) {
			dir := t.TempDir()
			for i := 0; i < 2048; i++ {
				if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f-%04d", i)), nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Mkdir(filepath.Join(dir, "nested"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "nested/child"), []byte("child"), 0600); err != nil {
				t.Fatal(err)
			}
			fs := &transferFS{}
			if remote {
				serverConn, clientConn := net.Pipe()
				server, err := sftp.NewServer(serverConn)
				if err != nil {
					t.Fatal(err)
				}
				done := make(chan struct{})
				go func() { defer close(done); _ = server.Serve() }()
				t.Cleanup(func() { _ = server.Close(); <-done })
				client, err := sftp.NewClientPipe(clientConn, clientConn)
				if err != nil {
					t.Fatal(err)
				}
				fs.client, fs.base = client, dir
			} else {
				root, err := os.OpenRoot(dir)
				if err != nil {
					t.Fatal(err)
				}
				fs.root = root
			}
			defer fs.Close()
			stop := errors.New("owned visitor budget exhausted")
			visited := 0
			err := fs.readDirEach(context.Background(), ".", func(os.FileInfo) error {
				visited++
				if visited == 3 {
					return stop
				}
				return nil
			})
			if !errors.Is(err, stop) || visited != 3 {
				t.Fatalf("iterator ignored admission stop: count=%d err=%v", visited, err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			visited = 0
			err = fs.readDirEach(ctx, ".", func(os.FileInfo) error { visited++; cancel(); return nil })
			if !errors.Is(err, context.Canceled) || visited != 1 {
				t.Fatalf("iterator ignored cancellation: count=%d err=%v", visited, err)
			}
			// Reuse after both early exits, across multiple packets/batches.
			visited = 0
			nested := 0
			err = fs.readDirEach(context.Background(), ".", func(info os.FileInfo) error {
				visited++
				if info.IsDir() {
					return fs.readDirEach(context.Background(), info.Name(), func(os.FileInfo) error { nested++; return nil })
				}
				return nil
			})
			if err != nil || visited != 2049 || nested != 1 {
				t.Fatalf("reuse/nested enumeration failed: %d/%d, %v", visited, nested, err)
			}
		})
	}
}

func TestIssue71ManifestCreationAdmissionAndCancellation(t *testing.T) {
	a := testApp(t)
	if err := os.WriteFile(filepath.Join(a.workPath, "small.txt"), []byte("control"), 0600); err != nil {
		t.Fatal(err)
	}
	m := a.transfers()
	in := transferCreate{Operation: "download", Paths: []string{"small.txt"}}
	for i := 0; i < cap(m.builders); i++ {
		m.builders <- struct{}{}
	}
	if _, err := m.create(context.Background(), in); err == nil || !strings.Contains(err.Error(), "繁忙") {
		t.Fatalf("saturated builders accepted work: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.create(ctx, in); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	for i := 0; i < cap(m.builders); i++ {
		<-m.builders
	}
	if _, err := m.create(context.Background(), in); err != nil {
		t.Fatal(err)
	}
}
