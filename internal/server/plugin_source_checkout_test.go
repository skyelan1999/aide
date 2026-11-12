package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPluginSourceCheckoutPublication(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	a.mu.Lock()
	areas := a.pluginSourceAreasLocked()
	a.mu.Unlock()
	dir := t.TempDir()
	var total int64
	entries := map[string]*pluginSourceCheckout{}
	call := func(in pluginSourceRequest) (any, error) {
		return a.pluginCheckoutRequest(ctx, areas, dir, &total, entries, in)
	}
	original := []byte{0, 1, 255, 2}
	os.WriteFile(filepath.Join(a.workspace.Name(), "probe.db"), original, 0600)
	value, e := call(pluginSourceRequest{Operation: "checkout", Path: "probe.db", Slot: "db", Write: true})
	if e != nil {
		t.Fatal(e)
	}
	local := filepath.Join(dir, value.(map[string]any)["path"].(string))
	changed := []byte{0, 255, 1, 2, 3}
	os.WriteFile(local, changed, 0600)
	if _, e = call(pluginSourceRequest{Operation: "commit", Slot: "db"}); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(filepath.Join(a.workspace.Name(), "probe.db"))
	if string(b) != string(changed) {
		t.Fatal("binary writeback mismatch")
	}
	if _, e = call(pluginSourceRequest{Operation: "commit", Slot: "db"}); e == nil {
		t.Fatal("repeated commit accepted")
	}
	if _, e = call(pluginSourceRequest{Operation: "checkout", Path: "created.db", Slot: "new", Write: true, AllowCreate: true}); e != nil {
		t.Fatal(e)
	}
	if _, e = call(pluginSourceRequest{Operation: "commit", Slot: "new"}); e != nil {
		t.Fatal(e)
	}
	if st, e := os.Stat(filepath.Join(a.workspace.Name(), "created.db")); e != nil || st.Size() != 0 {
		t.Fatal("create failed", e)
	}
}

func TestPluginSourceCheckoutRejectsStaleUnsafeAndReadOnly(t *testing.T) {
	for _, scenario := range []string{"readonly", "conflict", "revision", "symlink", "journal", "hidden"} {
		t.Run(scenario, func(t *testing.T) {
			a := testApp(t)
			ctx := context.Background()
			os.WriteFile(filepath.Join(a.workspace.Name(), "probe.db"), []byte("before"), 0600)
			a.mu.Lock()
			areas := a.pluginSourceAreasLocked()
			a.mu.Unlock()
			dir := t.TempDir()
			var total int64
			entries := map[string]*pluginSourceCheckout{}
			call := func(in pluginSourceRequest) (any, error) {
				return a.pluginCheckoutRequest(ctx, areas, dir, &total, entries, in)
			}
			value, e := call(pluginSourceRequest{Operation: "checkout", Path: "probe.db", Slot: "db", Write: scenario != "readonly", SQLite: true})
			if e != nil {
				t.Fatal(e)
			}
			local := filepath.Join(dir, value.(map[string]any)["path"].(string))
			os.WriteFile(local, []byte("candidate"), 0600)
			switch scenario {
			case "conflict":
				os.WriteFile(filepath.Join(a.workspace.Name(), "probe.db"), []byte("external"), 0600)
			case "revision":
				a.mu.Lock()
				a.wsRevision++
				a.mu.Unlock()
			case "symlink":
				os.Remove(local)
				os.Symlink(filepath.Join(a.workspace.Name(), "probe.db"), local)
			case "journal":
				os.WriteFile(filepath.Join(a.workspace.Name(), "probe.db-wal"), []byte("live"), 0600)
			case "hidden":
				a.mu.Lock()
				a.wsRevision++
				a.mu.Unlock()
			}
			if _, e = call(pluginSourceRequest{Operation: "commit", Slot: "db"}); e == nil {
				t.Fatal("unsafe commit accepted")
			}
			b, _ := os.ReadFile(filepath.Join(a.workspace.Name(), "probe.db"))
			want := "before"
			if scenario == "conflict" {
				want = "external"
			}
			if string(b) != want {
				t.Fatal("source modified after rejection")
			}
		})
	}
	a := testApp(t)
	a.mu.Lock()
	areas := a.pluginSourceAreasLocked()
	a.mu.Unlock()
	var total int64
	for _, p := range []string{"../outside", ".env", "a/../../x"} {
		if _, e := a.pluginCheckoutRequest(context.Background(), areas, t.TempDir(), &total, map[string]*pluginSourceCheckout{}, pluginSourceRequest{Operation: "checkout", Path: p, Slot: "db", Write: true, AllowCreate: true}); e == nil {
			t.Fatal("unsafe path", p)
		}
	}
}

