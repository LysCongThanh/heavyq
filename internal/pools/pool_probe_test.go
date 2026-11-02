package pools

import (
	"net"
	"os"
	"testing"
)

func TestPortBoundSeesListeners(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:0", "0.0.0.0:0", "[::1]:0"} {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			continue // e.g. no IPv6 on this host
		}
		port := ln.Addr().(*net.TCPAddr).Port
		if !PortBound(port) {
			t.Errorf("a listener on %s (port %d) was not seen", addr, port)
		}
		_ = ln.Close()
		if PortBound(port) {
			t.Errorf("port %d is free after closing %s", port, addr)
		}
	}
}

func TestParseTwoPorts(t *testing.T) {
	r, err := ParseTwoPorts("32768\t60999\n")
	if err != nil || r != (PortRange{32768, 60999}) {
		t.Fatalf("linux file: %+v %v", r, err)
	}
	if r, err := ParseTwoPorts("49152\n65535\n"); err != nil || r != (PortRange{49152, 65535}) {
		t.Fatalf("sysctl output: %+v %v", r, err)
	}
	for _, bad := range []string{"", "1", "1 2 3", "a b", "5 3", "0 10", "1 70000"} {
		if _, err := ParseTwoPorts(bad); err == nil {
			t.Errorf("ParseTwoPorts(%q) accepted", bad)
		}
	}
}

func TestRangeContains(t *testing.T) {
	r := PortRange{100, 200}
	if !r.Contains(100) || !r.Contains(200) || r.Contains(99) || r.Contains(201) {
		t.Fatal("Contains is not inclusive on both ends")
	}
}

func TestLocalEphemeralRangeIsSane(t *testing.T) {
	r := LocalEphemeralRange()
	if r.First < 1 || r.Last > 65535 || r.First > r.Last {
		t.Fatalf("range %+v", r)
	}
	if b, err := os.ReadFile("/proc/sys/net/ipv4/ip_local_port_range"); err == nil {
		want, perr := ParseTwoPorts(string(b))
		if perr != nil || r != want {
			t.Fatalf("got %+v, /proc says %+v (%v)", r, want, perr)
		}
	}
}
