package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSemanticSearchReferenceSource(t *testing.T) {
	a := testApp(t)
	if err := os.WriteFile(filepath.Join(a.reference.Name(), "datasheet.txt"), []byte("Crest factor reduction is supported in the baseband processing chain."), 0600); err != nil {
		t.Fatal(err)
	}
	a.sourceRegistry.Sources = append(a.sourceRegistry.Sources,
		Source{ID: "ref-enabled", Name: "Datasheets", Type: "local", Enabled: true},
		Source{ID: "ref-disabled", Name: "Disabled", Type: "local", Enabled: false},
	)
	got := a.semanticSearch("crest factor reduction baseband", "ref-enabled", a.workspace)
	if !strings.Contains(got, "datasheet.txt") || !strings.Contains(got, "Crest factor reduction") {
		t.Fatalf("reference source result missing expected document: %s", got)
	}
	got = a.semanticSearch("crest factor reduction", "ref-disabled", a.workspace)
	if !strings.Contains(got, "不存在或已停用") {
		t.Fatalf("disabled source was not rejected: %s", got)
	}
}
