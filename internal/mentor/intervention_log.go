package mentor

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Entry is one line of the intervention log: an eligible event, whichever arm
// it was drawn into. The log is append-only. When the outcome of an event
// becomes known, the same entry is appended again with Outcome set, and
// readers keep the last line per event (Fold).
type Entry struct {
	V         int       `json:"v"`
	At        time.Time `json:"at"`
	Rule      string    `json:"rule"`
	Level     int       `json:"level"`
	Seed      string    `json:"seed"`
	Arm       Arm       `json:"arm"`
	Event     string    `json:"event"` // id of the ledger record that raised it
	Lane      string    `json:"lane"`
	Session   string    `json:"session,omitempty"`
	RunID     string    `json:"run_id"`
	Tree      string    `json:"tree"`
	Streak    int       `json:"streak"`
	Outcome   string    `json:"outcome"`
	OutcomeAt time.Time `json:"outcome_at,omitzero"`
}

const logVersion = 1

// maxLogLine bounds one line read back; a longer one is corrupt and skipped.
const maxLogLine = 64 << 10

func (s *Store) logPath() string { return filepath.Join(s.Dir, "interventions.jsonl") }

// AppendEntry adds e to the log with one O_APPEND write (atomic for a line
// this small, so concurrent hooks never interleave).
func (s *Store) AppendEntry(e Entry) error {
	e.V = logVersion
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.logPath(), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.Write(append(b, '\n'))
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	return werr
}

// ReadEntries returns the folded log: one entry per event, the newest line
// of each, in the order the events first appeared. A missing log is empty;
// corrupt lines are skipped.
func (s *Store) ReadEntries() ([]Entry, error) {
	f, err := os.Open(s.logPath())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var all []Entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 4096), maxLogLine)
	for sc.Scan() {
		var e Entry
		if json.Unmarshal(sc.Bytes(), &e) == nil && e.Rule != "" && e.Event != "" {
			all = append(all, e)
		}
	}
	if err := sc.Err(); err != nil && !errors.Is(err, bufio.ErrTooLong) {
		return nil, err
	}
	return Fold(all), nil
}

// Fold keeps the last entry of each (rule, event), in first-seen order.
func Fold(all []Entry) []Entry {
	type key struct{ rule, event string }
	at := map[key]int{}
	var out []Entry
	for _, e := range all {
		k := key{e.Rule, e.Event}
		if i, ok := at[k]; ok {
			out[i] = e
			continue
		}
		at[k] = len(out)
		out = append(out, e)
	}
	return out
}
