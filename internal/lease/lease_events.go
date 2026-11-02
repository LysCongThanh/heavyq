package lease

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Event ops written to events.jsonl.
const (
	opClaim   = "claim"
	opRenew   = "renew"
	opRelease = "release"
	opReclaim = "reclaim"
)

// Event is one line of events.jsonl. Reclaim events name the previous holder
// and why the lease was stale (expired, dead, corrupt); release events carry
// forced when the releaser was not the holder.
type Event struct {
	At          time.Time `json:"at"`
	Op          string    `json:"op"`
	Item        string    `json:"item"`
	Lane        string    `json:"lane"`
	Session     string    `json:"session"`
	Forced      bool      `json:"forced,omitempty"`
	Reason      string    `json:"reason,omitempty"`
	PrevLane    string    `json:"prev_lane,omitempty"`
	PrevSession string    `json:"prev_session,omitempty"`
}

// appendEvent writes one line with a single O_APPEND write, so concurrent
// appenders (even without the store lock) never interleave within a line.
func (s *Store) appendEvent(e Event) error {
	e.At = e.At.UTC()
	b, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrEventLog, err)
	}
	f, err := os.OpenFile(s.eventsPath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrEventLog, err)
	}
	_, werr := f.Write(append(b, '\n'))
	cerr := f.Close()
	if werr != nil {
		return fmt.Errorf("%w: %v", ErrEventLog, werr)
	}
	if cerr != nil {
		return fmt.Errorf("%w: %v", ErrEventLog, cerr)
	}
	return nil
}
