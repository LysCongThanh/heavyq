package dash

import (
	"fmt"
	"net"
)

// listenFunc opens a TCP listener: net.Listen, except in tests.
type listenFunc func(network, address string) (net.Listener, error)

// loopbackHostNames are the Host header names the dashboard answers to.
var loopbackHostNames = []string{"127.0.0.1", "localhost", "[::1]"}

// Listen opens a loopback listener on addr ("127.0.0.1:0" picks a port) and
// records the port expected in Host headers.
func (s *Server) Listen(addr string) (net.Listener, error) { return s.listen(addr, net.Listen) }

func (s *Server) listen(addr string, listenTCP listenFunc) (net.Listener, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("--addr %q: %w", addr, err)
	}
	bind := addr
	switch host {
	case "127.0.0.1", "::1":
	case "localhost":
		// Loopback by definition (RFC 6761): a hosts file or DNS search domain
		// that says otherwise must not put the dashboard on the network.
		bind = net.JoinHostPort("127.0.0.1", port)
	default:
		return nil, fmt.Errorf("--addr %q: the dashboard only listens on 127.0.0.1, ::1 or localhost", addr)
	}
	ln, err := listenTCP("tcp", bind)
	if err != nil {
		return nil, err
	}
	// Fail closed on what was actually bound, whatever the name said.
	if tcp, ok := ln.Addr().(*net.TCPAddr); !ok || !tcp.IP.IsLoopback() {
		_ = ln.Close()
		return nil, fmt.Errorf("--addr %q: bound to %s, which is not a loopback address; refused", addr, ln.Addr())
	}
	_, s.port, _ = net.SplitHostPort(ln.Addr().String())
	return ln, nil
}

// hostAllowed is the DNS-rebinding check: the Host header must name the
// loopback listener itself. On port 80 browsers send the name without the
// port (http://127.0.0.1:80/ is http://127.0.0.1/), so the bare name passes too.
func (s *Server) hostAllowed(host string) bool {
	for _, name := range loopbackHostNames {
		if host == name+":"+s.port || (s.port == "80" && host == name) {
			return true
		}
	}
	return false
}
