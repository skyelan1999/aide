package main

import (
	"aide/internal/localmounts"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCompletedMailboxRequest(t *testing.T) {
	dir := t.TempDir()
	for _, state := range []string{"done", "error", "running"} {
		if err := localmounts.Write(filepath.Join(dir, "response.json"), response{ID: "finished", State: state}); err != nil {
			t.Fatal(err)
		}
		if got := completedRequest(dir, "finished"); got != (state != "running") {
			t.Fatalf("%s completed=%v", state, got)
		}
		if completedRequest(dir, "other") {
			t.Fatal("suppressed another request")
		}
	}
}
func TestHelperSingleInstanceAndStopDuringAction(t *testing.T) {
	dir := t.TempDir()
	release, err := acquireHelper(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err = acquireHelper(dir); err == nil {
		t.Fatal("second owner acquired lock")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go watchStop(ctx, cancel, dir)
	if err = os.WriteFile(filepath.Join(dir, "stop"), []byte("stop"), 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("in-flight action not cancelled")
	}

}
func TestStopTimeoutPreservesLiveOwner(t *testing.T) {
	dir := t.TempDir()
	release, err := acquireHelper(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if stopHelper(dir, 10*time.Millisecond) == nil {
		t.Fatal("stop silently replaced live helper")
	}
	if _, err = os.Stat(filepath.Join(dir, "serve.pid")); err != nil {
		t.Fatal(err)
	}
}
func TestComposeChoiceAndRejectArguments(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fixture")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "docker-compose")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AIDE_LOCAL_MOUNTS_COMPOSE", script)
	out, err := compose(context.Background(), "not-docker", "config", "--format", "json")
	if err != nil || string(out) != "config\n--format\njson\n" {
		t.Fatalf("%s %v", out, err)
	}
	t.Setenv("AIDE_LOCAL_MOUNTS_COMPOSE", "docker-compose;touch bad")
	if _, err = compose(context.Background(), script, "config"); err == nil {
		t.Fatal("accepted arbitrary command")
	}
	t.Setenv("AIDE_LOCAL_MOUNTS_COMPOSE", "docker")
	out, err = compose(context.Background(), script, "config")
	if err != nil || string(out) != "compose\nconfig\n" {
		t.Fatalf("%s %v", out, err)
	}
}
func TestApplyRecreatesOnlyAideAndRefreshesForwarders(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fixture")
	}
	for _, failFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "apply", true: "rollback"}[failFirst], func(t *testing.T) {
			root := t.TempDir()
			native := filepath.Join(root, ".aide-data/local-mounts")
			os.MkdirAll(native, 0700)
			previous := localmounts.Empty()
			localmounts.Write(filepath.Join(native, "active.json"), previous)
			localmounts.Write(filepath.Join(native, "compose.json"), map[string]any{"services": map[string]any{}})
			selected := t.TempDir()
			desired := localmounts.Config{Version: 1, Entries: []localmounts.Entry{{ID: "docs", Name: "docs", Path: selected, ReadOnly: true}}}
			localmounts.Write(filepath.Join(root, ".aide-data/storage/data/local-mounts/desired.json"), desired)
			script := filepath.Join(root, "docker-compose")
			body := "#!/bin/sh\ncd \"$(dirname \"$0\")\"\nprintf '%s\\n' \"$*\" >> calls\nif [ \"$1\" = config ];then printf '%s\\n' '{\"services\":{\"aide\":{\"volumes\":[]}}}';exit;fi\n"
			if failFirst {
				body += "if [ ! -e first ];then touch first;exit 1;fi\n"
			}
			os.WriteFile(script, []byte(body), 0700)
			t.Setenv("AIDE_LOCAL_MOUNTS_COMPOSE", script)
			os.Mkdir(filepath.Join(root, "scripts"), 0700)
			os.WriteFile(filepath.Join(root, "scripts/message-lan-control.sh"), []byte("#!/bin/sh\nprintf refreshed > \"$(dirname \"$0\")/../refreshed\"\n"), 0700)
			prior := confirmApplication
			confirmApplication = func(context.Context) error { return nil }
			defer func() { confirmApplication = prior }()
			err := apply(context.Background(), root, script, localmounts.Revision(desired))
			if (err != nil) != failFirst {
				t.Fatal(err)
			}
			if _, err = os.Stat(filepath.Join(root, "refreshed")); err != nil {
				t.Fatal("forwarders not refreshed", err)
			}
			calls, _ := os.ReadFile(filepath.Join(root, "calls"))
			lines := strings.Split(strings.TrimSpace(string(calls)), "\n")
			want := 2
			if failFirst {
				want = 3
			}
			if len(lines) != want {
				t.Fatalf("calls=%s", calls)
			}
			for _, line := range lines[1:] {
				if !strings.HasPrefix(line, "up -d --no-deps --no-build --pull never") || !strings.HasSuffix(line, " aide") {
					t.Fatal(line)
				}
			}
			if failFirst {
				raw, _ := os.ReadFile(filepath.Join(native, "active.json"))
				var c localmounts.Config
				json.Unmarshal(raw, &c)
				if localmounts.Revision(c) != localmounts.Revision(previous) {
					t.Fatal("rollback lost previous configuration")
				}
			}
		})
	}
}

