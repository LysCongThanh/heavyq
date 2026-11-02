package stance

import (
	"sort"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
)

// Activation is one stay of a lane in a stance, rebuilt from the events log.
type Activation struct {
	Stance  string
	Lane    string
	Trigger Trigger
	Since   time.Time
	// Left and Reason are set when the stay is over.
	Left   time.Time
	Reason string
}

// Activations pairs the enter and exit events (oldest first) of each lane. A
// lane's last enter without an exit is still active.
func Activations(events []Event) []Activation {
	var out []Activation
	open := map[string]int{} // lane -> index in out of its active stay
	for _, e := range events {
		switch e.Op {
		case OpEnter:
			open[e.Lane] = len(out)
			out = append(out, Activation{Stance: e.Stance, Lane: e.Lane, Trigger: e.Trigger, Since: e.At})
		case OpExit:
			if i, ok := open[e.Lane]; ok && out[i].Stance == e.Stance {
				out[i].Left, out[i].Reason = e.At, e.Reason
				delete(open, e.Lane)
			}
		}
	}
	return out
}

// StanceStats is the measurement of one stance in one repository.
type StanceStats struct {
	Stance      string
	Activations int
	Active      int            // still in the stance
	ByTrigger   map[string]int // how it was entered
	ByExit      map[string]int // how it ended
	// Proven counts the activations whose lane reached a passing run after
	// entering; the medians are over those.
	Proven       int
	MedProofSecs float64
	MedProofRuns float64
	Baselined    int // proven activations whose lane had earlier proofs to compare with
	MedBaseSecs  float64
	MedBaseRuns  float64
	NoProof      int // activations that ended before the lane reached a proof
}

// Stats aggregates the activations per stance. laneRuns returns the records
// of a lane, oldest first (ledgerindex.LaneRuns); the ledger index keeps the
// newest ones, so a very old activation may find no run and count as not proven.
// The comparison is descriptive: proof time and runs after entry against the
// lane's own earlier baseline. A causal claim needs the randomised holdout arm
// of the mentor log, which is not part of this package; a keyword entry is
// explicit intent and is never held out.
func Stats(acts []Activation, laneRuns func(lane string) []history.Record) []StanceStats {
	by := map[string]*StanceStats{}
	var proofSecs, proofRuns, baseSecs, baseRuns = map[string][]float64{}, map[string][]float64{}, map[string][]float64{}, map[string][]float64{}
	cache := map[string][]history.Record{}
	for _, a := range acts {
		s := by[a.Stance]
		if s == nil {
			s = &StanceStats{Stance: a.Stance, ByTrigger: map[string]int{}, ByExit: map[string]int{}}
			by[a.Stance] = s
		}
		s.Activations++
		s.ByTrigger[string(a.Trigger)]++
		if a.Reason == "" {
			s.Active++
		} else {
			s.ByExit[a.Reason]++
		}
		recs, ok := cache[a.Lane]
		if !ok {
			recs = laneRuns(a.Lane)
			cache[a.Lane] = recs
		}
		p := FirstProofAfter(recs, a.Since)
		if !p.Reached {
			if a.Reason != "" {
				s.NoProof++
			}
			continue
		}
		s.Proven++
		proofSecs[a.Stance] = append(proofSecs[a.Stance], p.Secs)
		proofRuns[a.Stance] = append(proofRuns[a.Stance], float64(p.Runs))
		if b := LaneBaseline(recs, a.Since); b.Measured {
			s.Baselined++
			baseSecs[a.Stance] = append(baseSecs[a.Stance], b.MedSecs)
			baseRuns[a.Stance] = append(baseRuns[a.Stance], b.MedRuns)
		}
	}
	out := make([]StanceStats, 0, len(by))
	for name, s := range by {
		s.MedProofSecs, s.MedProofRuns = median(proofSecs[name]), median(proofRuns[name])
		s.MedBaseSecs, s.MedBaseRuns = median(baseSecs[name]), median(baseRuns[name])
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Stance < out[j].Stance })
	return out
}
