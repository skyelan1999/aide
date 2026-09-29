package server

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestFileTransferMultipleAndDirectories(t *testing.T) {
	a := testApp(t)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.Mkdir(filepath.Join(a.workPath, "nested"), 0755))
	must(os.Mkdir(filepath.Join(a.workPath, "target"), 0755))
	original := []byte{0, 1, 2, 255}
	must(os.WriteFile(filepath.Join(a.workPath, "binary.dat"), original, 0644))
	must(os.WriteFile(filepath.Join(a.workPath, "nested", "child.txt"), []byte("child"), 0644))
	requireStatus(t, request(a, "POST", "/api/file/transfer", fileTransferRequest{Operation: "copy", Source: "workspace", Paths: []string{"binary.dat", "nested"}, Destination: "workspace", DestinationPath: "target"}), 200)
	copied, err := os.ReadFile(filepath.Join(a.workPath, "target", "binary.dat"))
	must(err)
	if !bytes.Equal(copied, original) {
		t.Fatalf("binary copy changed bytes: %v", copied)
	}
	child, err := os.ReadFile(filepath.Join(a.workPath, "target", "nested", "child.txt"))
	must(err)
	if string(child) != "child" {
		t.Fatalf("directory copy: %q", child)
	}
	if _, err := os.Stat(filepath.Join(a.workPath, "nested", "child.txt")); err != nil {
		t.Fatalf("copy deleted source: %v", err)
	}
	requireStatus(t, request(a, "POST", "/api/file/transfer", fileTransferRequest{Operation: "copy", Source: "workspace", Paths: []string{"binary.dat", "nested"}, Destination: "workspace", DestinationPath: "target"}), 409)
	if err := os.Mkdir(filepath.Join(a.workPath, "other"), 0755); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, "POST", "/api/file/transfer", fileTransferRequest{Operation: "move", Source: "workspace", Paths: []string{"binary.dat", "nested"}, Destination: "workspace", DestinationPath: "other"}), 200)
	if _, err := os.Stat(filepath.Join(a.workPath, "binary.dat")); !os.IsNotExist(err) {
		t.Fatalf("move retained source: %v", err)
	}
	if _, err := os.Stat(filepath.Join(a.workPath, "other", "nested", "child.txt")); err != nil {
		t.Fatalf("move lost child: %v", err)
	}
}

func TestFileTransferReferencesAndBoundaries(t *testing.T) {
	a := testApp(t)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.Mkdir(filepath.Join(a.workPath, "dest"), 0755))
	must(os.WriteFile(filepath.Join(a.reference.Name(), "note.txt"), []byte("reference"), 0644))
	a.sourceRegistry.Sources = append(a.sourceRegistry.Sources, Source{ID: "test-readonly", Name: "Read only", Type: "local", Enabled: true, RW: false})
	copyReq := fileTransferRequest{Operation: "copy", Source: "test-readonly", Paths: []string{"note.txt"}, Destination: "workspace", DestinationPath: "dest"}
	requireStatus(t, request(a, "POST", "/api/file/transfer", copyReq), 200)
	b, err := os.ReadFile(filepath.Join(a.workPath, "dest", "note.txt"))
	must(err)
	if string(b) != "reference" {
		t.Fatalf("reference copy: %q", b)
	}
	copyReq.Operation = "move"
	requireStatus(t, request(a, "POST", "/api/file/transfer", copyReq), 403)
	if _, err := os.Stat(filepath.Join(a.reference.Name(), "note.txt")); err != nil {
		t.Fatalf("read-only source lost: %v", err)
	}
	copyReq.Operation = "copy"
	copyReq.Destination = "test-readonly"
	requireStatus(t, request(a, "POST", "/api/file/transfer", copyReq), 400)
	copyReq.Source = "workspace"
	copyReq.Destination = "workspace"
	copyReq.Paths = []string{"dest"}
	copyReq.DestinationPath = "dest"
	requireStatus(t, request(a, "POST", "/api/file/transfer", copyReq), 400)
	copyReq.Paths = []string{"../note.txt"}
	copyReq.DestinationPath = "."
	requireStatus(t, request(a, "POST", "/api/file/transfer", copyReq), 400)
}

func TestFileTransferWritableReference(t *testing.T) {
	a := testApp(t)
	if err := os.Mkdir(filepath.Join(a.reference.Name(), "out"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.workPath, "draft.txt"), []byte("draft"), 0644); err != nil {
		t.Fatal(err)
	}
	a.sourceRegistry.Sources = append(a.sourceRegistry.Sources, Source{ID: "writable-ref", Name: "Writable", Type: "local", Enabled: true, RW: true})
	req := fileTransferRequest{Operation: "move", Source: "workspace", Paths: []string{"draft.txt"}, Destination: "writable-ref", DestinationPath: "out"}
	requireStatus(t, request(a, "POST", "/api/file/transfer", req), 200)
	if _, err := os.Stat(filepath.Join(a.workPath, "draft.txt")); !os.IsNotExist(err) {
		t.Fatalf("source remained: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(a.reference.Name(), "out", "draft.txt"))
	if err != nil || string(b) != "draft" {
		t.Fatalf("destination: %q %v", b, err)
	}
	req.Source = "writable-ref"
	req.Destination = "workspace"
	req.DestinationPath = "."
	req.Paths = []string{"out/draft.txt"}
	requireStatus(t, request(a, "POST", "/api/file/transfer", req), 200)
	if _, err := os.Stat(filepath.Join(a.reference.Name(), "out", "draft.txt")); !os.IsNotExist(err) {
		t.Fatalf("reference source remained: %v", err)
	}
	if _, err := os.Stat(filepath.Join(a.workPath, "draft.txt")); err != nil {
		t.Fatalf("workspace target missing: %v", err)
	}
}