func TestHelperStopChild(t *testing.T) {
	if dir := os.Getenv("AIDE_TEST_HELPER_CHILD"); dir != "" {
		release, err := acquireHelper(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go watchStop(ctx, cancel, dir)
		<-ctx.Done()
		return
	}
	dir := t.TempDir()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-test.run=^TestHelperStopChild$")
	cmd.Env = append(os.Environ(), "AIDE_TEST_HELPER_CHILD="+dir)
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, e := os.Stat(filepath.Join(dir, "serve.pid")); e == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	if err = stopHelper(dir, 3*time.Second); err != nil {
		t.Fatal(err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}

func TestApplyRejectsDesiredChangedDuringComposeQuery(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fixture")
	}
	root := t.TempDir()
	native := filepath.Join(root, ".aide-data/local-mounts")
	os.MkdirAll(native, 0700)
	previous := localmounts.Empty()
	localmounts.Write(filepath.Join(native, "active.json"), previous)
	localmounts.Write(filepath.Join(native, "compose.json"), map[string]any{"services": map[string]any{}})
	first := localmounts.Config{Version: 1, Entries: []localmounts.Entry{{ID: "docs", Name: "Confirmed", Path: t.TempDir(), ReadOnly: true}}}
	second := first
	second.Entries = append([]localmounts.Entry{}, first.Entries...)
	second.Entries[0].ReadOnly = false
	localmounts.Write(filepath.Join(root, ".aide-data/storage/data/local-mounts/desired.json"), first)
	localmounts.Write(filepath.Join(root, "next.json"), second)
	script := filepath.Join(root, "docker-compose")
	body := `#!/bin/sh
cd "$(dirname "$0")"
if [ "$1" = config ];then
 cp next.json .aide-data/storage/data/local-mounts/desired.json
 printf '%s\n' '{"services":{"aide":{"volumes":[]}}}'
else
 touch recreated
fi
`
	os.WriteFile(script, []byte(body), 0700)
	t.Setenv("AIDE_LOCAL_MOUNTS_COMPOSE", script)
	prior := confirmApplication
	confirmApplication = func(context.Context) error { return nil }
	defer func() { confirmApplication = prior }()
	if err := apply(context.Background(), root, script, localmounts.Revision(first)); err == nil {
		t.Fatal("applied unconfirmed replacement desired configuration")
	}
	active, err := localmounts.Read(filepath.Join(native, "active.json"))
	if err != nil || localmounts.Revision(active) != localmounts.Revision(previous) {
		t.Fatal("rejected revision changed active configuration", err)
	}
	if _, err = os.Stat(filepath.Join(root, "recreated")); !os.IsNotExist(err) {
		t.Fatal("recreated container for unconfirmed configuration")
	}
}

func TestApplyFreezesSnapshotWhenLaterSettingsRemainPending(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fixture")
	}
	root := t.TempDir()
	native := filepath.Join(root, ".aide-data/local-mounts")
	os.MkdirAll(native, 0700)
	previous := localmounts.Empty()
	localmounts.Write(filepath.Join(native, "active.json"), previous)
	localmounts.Write(filepath.Join(native, "compose.json"), map[string]any{"services": map[string]any{}})
	first := localmounts.Config{Version: 1, Entries: []localmounts.Entry{{ID: "docs", Name: "Confirmed", Path: t.TempDir(), ReadOnly: true}}}
	second := first
	second.Entries = append([]localmounts.Entry{}, first.Entries...)
	second.Entries[0].ReadOnly = false
	localmounts.Write(filepath.Join(root, ".aide-data/storage/data/local-mounts/desired.json"), first)
	localmounts.Write(filepath.Join(root, "next.json"), second)
	script := filepath.Join(root, "docker-compose")
	body := `#!/bin/sh
cd "$(dirname "$0")"
if [ "$1" = config ];then
  printf '%s\n' '{"services":{"aide":{"volumes":[]}}}'
else
 cp next.json .aide-data/storage/data/local-mounts/desired.json
 touch recreated
fi
`
	os.WriteFile(script, []byte(body), 0700)
	t.Setenv("AIDE_LOCAL_MOUNTS_COMPOSE", script)
	prior := confirmApplication
	confirmApplication = func(context.Context) error { return nil }
	defer func() { confirmApplication = prior }()
	if err := apply(context.Background(), root, script, localmounts.Revision(first)); err != nil {
		t.Fatal(err)
	}
	active, err := localmounts.Read(filepath.Join(native, "active.json"))
	if err != nil || localmounts.Revision(active) != localmounts.Revision(first) {
		t.Fatal("active snapshot did not preserve confirmed configuration", err)
	}
	desired, err := localmounts.Read(filepath.Join(root, ".aide-data/storage/data/local-mounts/desired.json"))
	if err != nil || localmounts.Revision(desired) != localmounts.Revision(second) {
		t.Fatal("later settings not preserved as pending", err)
	}
}
