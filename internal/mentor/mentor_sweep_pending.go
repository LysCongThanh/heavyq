package mentor

import (
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
)

// SweepPending settles pending events across every lane's records, for
// `heavyq mentor stats`: an event whose lane has not run since would show as
// pending for good. laneRecords returns the records of a lane (oldest first).
// It returns how many events it settled.
func (s *Store) SweepPending(laneRecords func(lane string) ([]history.Record, error), c Config, now time.Time) (int, error) {
	entries, err := s.ReadEntries()
	if err != nil {
		return 0, err
	}
	cache := map[string][]history.Record{}
	n := 0
	for _, e := range entries {
		if e.Outcome != OutcomePending {
			continue
		}
		recs, ok := cache[e.Lane]
		if !ok {
			if recs, err = laneRecords(e.Lane); err != nil {
				return n, err
			}
			cache[e.Lane] = recs
		}
		out := ResolveOutcome(e, recs)
		if out == OutcomePending {
			continue
		}
		e.Outcome, e.OutcomeAt = out, now.UTC()
		if err := s.AppendEntry(e); err != nil {
			return n, err
		}
		n++
	}
	if n > 0 {
		_, err = s.EvaluateAutoMute(c.AutoMute, now)
	}
	return n, err
}
