package dash

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/LysCongThanh/heavyq/internal/control"
	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/redact"
	"github.com/LysCongThanh/heavyq/internal/snapshot"
)

const (
	maxHistory     = 1000
	insightBuckets = 12
	maxBody        = 1 << 10
)

// insightWindows are the accepted ?window= values.
var insightWindows = map[string]time.Duration{
	"1h": time.Hour, "6h": 6 * time.Hour, "24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour,
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	// Close the connection after a refusal instead of keeping it alive (and
	// first draining a request body nobody reads): a local user without the
	// token cannot hold sockets and goroutines with requests it never finishes.
	// net/http still reads up to 256 KiB of an unread body when the handler
	// ends: no more reading from this connection at all.
	w.Header().Set("Connection", "close")
	_ = http.NewResponseController(w).SetReadDeadline(time.Now())
	writeJSON(w, code, map[string]string{"error": msg})
}

// IdleTimeout is how long a keep-alive connection may wait for its next
// request (net/http has no limit unless one is set). Long enough for the
// page's polling, short enough that idle sockets do not pile up.
const IdleTimeout = time.Minute

// NewHTTPServer is the http.Server that serves h: headers within 5 s, idle
// connections closed after IdleTimeout. No ReadTimeout or WriteTimeout: they
// would cut the long-lived event stream.
func NewHTTPServer(h http.Handler) *http.Server {
	return &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: IdleTimeout}
}

func (s *Server) meta(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, Meta{Version: s.opts.Version, Host: s.opts.Host, Schema: snapshot.Schema,
		Control: s.opts.Control, IntervalMS: int(s.opts.Interval.Milliseconds())})
}

func (s *Server) snapshotNow(w http.ResponseWriter, _ *http.Request) {
	b, err := s.hub.current()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(b)
}

func (s *Server) historyList(w http.ResponseWriter, r *http.Request) {
	n := 100
	if v := r.URL.Query().Get("n"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil || p < 1 {
			writeError(w, http.StatusBadRequest, "n must be a positive number")
			return
		}
		n = min(p, maxHistory)
	}
	recs, err := s.opts.History(n)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if recs == nil {
		recs = []history.Record{}
	}
	for i := range recs {
		recs[i].Cmd = redact.Command(recs[i].Cmd)
	}
	writeJSON(w, http.StatusOK, map[string]any{"records": recs})
}

func (s *Server) insights(w http.ResponseWriter, r *http.Request) {
	win := r.URL.Query().Get("window")
	if win == "" {
		win = "1h"
	}
	d, ok := insightWindows[win]
	if !ok {
		writeError(w, http.StatusBadRequest, "window must be 1h, 6h, 24h or 7d")
		return
	}
	to := s.opts.Now()
	var recs []history.Record
	var err error
	if s.opts.HistorySince != nil {
		recs, err = s.opts.HistorySince(to.Add(-d))
	} else {
		recs, err = s.opts.History(0)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, snapshot.ComputeInsights(recs, to.Add(-d), to, insightBuckets))
}

// ticketFor validates the {id} path value and looks the ticket up.
func (s *Server) ticketFor(w http.ResponseWriter, r *http.Request) (string, bool, bool) {
	id := r.PathValue("id")
	if !control.ValidTicket(id) {
		writeError(w, http.StatusBadRequest, "invalid ticket id")
		return "", false, false
	}
	exists, running := s.opts.Ticket(id)
	if !exists {
		writeError(w, http.StatusNotFound, "no such ticket (it may have finished)")
		return "", false, false
	}
	return id, running, true
}

func (s *Server) cancel(w http.ResponseWriter, r *http.Request) {
	id, _, ok := s.ticketFor(w, r)
	if !ok {
		return
	}
	if err := s.opts.Mailbox.RequestCancel(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"ok": true})
}

func (s *Server) reprio(w http.ResponseWriter, r *http.Request) {
	id, running, ok := s.ticketFor(w, r)
	if !ok {
		return
	}
	if running {
		writeError(w, http.StatusConflict, "the ticket is already running; only waiting tickets change priority")
		return
	}
	var body struct {
		Prio int `json:"prio"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "body must be {\"prio\": 1-9}")
		return
	}
	if err := s.opts.Mailbox.RequestPrio(id, body.Prio); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"ok": true})
}

func (s *Server) pause(paused bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if !s.opts.Machine(name) {
			writeError(w, http.StatusNotFound, "no such machine")
			return
		}
		if err := s.opts.Mailbox.SetPaused(name, paused); err != nil {
			code := http.StatusInternalServerError
			if errors.Is(err, control.ErrBadName) {
				code = http.StatusBadRequest
			}
			writeError(w, code, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}
