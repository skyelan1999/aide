package server

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/pkg/sftp"
)

func issue164String(value string) []byte {
	b := binary.BigEndian.AppendUint32(nil, uint32(len(value)))
	return append(b, value...)
}
func issue164Entry(name string) []byte {
	b := issue164String(name)
	b = append(b, issue164String("owned longname")...)
	b = binary.BigEndian.AppendUint32(b, 1|4) // size and POSIX permissions
	b = binary.BigEndian.AppendUint64(b, 23)
	return binary.BigEndian.AppendUint32(b, 0100644)
}
func issue164Names(count uint32, entries []byte) []byte {
	return append(binary.BigEndian.AppendUint32(nil, count), entries...)
}
func issue164Client(t *testing.T, body []byte) *sftp.Client {
	t.Helper()
	read := false
	return issueSFTPPacketClient(t, func(req []byte) ([]byte, error) {
		reply := append([]byte{104}, req[1:5]...)
		status := func(code uint32) []byte {
			data := binary.BigEndian.AppendUint32(nil, code)
			data = append(data, issue164String("")...)
			data = append(data, issue164String("")...)
			return append(append([]byte{101}, req[1:5]...), data...)
		}
		switch req[0] {
		case 11: // OPENDIR
			return append(append([]byte{102}, req[1:5]...), issue164String("owned-handle")...), nil
		case 4:
			return status(0), nil // CLOSE
		case 12: // READDIR: one reply followed by EOF
			if read {
				return status(1), nil
			}
			read = true
		case 16, 19: // REALPATH / READLINK
		default:
			return nil, fmt.Errorf("unexpected NAME fixture request %x", req)
		}
		return append(reply, body...), nil
	})
}

func TestIssue164MalformedNamesReturnErrorsWithoutPanic(t *testing.T) {
	file := issue164String("owned")
	noLongname := append(append([]byte{}, file...), 0, 0, 0)
	noAttrs := append(append([]byte{}, file...), issue164String("longname")...)
	badAttrs := append(append([]byte{}, noAttrs...), 0, 0, 0, 1) // missing size bytes
	cases := []struct {
		name string
		body []byte
	}{
		{"missing-count", nil},
		{"short-count", []byte{0, 0, 0}},
		{"missing-filename-length", issue164Names(1, nil)},
		{"huge-filename-length", issue164Names(1, []byte{0xff, 0xff, 0xff, 0xff})},
		{"truncated-filename", issue164Names(1, []byte{0, 0, 0, 10, 'x'})},
		{"truncated-longname-length", issue164Names(1, noLongname)},
		{"truncated-longname", issue164Names(1, append(append([]byte{}, file...), 0, 0, 0, 10, 'x'))},
		{"missing-attributes", issue164Names(1, noAttrs)},
		{"truncated-size", issue164Names(1, badAttrs)},
		{"overstated-count", issue164Names(2, issue164Entry("owned"))},
		{"maximal-count", issue164Names(^uint32(0), nil)},
	}
	for _, operation := range []string{"ReadDir", "ReadDirEach", "RealPath", "ReadLink"} {
		for _, tc := range cases {
			t.Run(operation+"/"+tc.name, func(t *testing.T) {
				defer func() {
					if p := recover(); p != nil {
						t.Errorf("malformed NAME response panicked in %s: %v", operation, p)
					}
				}()
				client := issue164Client(t, tc.body)
				var err error
				switch operation {
				case "ReadDir":
					_, err = client.ReadDir("owned")
				case "ReadDirEach":
					err = client.ReadDirEachContext(context.Background(), "owned", func(os.FileInfo) error { return nil })
				case "RealPath":
					_, err = client.RealPath("owned")
				case "ReadLink":
					_, err = client.ReadLink("owned")
				}
				if err == nil {
					t.Fatal("malformed NAME response accepted")
				}
			})
		}
	}
}

func TestIssue164ValidNamesKeepAttributesAndVisitorSemantics(t *testing.T) {
	for _, operation := range []string{"RealPath", "ReadLink"} {
		t.Run(operation, func(t *testing.T) {
			client := issue164Client(t, issue164Names(1, issue164Entry("/owned/target")))
			var value string
			var err error
			if operation == "RealPath" {
				value, err = client.RealPath("owned")
			} else {
				value, err = client.ReadLink("owned")
			}
			if err != nil || value != "/owned/target" {
				t.Fatalf("valid NAME rejected: %q %v", value, err)
			}
		})
	}
	entries := append(issue164Entry("."), issue164Entry("..")...)
	entries = append(entries, issue164Entry("child")...)
	client := issue164Client(t, issue164Names(3, entries))
	files, err := client.ReadDir("owned")
	if err != nil || len(files) != 1 || files[0].Name() != "child" || files[0].Size() != 23 || files[0].Mode().Perm() != 0644 {
		t.Fatalf("directory attrs or dot filtering changed: %#v %v", files, err)
	}
	for _, cancel := range []bool{false, true} {
		t.Run(fmt.Sprintf("visitor-cancel=%v", cancel), func(t *testing.T) {
			ctx, stop := context.WithCancel(context.Background())
			defer stop()
			client := issue164Client(t, issue164Names(3, entries))
			visited := 0
			owned := errors.New("owned visitor stop")
			err := client.ReadDirEachContext(ctx, "owned", func(os.FileInfo) error {
				visited++
				if cancel {
					stop()
					return nil
				}
				return owned
			})
			want := owned
			if cancel {
				want = context.Canceled
			}
			if !errors.Is(err, want) || visited != 1 {
				t.Fatalf("lost visitor boundary: visited=%d err=%v", visited, err)
			}
		})
	}
	zero := issue164Client(t, issue164Names(0, nil))
	if files, err := zero.ReadDir("owned"); err != nil || len(files) != 0 {
		t.Fatalf("empty listing changed: %v %v", files, err)
	}
}

func TestIssue164RoutingIDsAlreadyFailClosed(t *testing.T) {
	for _, payload := range [][]byte{nil, {0, 0, 0}, {0xff, 0xff, 0xff, 0xff}} {
		t.Run(fmt.Sprintf("id=%x", payload), func(t *testing.T) {
			client := issueSFTPPacketClient(t, func(req []byte) ([]byte, error) { return append([]byte{104}, payload...), nil })
			if value, err := client.RealPath("owned"); err == nil || value != "" {
				t.Fatalf("invalid routing ID accepted: %q %v", value, err)
			}
		})
	}
}
