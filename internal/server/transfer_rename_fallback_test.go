package server

import (
	"os"
	"testing"
)

func TestTransferRenameExclusiveFallbackPublishesFile(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err = root.WriteFile("stage", []byte("uploaded payload"), 0640); err != nil {
		t.Fatal(err)
	}
	if err = transferRenameExclusiveFallback(root, "stage", "target"); err != nil {
		t.Fatal(err)
	}
	content, err := root.ReadFile("target")
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "uploaded payload" {
		t.Fatalf("published content = %q", content)
	}
	if _, err = root.Lstat("stage"); !os.IsNotExist(err) {
		t.Fatalf("stage still exists after publish: %v", err)
	}
}

func TestTransferRenameExclusiveFallbackNeverReplacesTarget(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err = root.WriteFile("stage", []byte("new upload"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = root.WriteFile("target", []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = transferRenameExclusiveFallback(root, "stage", "target"); !os.IsExist(err) {
		t.Fatalf("existing target error = %v", err)
	}
	for name, expected := range map[string]string{"stage": "new upload", "target": "original"} {
		content, readErr := root.ReadFile(name)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(content) != expected {
			t.Fatalf("%s content = %q, want %q", name, content, expected)
		}
	}
}

func TestTransferRenameExclusiveFallbackPublishesDirectory(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err = root.Mkdir("stage", 0700); err != nil {
		t.Fatal(err)
	}
	if err = root.WriteFile("stage/child", []byte("child"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = transferRenameExclusiveFallback(root, "stage", "target"); err != nil {
		t.Fatal(err)
	}
	if content, readErr := root.ReadFile("target/child"); readErr != nil || string(content) != "child" {
		t.Fatalf("published directory content = %q, %v", content, readErr)
	}
}
