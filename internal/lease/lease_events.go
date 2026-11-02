package lease

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Event ops written to events.jsonl. OpBlock and OpDone are written by Finish
// (`heavyq item block` and `heavyq item done`); the rest by the lease calls.
const (
	OpClaim   = "claim"
	OpRenew   = "renew"
	OpRelease = "release"
	OpReclaim = "reclaim"
	OpBlock   = "block"
	OpDone    = "done"

	opClaim   = OpClaim
	opRenew   = OpRenew
	opRelease = OpRelease
	opReclaim = OpReclaim
)

// Event is one line of events.jsonl. Reclaim events name the previous holder
// and why the lease was stale (expired, dead, corrupt); release events carry
// forced when the releaser was not the holder; block events carry the need
// and done events the commit the item was proven on.
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
	Needs       string    `json:"needs,omitempty"`
	Commit      string    `json:"commit,omitempty"`
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
