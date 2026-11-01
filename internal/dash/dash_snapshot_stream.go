package dash

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/LysCongThanh/heavyq/internal/snapshot"
)

// heartbeatEvery keeps idle proxies and browsers from closing the stream.
const heartbeatEvery = 15 * time.Second

// hub produces snapshots while at least one stream is open and fans them out.
// One producer serves every client, so N open tabs cost one CPU sample.
type hub struct {
	build    func() (snapshot.Snapshot, error)
	interval time.Duration

	mu      sync.Mutex
	subs    map[chan []byte]struct{}
	latest  []byte
	at      time.Time
	running bool
	once    sync.Mutex // serialises on-demand builds
}

func newHub(build func() (snapshot.Snapshot, error), interval time.Duration) *hub {
	return &hub{build: build, interval: interval, subs: map[chan []byte]struct{}{}}
}

// current returns a snapshot at most one interval old, building one if needed.
func (h *hub) current() ([]byte, error) {
	h.once.Lock()
	defer h.once.Unlock()
	h.mu.Lock()
	if h.latest != nil && time.Since(h.at) < h.interval {
		b := h.latest
		h.mu.Unlock()
		return b, nil
	}
	h.mu.Unlock()
	return h.refresh()
}

func (h *hub) refresh() ([]byte, error) {
	s, err := h.build()
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	h.mu.Lock()
	h.latest, h.at = b, time.Now()
	for c := range h.subs {
		select {
		case c <- b:
		default: // slow client: it gets the next one
		}
	}
	h.mu.Unlock()
	return b, nil
}

func (h *hub) subscribe() chan []byte {
	c := make(chan []byte, 1)
	h.mu.Lock()
	h.subs[c] = struct{}{}
	start := !h.running
	h.running = true
	h.mu.Unlock()
	if start {
		go h.produce()
	}
	return c
}

func (h *hub) unsubscribe(c chan []byte) {
	h.mu.Lock()
	delete(h.subs, c)
	h.mu.Unlock()
}

// produce refreshes every interval until nobody listens.
func (h *hub) produce() {
	for {
		h.once.Lock()
		_, _ = h.refresh()
		h.once.Unlock()
		time.Sleep(h.interval)
		h.mu.Lock()
		if len(h.subs) == 0 {
			h.running = false
			h.mu.Unlock()
			return
		}
		h.mu.Unlock()
	}
}

// stream sends `event: snapshot` messages until the client goes away.
func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	c := s.hub.subscribe()
	defer s.hub.unsubscribe(c)
	if b, err := s.hub.current(); err == nil {
		fmt.Fprintf(w, "event: snapshot\ndata: %s\n\n", b)
	}
	fl.Flush()
	beat := time.NewTicker(heartbeatEvery)
	defer beat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case b := <-c:
			fmt.Fprintf(w, "event: snapshot\ndata: %s\n\n", b)
			fl.Flush()
		case <-beat.C:
			fmt.Fprint(w, ": keep-alive\n\n")
			fl.Flush()
		}
	}
}
