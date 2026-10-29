package server

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/pkg/sftp"
)

func issue163Client(t *testing.T, attrs []byte) *sftp.Client {
	t.Helper()
	return issueSFTPPacketClient(t, func(req []byte) ([]byte, error) {
		if req[0] != 7 {
			return nil, fmt.Errorf("unexpected Lstat fixture request %x", req)
		}
		reply := append([]byte{105}, req[1:5]...)
		return append(reply, attrs...), nil
	})
}

func issue163Attrs(count uint32, entries []byte) []byte {
	b := binary.BigEndian.AppendUint32(nil, 0x80000000)
	b = binary.BigEndian.AppendUint32(b, count)
	return append(b, entries...)
}

func TestIssue163SFTPCountCannotAllocateWithoutPayload(t *testing.T) {
	// The old decoder allocates 32 MiB for this 8-byte attribute payload, then
	// reports truncation. Keep the reproduction finite; do not send UINT32_MAX
	// until the bounded-allocation guard has passed.
	client := issue163Client(t, issue163Attrs(1<<20, nil))
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	info, err := client.Lstat("owned-fixture")
	runtime.ReadMemStats(&after)
	if err == nil || info != nil {
		t.Fatalf("malformed attributes accepted: %v %v", info, err)
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 8<<20 {
		t.Fatalf("tiny SFTP packet caused %d bytes of allocations before rejecting its count", allocated)
	}
	if !strings.Contains(err.Error(), "short") {
		t.Fatalf("wrong malformed packet error: %v", err)
	}
	// This runs only after the finite allocation guard above passed.
	maxClient := issue163Client(t, issue163Attrs(^uint32(0), nil))
	if info, err := maxClient.Lstat("owned-max-count"); err == nil || info != nil {
		t.Fatalf("maximal count accepted: %v %v", info, err)
	}
	// A rejected reply does not poison request routing on the same connection.
	if info, err := client.Lstat("owned-reuse"); err == nil || info != nil {
		t.Fatal("subsequent malformed request was accepted")
	}
}

func TestIssue163SFTPExtendedAttributeBoundaries(t *testing.T) {
	// Only moderate counts here, so independently selecting this test remains
	// safe against the unfixed decoder too. The maximal count is exercised in
	// the guarded test above after its first allocation check succeeds.
	entry := func(typ, data string) []byte {
		b := binary.BigEndian.AppendUint32(nil, uint32(len(typ)))
		b = append(b, typ...)
		b = binary.BigEndian.AppendUint32(b, uint32(len(data)))
		return append(b, data...)
	}
	cases := []struct {
		name  string
		count uint32
		data  []byte
		valid bool
	}{
		{"zero", 0, nil, true},
		{"one-empty-pair", 1, make([]byte, 8), true},
		{"two-pairs", 2, append(entry("name", "value"), entry("", "")...), true},
		{"missing-count-data", 1, nil, false},
		{"short-minimum-pair", 1, make([]byte, 7), false},
		{"count-exceeds-payload", 2, entry("", ""), false},
		{"string-length-exceeds-payload", 1, []byte{0xff, 0xff, 0xff, 0xff, 0, 0, 0, 0}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := issue163Client(t, issue163Attrs(tc.count, tc.data))
			info, err := client.Lstat("owned-fixture")
			if tc.valid {
				if err != nil || info == nil {
					t.Fatalf("valid attributes rejected: %v", err)
				}
				fs, ok := info.Sys().(*sftp.FileStat)
				if !ok || len(fs.Extended) != int(tc.count) {
					t.Fatalf("lost extended attributes: %#v", info.Sys())
				}
				if tc.name == "two-pairs" && (fs.Extended[0].ExtType != "name" || fs.Extended[0].ExtData != "value") {
					t.Fatal("changed decoded extended attributes")
				}
			} else if err == nil || info != nil {
				t.Fatalf("malformed attributes accepted: %#v %v", info, err)
			}
		})
	}
}
