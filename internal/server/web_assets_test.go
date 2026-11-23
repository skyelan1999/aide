package server

import (
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmbeddedWebAssetsMatchRuntimeInventory(t *testing.T) {
	buildOnly := func(path string) bool {
		name := strings.TrimPrefix(path, "web/")
		return strings.HasSuffix(path, ".ts") || path == "web/TYPESCRIPT.md" ||
			(!strings.Contains(name, "/") && strings.HasPrefix(name, "tsconfig") && strings.HasSuffix(name, ".json"))
	}
	err := filepath.WalkDir("web", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		path = filepath.ToSlash(path)
		file, err := assets.Open(path)
		if buildOnly(path) {
			if err == nil {
				file.Close()
				t.Errorf("build-time source leaked into runtime assets: %s", path)
			}
			return nil
		}
		if err != nil {
			t.Errorf("runtime asset is not embedded: %s: %v", path, err)
			return nil
		}
		return file.Close()
	})
	if err != nil {
		t.Fatal(err)
	}
}
