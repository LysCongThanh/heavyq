package pools

import (
	"errors"
	"net"
	"strconv"
	"syscall"
	"time"
)

// dialTimeout bounds the connect that backs up the listen probe; a loopback
// connect answers at once, so this only matters for a filtered port.
const dialTimeout = 250 * time.Millisecond

// PortBound reports whether something holds the TCP port on the loopback
// interface, IPv4 or IPv6. It tries to listen on it, which is what the run
// will do, and then to connect to it. Listening alone misses a wildcard
// listener (0.0.0.0 or [::]) on BSD and macOS: Go sets SO_REUSEADDR, which
// there lets a second socket bind a specific address beside the wildcard one,
// where Linux answers EADDRINUSE. A listener accepts a connect, so the connect
// finds it on every platform. A port that cannot be
// tried (a privileged port without the right) counts as bound: a pool never
// hands out what it cannot test. A host without IPv6 is not an error.
func PortBound(port int) bool {
	p := strconv.Itoa(port)
	if listenBusy("tcp4", net.JoinHostPort("127.0.0.1", p), true) {
		return true
	}
	if listenBusy("tcp6", net.JoinHostPort("::1", p), false) {
		return true
	}
	return accepts("127.0.0.1", p) || accepts("::1", p)
}

// accepts is true when something takes a TCP connection on host:port. A host
// without that address, or a refusal, is "nothing there".
func accepts(host, port string) bool {
	c, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), dialTimeout)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
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
