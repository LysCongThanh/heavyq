package cli

import (
	"bufio"
	"io"
	"net"
	"regexp"
	"strings"
	"testing"
	"time"
)

// dashAddr starts `heavyq dash` and returns its host:port.
func (h *rig) dashAddr() string {
	h.t.Helper()
	p := h.start("dash")
	re := regexp.MustCompile(`http://([0-9.:]+)/`)
	var addr string
	h.until("the dashboard address", func() bool {
		m := re.FindStringSubmatch(p.out.String())
		if m != nil {
			addr = m[1]
		}
		return addr != ""
	})
	return addr
}

// A request the dashboard refuses (no token) is answered at once and the
// connection closed: a local user without the token cannot hold sockets and
// goroutines by announcing a body and never sending it.
func TestE2EDashRefusalClosesTheConnection(t *testing.T) {
	h := newRig(t, baseConfig)
	addr := h.dashAddr()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	req := "POST /api/v1/tickets/x/cancel HTTP/1.1\r\nHost: " + addr + "\r\nContent-Type: application/json\r\nContent-Length: 100000\r\n\r\n{"
	if _, err := io.WriteString(c, req); err != nil {
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	r := bufio.NewReader(c)
	status, err := r.ReadString('\n')
	if err != nil || !strings.Contains(status, " 401 ") {
		t.Fatalf("no prompt refusal: %q %v", status, err)
	}
	if _, err := io.Copy(io.Discard, r); err != nil {
		t.Fatalf("the connection stays open after the refusal: %v", err)
	}
}
