package server

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
)

func TestIssue617VisitorCancellationStopsBeforeNextReadDir(t *testing.T) {
	for _, visitorError := range []bool{false, true} {
		t.Run(fmt.Sprintf("visitor-error=%v", visitorError), func(t *testing.T) {
			var reads, closes atomic.Int32
			client := issueSFTPPacketClient(t, func(req []byte) ([]byte, error) {
				reply := append([]byte{}, req[1:5]...)
				status := func(code uint32) []byte {
					body := binary.BigEndian.AppendUint32(nil, code)
					body = append(body, issue164String("")...)
					body = append(body, issue164String("")...)
					return append(append([]byte{101}, reply...), body...)
				}
				switch req[0] {
				case 11:
					return append(append([]byte{102}, reply...), issue164String("owned")...), nil
				case 12:
					if reads.Add(1) == 1 {
						return append(append([]byte{104}, reply...), issue164Names(1, issue164Entry("last"))...), nil
					}
					return status(1), nil
				case 4:
					closes.Add(1)
					return status(0), nil
				}
				return nil, fmt.Errorf("unexpected owned request %d", req[0])
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			owned := errors.New("owned visitor failure")
			err := client.ReadDirEachContext(ctx, "owned", func(os.FileInfo) error {
				cancel()
				if visitorError {
					return owned
				}
				return nil
			})
			want := error(context.Canceled)
			if visitorError {
				want = owned
			}
			if !errors.Is(err, want) || reads.Load() != 1 || closes.Load() != 1 {
				t.Fatalf("cancellation boundary: err=%v reads=%d closes=%d", err, reads.Load(), closes.Load())
			}
		})
	}
}
