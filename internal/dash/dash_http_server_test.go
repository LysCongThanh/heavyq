package dash

import (
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

// The dashboard's server closes idle keep-alive connections (net/http keeps
// them forever without IdleTimeout) but never cuts a request in progress: the
// live stream is one long response.
func TestHTTPServerClosesIdleConnectionsOnly(t *testing.T) {
	hs := NewHTTPServer(http.NotFoundHandler())
	if hs.IdleTimeout <= 0 || hs.ReadHeaderTimeout <= 0 || hs.ReadTimeout != 0 || hs.WriteTimeout != 0 {
		t.Fatalf("timeouts: idle %s, header %s, read %s, write %s", hs.IdleTimeout, hs.ReadHeaderTimeout, hs.ReadTimeout, hs.WriteTimeout)
	}
	hs.IdleTimeout = 200 * time.Millisecond // the same server, a test-sized wait
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = hs.Serve(ln) }()
	t.Cleanup(func() { _ = hs.Close() })
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	if _, err := io.WriteString(c, "GET /x HTTP/1.1\r\nHost: x\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.Copy(io.Discard, c); err != nil {
		t.Fatalf("an idle connection stays open: %v", err)
	}
}
