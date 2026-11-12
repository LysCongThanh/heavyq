package dash

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// wantCSP is written out, not taken from pageCSP: an edit that loosens the
// policy must fail here.
const wantCSP = "default-src 'none'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

var wantGuardHeaders = map[string]string{
	"X-Content-Type-Options": "nosniff",
	"Referrer-Policy":        "no-referrer",
	"X-Frame-Options":        "DENY",
	"Cache-Control":          "no-store",
}

func checkHeaders(t *testing.T, what string, h http.Header, want map[string]string) {
	t.Helper()
	for k, v := range want {
		if got := h.Get(k); got != v {
			t.Errorf("%s: %s = %q, want %q", what, k, got, v)
		}
	}
}

func TestSecurityHeadersOnPageAndAPI(t *testing.T) {
	f := start(t, false)
	res, err := http.Get(f.base + "/")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("page: %d", res.StatusCode)
	}
	checkHeaders(t, "page", res.Header, wantGuardHeaders)
	checkHeaders(t, "page", res.Header, map[string]string{"Content-Security-Policy": wantCSP})
	req, _ := http.NewRequest("GET", f.base+"/api/v1/meta", nil)
	req.Header.Set("Authorization", "Bearer "+f.tok)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("meta: %d", res.StatusCode)
	}
	checkHeaders(t, "api", res.Header, wantGuardHeaders)
}

// The DNS-rebinding defence is the host name: a rebinding page reaches the
// dashboard on its real port, so every case here uses that port.
func TestForeignHostOnListenerPortRefused(t *testing.T) {
	f := start(t, true)
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(f.base, "http://"))
	n, _ := strconv.Atoi(port)
	other := strconv.Itoa(n%65535 + 1)
	for _, host := range []string{
		"attacker.example:" + port,
		"localhost.attacker.example:" + port,
		"127.0.0.1.attacker.example:" + port,
		"127.0.0.2:" + port,
		"127.0.0.1:" + other,
		"localhost:" + other,
		"127.0.0.1",
		"localhost",
	} {
		hdr := map[string]string{"Host": host, "Origin": "http://" + host}
		if code, _ := f.do("POST", "/api/v1/machines/local/pause", "", hdr); code != 421 {
			t.Errorf("Host %q: %d, want 421", host, code)
		}
	}
	if f.box.Paused("local") {
		t.Fatal("a foreign Host had an effect")
	}
	for _, host := range []string{"127.0.0.1:" + port, "localhost:" + port} {
		if code, _ := f.do("GET", "/api/v1/meta", "", map[string]string{"Host": host}); code != 200 {
			t.Errorf("Host %q: %d, want 200", host, code)
		}
	}
}

// On port 80 browsers drop the port from Host and Origin (http://127.0.0.1:80/
// is http://127.0.0.1/), so the bare loopback names must pass there.
func TestPort80AcceptsBareLoopbackHost(t *testing.T) {
	s, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0") // port 80 needs privilege: the expected port is set below
	if err != nil {
		t.Fatal(err)
	}
	s.port = "80"
	hs := &http.Server{Handler: s.Handler()}
	go func() { _ = hs.Serve(ln) }()
	t.Cleanup(func() { _ = hs.Close() })
	get := func(host string) int {
		req, _ := http.NewRequest("GET", "http://"+ln.Addr().String()+"/", nil)
		req.Host = host
		req.Header.Set("Origin", "http://"+host)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	for _, host := range []string{"127.0.0.1", "localhost", "[::1]", "127.0.0.1:80"} {
		if code := get(host); code != 200 {
			t.Errorf("port 80, Host %q: %d, want 200", host, code)
		}
	}
	for _, host := range []string{"attacker.example", "attacker.example:80", "127.0.0.1:8080"} {
		if code := get(host); code != 421 {
			t.Errorf("port 80, Host %q: %d, want 421", host, code)
		}
	}
}
