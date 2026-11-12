package hostbridge

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fixtureRoot(t *testing.T, ro bool) *rootFS {
	t.Helper()
	r, e := os.OpenRoot(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	f := &rootFS{root: r, ro: ro, uid: 1000, gid: 1000, files: map[*rootFile]bool{}}
	t.Cleanup(f.close)
	return f
}
func TestRootObjectSurvivesPathAndParentReplacement(t *testing.T) {
	for _, parent := range []bool{false, true} {
		t.Run(map[bool]string{false: "directory", true: "parent"}[parent], func(t *testing.T) {
			base := t.TempDir()
			selected := filepath.Join(base, "parent", "selected")
			outside := filepath.Join(base, "outside")
			os.MkdirAll(selected, 0700)
			os.Mkdir(outside, 0700)
			os.WriteFile(filepath.Join(selected, "marker"), []byte("original"), 0600)
			os.WriteFile(filepath.Join(outside, "marker"), []byte("outside"), 0600)
			root, e := os.OpenRoot(selected)
			if e != nil {
				t.Fatal(e)
			}
			f := &rootFS{root: root, uid: 1000, gid: 1000, files: map[*rootFile]bool{}}
			defer f.close()
			before, e := Identity(root)
			if e != nil {
				t.Fatal(e)
			}
			if parent {
				if e = os.Rename(filepath.Join(base, "parent"), filepath.Join(base, "held-parent")); e != nil {
					t.Fatal("could not perform parent replacement fixture", e)
				}
				if e = os.Symlink(outside, filepath.Join(base, "parent")); e != nil {
					t.Fatal("could not install replacement parent", e)
				}
			} else {
				if e = os.Rename(selected, filepath.Join(base, "held")); e != nil {
					t.Fatal("could not perform directory replacement fixture", e)
				}
				if e = os.Symlink(outside, selected); e != nil {
					t.Fatal("could not install replacement directory", e)
				}
			}
			input, e := f.Open("marker")
			if e != nil {
				t.Fatal(e)
			}
			b, e := io.ReadAll(input)
			input.Close()
			if e != nil || string(b) != "original" {
				t.Fatal("path substitution escaped the retained object", string(b), e)
			}
			out, e := f.Create("new.txt")
			if e != nil {
				t.Fatal(e)
			}
			if _, e = out.Write([]byte("written")); e != nil {
				t.Fatal(e)
			}
			out.Close()
			if _, e = os.Stat(filepath.Join(outside, "new.txt")); !os.IsNotExist(e) {
				t.Fatal("outside object was written")
			}
			if after, e := Identity(root); e != nil || after != before {
				t.Fatal("opened identity changed", e)
			}
		})
	}
}
func TestRootReadOnlyAndTraversal(t *testing.T) {
	f := fixtureRoot(t, true)
	f.root.WriteFile("inside", []byte("read"), 0600)
	for _, p := range []string{"../escape", "/../escape", "x\\y"} {
		if _, e := f.Open(p); e == nil {
			t.Fatal("traversal", p)
		}
	}
	if _, e := f.Create("new"); e == nil {
		t.Fatal("readonly create")
	}
	if e := f.Rename("inside", "moved"); e == nil {
		t.Fatal("readonly rename")
	}
	if e := f.Remove("inside"); e == nil {
		t.Fatal("readonly remove")
	}
	if e := f.Chmod("inside", 0777); e == nil {
		t.Fatal("readonly chmod")
	}
	if e := f.MkdirAll("new-dir", 0700); e == nil {
		t.Fatal("readonly mkdir")
	}
	outside := filepath.Join(t.TempDir(), "private")
	os.WriteFile(outside, []byte("outside"), 0600)
	if e := f.root.Symlink(outside, "link"); e != nil {
		t.Fatal("outside symlink fixture was not created", e)
	}
	if _, e := f.Open("link"); e == nil {
		t.Fatal("followed outside symlink")
	}
}
func TestBridgeMalformedRPCIsBoundedAndExportRevokes(t *testing.T) {
	server, e := New(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	defer server.Close()
	root, e := os.OpenRoot(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	token, e := server.Export(root, false, 1000, 1000)
	if e != nil {
		t.Fatal(e)
	}
	address := net.JoinHostPort("127.0.0.1", fmtInt(server.Port()))
	// Header claims an oversized record before any allocation/opaque decode.
	conn, e := net.DialTimeout("tcp", address, time.Second)
	if e != nil {
		t.Fatal(e)
	}
	conn.SetDeadline(time.Now().Add(time.Second))
	var word [4]byte
	binary.BigEndian.PutUint32(word[:], 0x80400000)
	conn.Write(word[:])
	var b [64]byte
	if _, e = conn.Read(b[:]); e == nil {
		t.Fatal("oversized RPC record accepted")
	}
	conn.Close()
	// A small record containing a forged enormous opaque length is also refused.
	conn, e = net.DialTimeout("tcp", address, time.Second)
	if e != nil {
		t.Fatal(e)
	}
	conn.SetDeadline(time.Now().Add(time.Second))
	fields := []uint32{1, 0, 2, 100005, 3, 1, 0, 0, 0, 0, 0x7fffffff}
	packet := make([]byte, 4+len(fields)*4)
	binary.BigEndian.PutUint32(packet, uint32(len(fields)*4)|0x80000000)
	for i, v := range fields {
		binary.BigEndian.PutUint32(packet[4+i*4:], v)
	}
	conn.Write(packet)
	_, _ = conn.Read(b[:])
	conn.Close()
	server.mu.RLock()
	f := server.exports[token]
	server.mu.RUnlock()
	if _, e = f.Stat("."); e != nil {
		t.Fatal("valid export lost after malformed RPC", e)
	}
	server.Release(token)
	if _, e = f.Open("."); e == nil {
		t.Fatal("revoked export remained usable")
	}
}
func fmtInt(v int) string {
	const digits = "0123456789"
	if v == 0 {
		return "0"
	}
	b := make([]byte, 0, 8)
	for v > 0 {
		b = append([]byte{digits[v%10]}, b...)
		v /= 10
	}
	return string(b)
}
