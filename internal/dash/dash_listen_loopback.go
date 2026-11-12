package dash

import (
	"fmt"
	"net"
)

// listenFunc opens a TCP listener: net.Listen, except in tests.
type listenFunc func(network, address string) (net.Listener, error)

// Listen opens a loopback listener on addr ("127.0.0.1:0" picks a port) and
// records the port expected in Host headers.
func (s *Server) Listen(addr string) (net.Listener, error) { return s.listen(addr, net.Listen) }

func (s *Server) listen(addr string, listenTCP listenFunc) (net.Listener, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("--addr %q: %w", addr, err)
	}
	if host != "127.0.0.1" && host != "::1" && host != "localhost" {
		return nil, fmt.Errorf("--addr %q: the dashboard only listens on 127.0.0.1, ::1 or localhost", addr)
	}
	ln, err := listenTCP("tcp", addr)
	if err != nil {
		return nil, err
	}
	_, s.port, _ = net.SplitHostPort(ln.Addr().String())
	return ln, nil
}

// hostAllowed is the DNS-rebinding check: the Host header must name the
// loopback listener itself.
func (s *Server) hostAllowed(host string) bool {
	for _, name := range []string{"127.0.0.1", "localhost", "[::1]"} {
		if host == name+":"+s.port {
			return true
		}
	}
	return false
}
