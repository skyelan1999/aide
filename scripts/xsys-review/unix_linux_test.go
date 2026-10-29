//go:build linux

package xsysreview

import (
	"errors"
	"os"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

func TestIssue294DedupeCount(t *testing.T) {
	// An invalid fd makes accepted counts fail at the ioctl. Oversized counts
	// must fail earlier with EINVAL rather than silently truncating the ABI field.
	for _, n := range []int{65535, 65536, 65537} {
		err := unix.IoctlFileDedupeRange(-1, &unix.FileDedupeRange{Info: make([]unix.FileDedupeRangeInfo, n)})
		want := unix.EBADF
		if n > 65535 {
			want = unix.EINVAL
		}
		if !errors.Is(err, want) {
			t.Fatalf("count %d: got %v, want %v", n, err, want)
		}
	}
}

func TestIssue295CredentialsAndDestinationLengths(t *testing.T) {
	for n := 0; n < unix.SizeofUcred; n++ {
		m := &unix.SocketControlMessage{Header: unix.Cmsghdr{Level: unix.SOL_SOCKET, Type: unix.SCM_CREDENTIALS}, Data: make([]byte, n)}
		if _, err := unix.ParseUnixCredentials(m); !errors.Is(err, unix.EINVAL) {
			t.Fatalf("credential length %d: %v", n, err)
		}
	}
	encoded := unix.UnixCredentials(&unix.Ucred{Pid: 123, Uid: 456, Gid: 789})
	msgs, err := unix.ParseSocketControlMessage(encoded)
	if err != nil {
		t.Fatal(err)
	}
	cred, err := unix.ParseUnixCredentials(&msgs[0])
	if err != nil || cred.Pid != 123 || cred.Uid != 456 || cred.Gid != 789 {
		t.Fatalf("round trip: %v %v", cred, err)
	}
	for _, v := range []struct{ level, kind, size int32 }{
		{unix.SOL_IP, unix.IP_ORIGDSTADDR, unix.SizeofSockaddrInet4},
		{unix.SOL_IPV6, unix.IPV6_ORIGDSTADDR, unix.SizeofSockaddrInet6},
	} {
		for n := 0; n < int(v.size); n++ {
			m := &unix.SocketControlMessage{Header: unix.Cmsghdr{Level: v.level, Type: v.kind}, Data: make([]byte, n)}
			if _, err := unix.ParseOrigDstAddr(m); !errors.Is(err, unix.EINVAL) {
				t.Fatalf("destination %d length %d: %v", v.kind, n, err)
			}
		}
		data := make([]byte, v.size)
		data[2], data[3] = 0x12, 0x34
		m := &unix.SocketControlMessage{Header: unix.Cmsghdr{Level: v.level, Type: v.kind}, Data: data}
		addr, err := unix.ParseOrigDstAddr(m)
		if err != nil {
			t.Fatal(err)
		}
		switch a := addr.(type) {
		case *unix.SockaddrInet4:
			if a.Port != 0x1234 {
				t.Fatal(a)
			}
		case *unix.SockaddrInet6:
			if a.Port != 0x1234 {
				t.Fatal(a)
			}
		default:
			t.Fatalf("unexpected address %T", addr)
		}
	}
}

func TestIssue299ControlMessageHeader(t *testing.T) {
	for n := 0; n < unix.CmsgLen(0); n++ {
		if _, _, _, err := unix.ParseOneSocketControlMessage(make([]byte, n)); !errors.Is(err, unix.EINVAL) {
			t.Fatalf("length %d: %v", n, err)
		}
	}
	buf := make([]byte, unix.CmsgSpace(4))
	h := (*unix.Cmsghdr)(unsafe.Pointer(&buf[0]))
	h.SetLen(unix.CmsgLen(4))
	h.Level = unix.SOL_SOCKET
	h.Type = unix.SCM_RIGHTS
	if _, data, _, err := unix.ParseOneSocketControlMessage(buf); err != nil || len(data) != 4 {
		t.Fatalf("valid header: %d %v", len(data), err)
	}
}

func TestIssue296TeeByteCount(t *testing.T) {
	in, source, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	defer source.Close()
	out, destination, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	defer destination.Close()
	if _, err = source.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	n, err := unix.Tee(int(in.Fd()), int(destination.Fd()), 5, 0)
	if err != nil || n != 5 {
		t.Fatalf("tee = %d, %v", n, err)
	}
	data := make([]byte, 5)
	if _, err := out.Read(data); err != nil || string(data) != "hello" {
		t.Fatalf("destination = %q, %v", data, err)
	}
}