func TestPluginSourceCheckoutReferenceVisibilityAndRights(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	a.reference.WriteFile("probe.db", []byte("before"), 0600)
	a.mu.Lock()
	for i := range a.sourceRegistry.Sources {
		if a.sourceRegistry.Sources[i].ID == contextSource {
			a.sourceRegistry.Sources[i].RW = true
		}
	}
	areas := a.pluginSourceAreasLocked()
	a.mu.Unlock()
	dir := t.TempDir()
	var total int64
	entries := map[string]*pluginSourceCheckout{}
	_, e := a.pluginCheckoutRequest(ctx, areas, dir, &total, entries, pluginSourceRequest{Operation: "checkout", Source: "source:context", Path: "probe.db", Slot: "db", Write: true})
	if e != nil {
		t.Fatal(e)
	}
	a.mu.Lock()
	for i := range a.sourceRegistry.Sources {
		if a.sourceRegistry.Sources[i].ID == contextSource {
			a.sourceRegistry.Sources[i].AIHiddenWorkspaces = []string{a.wsID()}
		}
	}
	a.mu.Unlock()
	if _, e = a.pluginCheckoutRequest(ctx, areas, dir, &total, entries, pluginSourceRequest{Operation: "commit", Slot: "db"}); e == nil {
		t.Fatal("hidden reference committed")
	}
	if _, e = a.attachmentSourceBytes(Attachment{Root: "source", Source: contextSource, Path: "probe.db"}); e == nil {
		t.Fatal("hidden attachment read")
	}
}

func TestPluginSourceCheckoutCancellationAndRelock(t *testing.T) {
	for _, kind := range []string{"cancel", "lock-unlock"} {
		t.Run(kind, func(t *testing.T) {
			a := testApp(t)
			a.workspace.WriteFile("probe", []byte("before"), 0600)
			a.mu.Lock()
			areas := a.pluginSourceAreasLocked()
			a.mu.Unlock()
			dir := t.TempDir()
			var total int64
			entries := map[string]*pluginSourceCheckout{}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			out, e := a.pluginCheckoutRequest(ctx, areas, dir, &total, entries, pluginSourceRequest{Operation: "checkout", Path: "probe", Slot: "file", Write: true})
			if e != nil {
				t.Fatal(e)
			}
			os.WriteFile(filepath.Join(dir, out.(map[string]any)["path"].(string)), []byte("candidate"), 0600)
			if kind == "cancel" {
				cancel()
			} else {
				a.writeLockState(true)
				a.writeLockState(false)
			}
			if _, e = a.pluginCheckoutRequest(ctx, areas, dir, &total, entries, pluginSourceRequest{Operation: "commit", Slot: "file"}); e == nil {
				t.Fatal("revoked checkout committed")
			}
			b, _ := a.workspace.ReadFile("probe")
			if string(b) != "before" {
				t.Fatal("source changed after revocation")
			}
		})
	}
}
func TestPluginSourceAtomicPublishGuard(t *testing.T) {
	r, e := os.OpenRoot(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	r.WriteFile("probe", []byte("before"), 0640)
	f := &transferFS{root: r}
	if e = f.writeAtomicChecked("probe", []byte("candidate"), false, func() error { return context.Canceled }); e == nil {
		t.Fatal("publication ignored guard")
	}
	b, _ := r.ReadFile("probe")
	if string(b) != "before" {
		t.Fatal("guard failure overwrote source")
	}
	dir, e := r.Open(".")
	if e != nil {
		t.Fatal(e)
	}
	defer dir.Close()
	names, _ := dir.Readdirnames(-1)
	if len(names) != 1 || names[0] != "probe" {
		t.Fatal("temporary output leaked", names)
	}
}
