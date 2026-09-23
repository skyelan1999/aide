package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnvironmentGuide(t *testing.T) {
	a := testApp(t)
	if a.environmentGuide() != "" {
		t.Fatal("disabled plugin injected context")
	}
	a.pluginRegistry.Plugins = append(a.pluginRegistry.Plugins, PluginManifest{ID: "environment-guide", Enabled: true})
	os.WriteFile(filepath.Join(a.workPath, "README.md"), []byte("PRIVATE BODY"), 0600)
	os.WriteFile(filepath.Join(a.workPath, ".env"), []byte("SECRET"), 0600)
	src := Source{ID: "remote", Name: "Remote reference", Type: "link", Enabled: true}
	src.Config.URL = "https://secret:credential@example.com/?token=hidden"
	a.sourceRegistry.Sources = append(a.sourceRegistry.Sources, src, Source{ID: "disabled", Name: "Disabled source", Enabled: false})
	guide := a.environmentGuide()
	for _, want := range []string{"README.md", "Remote reference", "UNTRUSTED", "Contents not read"} {
		if !strings.Contains(guide, want) {
			t.Fatalf("missing %s", want)
		}
	}
	for _, bad := range []string{"PRIVATE BODY", "SECRET", ".env", "credential", "token=hidden", "Disabled source"} {
		if strings.Contains(guide, bad) {
			t.Fatalf("leaked %s", bad)
		}
	}
	preview := a.buildContextPreview(nil, "Explain workspace", "chat", "", a.settings, ProfileParams{MaxTokens: 512}, true)
	if !strings.Contains(preview.Messages[0].Content, "Environment guide plugin") {
		t.Fatal("not in actual request builder")
	}
	if preview.Breakdown.SystemChars != len(preview.Messages[0].Content) {
		t.Fatal("not counted in budget")
	}
	a.pluginRegistry.Plugins[len(a.pluginRegistry.Plugins)-1].Enabled = false
	if a.environmentGuide() != "" {
		t.Fatal("toggle off ignored")
	}
}
