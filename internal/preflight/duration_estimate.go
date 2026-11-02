package preflight

import (
	"sort"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
)

// Durations is the median run time of each command the ledger has a passing
// run of, by cmd_hash (history.CmdHash of the argv heavyq ran).
type Durations map[string]time.Duration

// P50ByCmdHash computes the median RunSecs per cmd_hash over the passing runs
// of recs. A run counts when it ran to its end and exited 0, and is not a
// replay of another run (a cache hit takes no time of its own).
func P50ByCmdHash(recs []history.Record) Durations {
	by := map[string][]float64{}
	for _, r := range recs {
		if r.CmdHash == "" || r.Exit != 0 || r.Outcome() != history.OutcomeRan ||
			r.Cache == "hit" || r.Cache == "coalesced" || r.RunSecs <= 0 {
			continue
		}
		by[r.CmdHash] = append(by[r.CmdHash], r.RunSecs)
	}
	out := make(Durations, len(by))
	for h, xs := range by {
		sort.Float64s(xs)
		n := len(xs)
		m := xs[n/2]
		if n%2 == 0 {
			m = (xs[n/2-1] + xs[n/2]) / 2
		}
		out[h] = time.Duration(m * float64(time.Second))
	}
	return out
}

// Estimate is the proof's duration: the sum of the median of each command's
// hash. It is unknown when any command has no passing run in d, or has no
// command at all.
func (d Durations) Estimate(cmdHashes []string) Estimate {
	if len(cmdHashes) == 0 {
		return Estimate{}
	}
	var total time.Duration
	for _, h := range cmdHashes {
		p, ok := d[h]
		if !ok {
			return Estimate{}
		}
		total += p
	}
	return Estimate{P50: total, Known: true}
}
