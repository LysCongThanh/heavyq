package pools

import (
	"errors"
	"net"
	"strconv"
	"syscall"
)

// PortBound reports whether something holds the TCP port on the loopback
// interface, IPv4 or IPv6. It tries to listen on it, which is what the run
// will do, so it answers for wildcard binds as well. A port that cannot be
// tried (a privileged port without the right) counts as bound: a pool never
// hands out what it cannot test. A host without IPv6 is not an error.
func PortBound(port int) bool {
	p := strconv.Itoa(port)
	if listenBusy("tcp4", net.JoinHostPort("127.0.0.1", p), true) {
		return true
	}
	return listenBusy("tcp6", net.JoinHostPort("::1", p), false)
}

// listenBusy tries to listen on addr. With strict, any failure is "busy";
// otherwise only a conflict or a permission problem is (IPv6 may be absent).
func listenBusy(network, addr string, strict bool) bool {
	l, err := net.Listen(network, addr)
	if err == nil {
		_ = l.Close()
		return false
	}
	if strict {
		return true
	}
	return errors.Is(err, syscall.EADDRINUSE) || errors.Is(err, syscall.EACCES)
}
