// Package dash serves the heavyq dashboard: an embedded single-page app and
// a small JSON/SSE API over the queue snapshot, history and control mailbox.
//
// Security model: the server listens on loopback only, checks the Host header
// (DNS rebinding) and the Origin of state-changing requests, and requires a
// random per-start bearer token for every API call. It is read-only unless
// started with control enabled.
package dash

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/LysCongThanh/heavyq/internal/control"
	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/snapshot"
)

//go:embed web/index.html
var webFS embed.FS

// Meta is served at /api/v1/meta.
type Meta struct {
	Version    string `json:"version"`
	Host       string `json:"host"`
	Schema     int    `json:"schema"`
	Control    bool   `json:"control"`
	IntervalMS int    `json:"interval_ms"`
}

// Options wires the server to heavyq's state.
type Options struct {
	Control  bool          // allow cancel / reprio / pause
	Interval time.Duration // snapshot period of the stream
	Version  string
	Host     string
	// Snapshot builds the current state (takes about a CPU sample interval).
	Snapshot func() (snapshot.Snapshot, error)
	// History returns up to n most recent records (n <= 0: all), oldest first.
	History func(n int) ([]history.Record, error)
	// HistorySince returns the records that ended at or after since, oldest
	// first, reading no further back than that (the insights windows). Nil: the
	// insights read History(0) and filter.
	HistorySince func(since time.Time) ([]history.Record, error)
	Mailbox      control.Mailbox
	// Ticket reports whether ticket id exists and whether it is running.
	Ticket func(id string) (exists, running bool)
	// Machine reports whether a machine of that name is configured.
	Machine func(name string) bool
	Now     func() time.Time // for tests; time.Now when nil
}

// Server is one dashboard instance.
type Server struct {
	opts  Options
	token string
	port  string // expected port in the Host header
	hub   *hub
}

// New creates a server with a fresh random token.
func New(opts Options) (*Server, error) {
	if opts.Interval <= 0 {
		opts.Interval = 2 * time.Second
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("dashboard token: %w", err)
	}
	s := &Server{opts: opts, token: hex.EncodeToString(b)}
	s.hub = newHub(opts.Snapshot, opts.Interval)
	return s, nil
}

// Token is the bearer token API calls must present.
func (s *Server) Token() string { return s.token }

// Listen opens a loopback listener on addr ("127.0.0.1:0" picks a port) and
// records the port expected in Host headers.
func (s *Server) Listen(addr string) (net.Listener, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("--addr %q: %w", addr, err)
	}
	if host != "127.0.0.1" && host != "::1" && host != "localhost" {
		return nil, fmt.Errorf("--addr %q: the dashboard only listens on 127.0.0.1, ::1 or localhost", addr)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	_, s.port, _ = net.SplitHostPort(ln.Addr().String())
	return ln, nil
}

// Handler routes the page and the API.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.page)
	mux.HandleFunc("GET /api/v1/meta", s.api(s.meta))
	mux.HandleFunc("GET /api/v1/snapshot", s.api(s.snapshotNow))
	mux.HandleFunc("GET /api/v1/stream", s.api(s.stream))
	mux.HandleFunc("GET /api/v1/history", s.api(s.historyList))
	mux.HandleFunc("GET /api/v1/insights", s.api(s.insights))
	mux.HandleFunc("POST /api/v1/tickets/{id}/cancel", s.api(s.control(s.cancel)))
	mux.HandleFunc("POST /api/v1/tickets/{id}/prio", s.api(s.control(s.reprio)))
	mux.HandleFunc("POST /api/v1/machines/{name}/pause", s.api(s.control(s.pause(true))))
	mux.HandleFunc("POST /api/v1/machines/{name}/resume", s.api(s.control(s.pause(false))))
	return s.guard(mux)
}

// guard applies headers and the DNS-rebinding / cross-origin checks.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Cache-Control", "no-store")
		if !s.hostAllowed(r.Host) {
			writeError(w, http.StatusMisdirectedRequest, "unexpected Host header")
			return
		}
		if o := r.Header.Get("Origin"); o != "" && o != "http://"+r.Host {
			writeError(w, http.StatusForbidden, "cross-origin request refused")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) hostAllowed(host string) bool {
	for _, name := range []string{"127.0.0.1", "localhost", "[::1]"} {
		if host == name+":"+s.port {
			return true
		}
	}
	return false
}

// api requires the bearer token.
func (s *Server) api(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
			writeError(w, http.StatusUnauthorized, "missing or wrong token: open the link printed by heavyq dash")
			return
		}
		h(w, r)
	}
}

// control refuses state changes unless the server runs with --control.
func (s *Server) control(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.opts.Control {
			writeError(w, http.StatusForbidden, "read-only dashboard: restart it with heavyq dash --control")
			return
		}
		h(w, r)
	}
}

const pageCSP = "default-src 'none'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

func (s *Server) page(w http.ResponseWriter, _ *http.Request) {
	b, err := webFS.ReadFile("web/index.html")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Security-Policy", pageCSP)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(b)
}
