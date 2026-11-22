package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildPjplanFilename(t *testing.T) {
	settings := map[string]any{"projectIdentifier": "1234567", "versionTrack": "P", "majorVersion": "A", "minorVersion": "1"}
	classes := []pjplanClass{{Code: "10262", Name: "Design Specification"}, {Code: "19812", Name: "需求文档"}}
	name, err := buildPjplanFilename(settings, classes, "102 62", "md")
	if err != nil {
		t.Fatal(err)
	}
	if name != "102 62 Design Specification - 1234567 - PA1.md" {
		t.Fatalf("unexpected filename %q", name)
	}
	if _, err := buildPjplanFilename(settings, classes, "99999", "md"); err == nil {
		t.Fatal("unregistered class must fail")
	}
	settings["projectIdentifier"] = "非法 标识!"
	if _, err := buildPjplanFilename(settings, classes, "10262", "md"); err == nil {
		t.Fatal("invalid project identifier must fail")
	}
}

func TestPjplanValidationSkippedWhenDisabled(t *testing.T) {
	a := &App{}
	if a.pjplanEnabled() {
		t.Fatal("empty registry must report disabled")
	}
	if err := a.validatePjplanFilename("REQ-001-任意命名.md"); err != nil {
		t.Fatalf("disabled PjPlan must not reject legacy names: %v", err)
	}
}

func TestRequirementsIndexIncludesPjplanNamedDocuments(t *testing.T) {
	dir := t.TempDir()
	legacy := "# REQ-001 · 传统命名\n\n- 编号：REQ-001\n- 状态：待评审\n"
	pjNamed := "# REQ-002 · PjPlan 命名\n\n- 编号：REQ-002\n- 状态：待评审\n"
	if err := os.WriteFile(filepath.Join(dir, "REQ-001-传统命名.md"), []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	pjName := "198 12 需求文档 - 1234567 - PA1.md"
	if err := os.WriteFile(filepath.Join(dir, pjName), []byte(pjNamed), 0600); err != nil {
		t.Fatal(err)
	}
	indexFile := "120 2 索引 - 1234567 - PA1.md"
	if err := (&App{}).rewriteRequirementsIndex(dir, indexFile); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, indexFile))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{"REQ-001", "REQ-002", "共 2 个需求"} {
		if !strings.Contains(text, want) {
			t.Fatalf("index missing %q:\n%s", want, text)
		}
	}
}
