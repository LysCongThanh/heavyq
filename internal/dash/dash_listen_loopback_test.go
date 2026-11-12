package dash

import (
	"errors"
	"net"
	"testing"
)

// A resolver (hosts file, DNS search domain) that answers "localhost" with a
// LAN address must not put the dashboard on the network: what the listener is
// bound to decides, not the name in --addr.
func TestListenRefusesNonLoopbackBind(t *testing.T) {
	var bound net.Listener
	s := &Server{}
	ln, err := s.listen("localhost:0", func(network, _ string) (net.Listener, error) {
		l, err := net.Listen(network, "0.0.0.0:0") // what a broken resolver would give
		bound = l
		return l, err
	})
	if err == nil {
		_ = ln.Close()
		t.Fatalf("listener on %s accepted as loopback", ln.Addr())
	}
	if bound == nil {
		t.Fatalf("listen func never called: %v", err)
	}
	if cerr := bound.Close(); !errors.Is(cerr, net.ErrClosed) {
		t.Fatalf("refused listener left open (close: %v)", cerr)
	}
}

// "localhost" is the loopback address by definition (RFC 6761): it is never
// handed to the resolver.
func TestListenLocalhostNeverAsksResolver(t *testing.T) {
	var asked string
	s := &Server{}
	ln, err := s.listen("localhost:0", func(network, address string) (net.Listener, error) {
		asked = address
		return net.Listen(network, address)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	if asked != "127.0.0.1:0" {
		t.Fatalf("listened on %q, want 127.0.0.1:0", asked)
	}
	if ip := ln.Addr().(*net.TCPAddr).IP; !ip.IsLoopback() {
		t.Fatalf("bound to %s", ip)
	}
}
