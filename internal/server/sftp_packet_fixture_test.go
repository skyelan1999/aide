package server

import (
	"encoding/binary"
	"fmt"
	"github.com/pkg/sftp"
	"io"
	"net"
	"testing"
	"time"
)

// Speak SFTP over an owned in-memory pipe, without SSH credentials, devices,
// network listeners or server filesystem access. The real vendored Client
// handles handshake, request routing and reply decoding.
func issueSFTPPacketClient(t *testing.T, answer func([]byte) ([]byte, error)) *sftp.Client {
	t.Helper()
	server, conn := net.Pipe()
	_ = server.SetDeadline(time.Now().Add(15 * time.Second))
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	done := make(chan error, 1)
	go func() {
		defer server.Close()
		read := func() ([]byte, error) {
			var head [4]byte
			if _, err := io.ReadFull(server, head[:]); err != nil {
				return nil, err
			}
			n := binary.BigEndian.Uint32(head[:])
			if n < 1 || n > 1024 {
				return nil, fmt.Errorf("owned fixture request length %d", n)
			}
			data := make([]byte, n)
			_, err := io.ReadFull(server, data)
			return data, err
		}
		send := func(data []byte) error {
			packet := binary.BigEndian.AppendUint32(nil, uint32(len(data)))
			packet = append(packet, data...)
			_, err := server.Write(packet)
			return err
		}
		init, err := read()
		if err != nil {
			done <- err
			return
		}
		if len(init) != 5 || init[0] != 1 {
			done <- fmt.Errorf("unexpected INIT %x", init)
			return
		}
		if err = send([]byte{2, 0, 0, 0, 3}); err != nil {
			done <- err
			return
		}
		for {
			req, err := read()
			if err != nil {
				if err == io.EOF || err == io.ErrClosedPipe {
					err = nil
				}
				done <- err
				return
			}
			if len(req) < 5 {
				done <- fmt.Errorf("unexpected request %x", req)
				return
			}
			reply, err := answer(req)
			if err != nil {
				done <- err
				return
			}
			if err = send(reply); err != nil {
				done <- err
				return
			}
		}
	}()
	client, err := sftp.NewClientPipe(conn, conn)
	if err != nil {
		conn.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = client.Close()
		_ = conn.Close()
		_ = server.Close()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("owned SFTP server: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("owned SFTP server did not stop")
		}
	})
	return client
}
