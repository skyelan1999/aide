//go:build !windows

package messaging

import (
	"net"
	"syscall"
)

func setDiscoveryInterface(conn *net.UDPConn, ip net.IP) error {
	raw, e := conn.SyscallConn()
	if e != nil {
		return e
	}
	var result error
	e = raw.Control(func(fd uintptr) {
		var addr [4]byte
		copy(addr[:], ip.To4())
		result = syscall.SetsockoptInet4Addr(int(fd), syscall.IPPROTO_IP, syscall.IP_MULTICAST_IF, addr)
	})
	if e != nil {
		return e
	}
	return result
}
