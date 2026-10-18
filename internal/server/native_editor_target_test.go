package server

import (
	"os"
	"path/filepath"
	"testing"
)

func editorTargetFixture(t *testing.T) (*os.Root, nativeEditorTargetConfig, nativeEditorOpenRequest) {
	t.Helper()
	dir := t.TempDir()
	for _, p := range []string{"session/project", "session/bridge"} {
		if e := os.MkdirAll(filepath.Join(dir, p), 0700); e != nil {
			t.Fatal(e)
		}
	}
	for _, ext := range []string{".kicad_pro", ".kicad_sch", ".kicad_pcb"} {
		os.WriteFile(filepath.Join(dir, "session/project/STM32"+ext), []byte("fixture"), 0600)
	}
	os.WriteFile(filepath.Join(dir, "session/bridge/session.json"), []byte(`{"generation":"generation_1"}`), 0600)
	root, e := os.OpenRoot(dir)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { root.Close() })
	return root, nativeEditorTargetConfig{dir, "session"}, nativeEditorOpenRequest{1, "editor_1", "session", "pcb"}
}
func TestNativeEditorTargetBoundToConfiguredWorkspace(t *testing.T) {
	root, cfg, in := editorTargetFixture(t)
	out, e := prepareNativeEditorTarget(root, cfg, in)
	if e != nil {
		t.Fatal(e)
	}
	if out.ProjectStem != "STM32" || out.Generation != "generation_1" || len(out.Versions) != 4 {
		t.Fatalf("bad snapshot %#v", out)
	}
	cfg.WorkspaceRoot = t.TempDir()
	if _, e = prepareNativeEditorTarget(root, cfg, in); e == nil {
		t.Fatal("accepted foreign workspace")
	}
}
func TestNativeEditorTargetRejectsAliasesAndMalformedSession(t *testing.T) {
	for _, change := range []func(*nativeEditorOpenRequest){func(r *nativeEditorOpenRequest) { r.SessionPath = "../session" }, func(r *nativeEditorOpenRequest) { r.SessionPath = "other" }, func(r *nativeEditorOpenRequest) { r.View = "terminal" }, func(r *nativeEditorOpenRequest) { r.RequestID = "arbitrary" }} {
		root, cfg, in := editorTargetFixture(t)
		change(&in)
		if _, e := prepareNativeEditorTarget(root, cfg, in); e == nil {
			t.Fatal("accepted invalid request")
		}
	}
	root, cfg, in := editorTargetFixture(t)
	p := filepath.Join(root.Name(), "session/project/STM32.kicad_pcb")
	os.Remove(p)
	os.Symlink(filepath.Join(t.TempDir(), "elsewhere"), p)
	if _, e := prepareNativeEditorTarget(root, cfg, in); e == nil {
		t.Fatal("accepted symlink")
	}
}
func TestNativeEditorTargetRejectsMultipleProjectsAndMissingGeneration(t *testing.T) {
	root, cfg, in := editorTargetFixture(t)
	os.WriteFile(filepath.Join(root.Name(), "session/project/other.kicad_pro"), []byte("other"), 0600)
	if _, e := prepareNativeEditorTarget(root, cfg, in); e == nil {
		t.Fatal("accepted ambiguous project")
	}
	os.Remove(filepath.Join(root.Name(), "session/project/other.kicad_pro"))
	os.WriteFile(filepath.Join(root.Name(), "session/bridge/session.json"), []byte(`{}`), 0600)
	if _, e := prepareNativeEditorTarget(root, cfg, in); e == nil {
		t.Fatal("accepted missing generation")
	}
}
