package preflight

import (
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
)

// Thresholds of the brief's nudge: a lane that has run this many heavy commands
// in the window is in a long session.
const (
	NudgeRuns   = 5
	NudgeWindow = 30 * time.Minute
)

// ShouldNudge says whether the brief should tell the agent to run `heavyq
// preflight`: the lane has items, no run span is open, and the session looks
// long (NudgeRuns heavy runs of the lane within NudgeWindow). recs are the
// lane's latest ledger records, oldest first. Replays of other runs (cache
// hits) and runs that never ran (gave up, cancelled) are not work.
func ShouldNudge(recs []history.Record, hasItems, hasOpenSpan bool, now time.Time) bool {
	if !hasItems || hasOpenSpan {
		return false
	}
	n := 0
	for _, r := range recs {
		if r.Light || r.Outcome() != history.OutcomeRan || r.Cache == "hit" || r.Cache == "coalesced" ||
			r.End.Before(now.Add(-NudgeWindow)) || r.End.After(now.Add(time.Minute)) {
			continue
		}
		n++
	}
	return n >= NudgeRuns
}
