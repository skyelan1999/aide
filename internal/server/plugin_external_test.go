package server

import (
	"os"
	"path/filepath"
	"testing"
)

// Integration tests use independent plugin checkouts supplied explicitly by the
// developer. A missing configured source is a failure, not an implicit download.
func externalPluginSource(t *testing.T, id string, names ...string) string {
	t.Helper()
	root := os.Getenv("AIDE_PLUGIN_TEST_ROOT")
	if root == "" {
		t.Skip("independent-plugin integration: set AIDE_PLUGIN_TEST_ROOT to a directory containing plugin-ID checkouts")
	}
	parts := append([]string{root, id}, names...)
	source := filepath.Join(parts...)
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("independent plugin source %s: %v", id, err)
	}
	return source
}
