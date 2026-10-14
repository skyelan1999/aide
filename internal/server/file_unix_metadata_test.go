package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
)

func TestFileUnixPermissions(t *testing.T) {
	for _, item := range []struct {
		mode os.FileMode
		want string
	}{{0640, "-rw-r-----"}, {0755 | os.ModeDir, "drwxr-xr-x"}, {0755 | os.ModeSetuid, "-rwsr-xr-x"}, {0640 | os.ModeSetgid, "-rw-r-S---"}, {0777 | os.ModeSticky | os.ModeDir, "drwxrwxrwt"}, {0666 | os.ModeSticky, "-rw-rw-rwT"}} {
		if got := fileUnixPermissions(item.mode); got != item.want {
			t.Fatalf("%v: got %s want %s", item.mode, got, item.want)
		}
	}
}
func TestFileUnixLocalListing(t *testing.T) {
	a := testApp(t)
	p := filepath.Join(a.workspace.Name(), "owner.txt")
	if err := os.WriteFile(p, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(a.workspace.Name(), "folder"), 0700); err != nil {
		t.Fatal(err)
	}
	w := request(a, "GET", "/api/files?root=workspace&path=.", nil)
	requireStatus(t, w, 200)
	var entries []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, entry := range entries {
		if entry["name"] != "owner.txt" {
			continue
		}
		found = true
		if runtime.GOOS != "linux" {
			if entry["unix"] != nil {
				t.Fatal("non-Linux local listing fabricated Linux data")
			}
			continue
		}
		meta, ok := entry["unix"].(map[string]any)
		if !ok {
			t.Fatal(entry)
		}
		if meta["permissions"] != "-rw-r-----" || meta["uid"] != strconv.Itoa(os.Geteuid()) || meta["gid"] != strconv.Itoa(os.Getegid()) || meta["owner"] == "" || meta["group"] == "" {
			t.Fatal(meta)
		}
	}
	if !found {
		t.Fatal("fixture missing")
	}
}
func TestFileUnixRemoteReported(t *testing.T) {
	out := "-rwsr-xr--+ 1 alice research 42 Oct 14 12:00 report.txt\n" + "drwxrwxr-t 1 1001 1002 0 Oct 14 12:00 shared\n" + "-rw 1 unknown group 1 Oct 14 12:00 legacy.txt\n"
	entries := parseSFTPList(out, ".")
	if len(entries) != 3 {
		t.Fatal(entries)
	}
	first := entries[0]["unix"].(*fileUnixMetadata)
	if first.Owner != "alice" || first.Group != "research" || first.Permissions != "-rwsr-xr--+" {
		t.Fatal(first)
	}
	second := entries[1]["unix"].(*fileUnixMetadata)
	if second.Owner != "1001" || second.Group != "1002" || second.Permissions != "drwxrwxr-t" {
		t.Fatal(second)
	}
	if entries[2]["unix"] != nil {
		t.Fatal("invalid remote mode was synthesized")
	}
}
