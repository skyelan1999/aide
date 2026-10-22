package localmounts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPathsAndOverlap(t *testing.T) {
	for _, p := range []string{"relative", "C:relative", "/", "E:/", "//server/share", "/tmp/a\nother"} {
		if _, e := Normalize(p); e == nil {
			t.Errorf("accepted %q", p)
		}
	}
	for in, want := range map[string]string{`e:\Projects\中文 space`: `E:/Projects/中文 space`, "/Volumes/SSD/Projects/": "/Volumes/SSD/Projects"} {
		got, e := Normalize(in)
		if e != nil || got != want {
			t.Fatalf("%s %s %v", in, got, e)
		}
	}
	if !Overlap("E:/Projects", `e:\projects\nested`) || Overlap("E:/Projects", "E:/Projects2") || Overlap("/Volumes/A", "/Volumes/a") {
		t.Fatal("incorrect directory boundary")
	}
	if _, ok := Relative("/Volumes/A", "/Volumes/A/../B"); ok {
		t.Fatal("traversal accepted")
	}
}
func TestPrepareRightsAndDisconnectedDrive(t *testing.T) {
	root := t.TempDir()
	a, b := filepath.Join(t.TempDir(), "read $literal"), filepath.Join(t.TempDir(), "write")
	os.Mkdir(a, 0700)
	os.Mkdir(b, 0700)
	c := Config{1, []Entry{{ID: "docs", Name: "Docs", Path: a, ReadOnly: true}, {ID: "output", Name: "Output", Path: b}, {ID: "offline", Name: "Offline", Path: filepath.Join(t.TempDir(), "disconnected"), ReadOnly: true}}}
	p := filepath.Join(root, ".aide-data/storage/data/local-mounts/desired.json")
	if e := Write(p, c); e != nil {
		t.Fatal(e)
	}
	active, override, e := Prepare(root, nil)
	if e != nil {
		t.Fatal(e)
	}
	if active.Entries[2].Error == "" {
		t.Fatal("missing drive not marked unavailable")
	}
	if _, e = os.Stat(c.Entries[2].Path); !os.IsNotExist(e) {
		t.Fatal("created disconnected drive directory")
	}
	raw, e := os.ReadFile(override)
	if e != nil {
		t.Fatal(e)
	}
	var doc struct {
		Services map[string]struct{ Volumes []Volume }
	}
	if e = json.Unmarshal(raw, &doc); e != nil {
		t.Fatal(e)
	}
	vs := doc.Services["aide"].Volumes
	if len(vs) != 3 || !vs[0].ReadOnly || vs[1].ReadOnly || !vs[2].ReadOnly || vs[0].Bind["create_host_path"] || !strings.Contains(vs[0].Source, "$$literal") {
		t.Fatalf("incorrect mounts %+v", vs)
	}
	before := string(raw)
	if _, _, e = Prepare(root, []string{filepath.Dir(a)}); e == nil {
		t.Fatal("alias overlaps an existing RW root")
	}
	raw, _ = os.ReadFile(override)
	if string(raw) != before {
		t.Fatal("failed preparation changed active config")
	}
}
func TestRejectDuplicateAndAlias(t *testing.T) {
	c := Config{1, []Entry{{ID: "docs", Name: "Docs", Path: "E:/Work", ReadOnly: true}, {ID: "output", Name: "Output", Path: "e:/work/other"}}}
	if Validate(c) == nil {
		t.Fatal("nested RO/RW roots accepted")
	}
	c.Entries[1].Path = "F:/Other"
	c.Entries[1].ID = "docs"
	if Validate(c) == nil {
		t.Fatal("duplicate id")
	}
	c.Entries[1].ID = "output"
	if err := Validate(c); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareRestoresSnapshotWhenOverrideCannotReplace(t *testing.T) {
	root := t.TempDir()
	native := filepath.Join(root, ".aide-data/local-mounts")
	os.MkdirAll(filepath.Join(native, "compose.json"), 0700)
	previous := Empty()
	if err := Write(filepath.Join(native, "active.json"), previous); err != nil {
		t.Fatal(err)
	}
	next := Config{1, []Entry{{ID: "next", Name: "Next", Path: t.TempDir(), ReadOnly: true}}}
	if err := Write(filepath.Join(root, ".aide-data/storage/data/local-mounts/desired.json"), next); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Prepare(root, nil); err == nil {
		t.Fatal("expected override replacement error")
	}
	actual, err := Read(filepath.Join(native, "active.json"))
	if err != nil || Revision(actual) != Revision(previous) {
		t.Fatalf("snapshot changed: %+v %v", actual, err)
	}
	os.Remove(filepath.Join(native, "active.json"))
	if _, _, err := Prepare(root, nil); err == nil {
		t.Fatal("expected override replacement error")
	}
	if _, err := os.Stat(filepath.Join(native, "active.json")); !os.IsNotExist(err) {
		t.Fatal("failed first preparation left active snapshot")
	}
}
