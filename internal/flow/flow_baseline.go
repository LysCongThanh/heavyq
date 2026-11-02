package flow

import (
	"sort"
	"time"
)

// The baseline of the constraint finder: per state, an exponentially weighted
// moving average of the share of lead time the state held in past windows. A
// share that is large but normal for this repository is not news: the
// constraint is significant only when it also exceeds its own baseline
// (autonomy-loop plan, section 3.2).
const (
	// BaselineAlpha is the weight of the newest window in the average.
	BaselineAlpha = 0.2
	// BaselineStep is the least time between two updates of the baseline. The
	// windows are four hours wide, so a finer step would only average the same
	// minutes again, and the state is refreshed every minute or so.
	BaselineStep = 30 * time.Minute
	// baselineFloor drops a state from the baseline once its average is this
	// small: it has not been a wait in a long while.
	baselineFloor = 0.005
)

// Baseline is the per-state EWMA of shares. It starts empty (every state's
// baseline is 0), so the first windows of a repository, and any state that was
// never a wait before, are news as soon as the share reaches 0.30. A wait that
// persists is learned as normal over hours, not at once.
type Baseline struct {
	// At is when the shares were last folded in.
	At     time.Time         `json:"at"`
	Shares map[State]float64 `json:"shares"`
}

// Of is the baseline share of s; 0 for a nil baseline or an unseen state.
func (b *Baseline) Of(s State) float64 {
	if b == nil {
		return 0
	}
	return b.Shares[s]
}

// NextBaseline folds the shares of f (a window ending at now) into prev and
// returns the result, to be stored and handed to the next Build. Less than a
// BaselineStep after the last update it returns prev unchanged, so however
// often the state is refreshed the average moves at one pace. A baseline
// stamped in the future (a clock step) counts as old.
func NextBaseline(prev *Baseline, f Fleet, now time.Time) *Baseline {
	if f.LeadS <= 0 {
		return prev // an empty window says nothing about what is normal
	}
	if prev != nil {
		if age := now.Sub(prev.At); age >= 0 && age < BaselineStep {
			return prev
		}
	}
	next := &Baseline{At: now.UTC(), Shares: map[State]float64{}}
	seen := map[State]bool{}
	for s, secs := range f.ByStateS {
		if s == Working || s.IsTerminal() {
			continue // working is the work itself; terminal states hold no time
		}
		seen[s] = true
		next.Shares[s] = BaselineAlpha*secs/f.LeadS + (1-BaselineAlpha)*prev.Of(s)
	}
	if prev != nil {
		for s, v := range prev.Shares {
			if !seen[s] { // not a wait in this window: the average decays
				next.Shares[s] = (1 - BaselineAlpha) * v
			}
		}
	}
	for s, v := range next.Shares {
		if v < baselineFloor {
			delete(next.Shares, s)
		}
	}
	return next
}

// Hotspot is a file that conflicts again and again.
type Hotspot struct {
	File  string  `json:"file"`
	Count int     `json:"count"` // merges it conflicted in
	Share float64 `json:"share"` // of all merges that conflicted
}

// ConflictHotspots ranks the files by how many conflicting merges (spans of
// ConflictRework with Files) they were in, most first, then by name. A file in
// at least 30% of the conflicts is a structural hotspot: the cure is not
// resolving it faster but not having one file everybody edits.
func ConflictHotspots(ext []External) []Hotspot {
	counts := map[string]int{}
	merges := 0
	for _, e := range ext {
		if e.Span.State != ConflictRework || len(e.Files) == 0 {
			continue
		}
		merges++
		for _, f := range e.Files {
			counts[f]++
		}
	}
	out := make([]Hotspot, 0, len(counts))
	for f, n := range counts {
		out = append(out, Hotspot{File: f, Count: n, Share: float64(n) / float64(merges)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].File < out[j].File
	})
	return out
}
