package stance

import (
	"sort"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
)

// counted: a run that counts for measurement: it went to its end on a known tree
// and is not a replay of another run's result.
func counted(r history.Record) bool {
	return r.Result == "" && r.Tree != "" && r.Cache != "hit" && r.Cache != "coalesced"
}

// Proof is how long, and how many runs, it took a lane to its first passing
// run after a moment.
type Proof struct {
	Reached bool
	Secs    float64 // from the moment to the end of the passing run
	Runs    int     // counted runs from the moment to it, the passing one included
}

// FirstProofAfter measures recs (one lane, oldest first) from the moment at:
// the first counted run that ended after it and exited 0 is the proof; Runs
// counts the runs ended after it up to that one. Not reached: the lane has
// not passed since (Runs then counts the failures so far).
func FirstProofAfter(recs []history.Record, at time.Time) Proof {
	var p Proof
	for _, r := range recs {
		if !counted(r) || !r.End.After(at) {
			continue
		}
		p.Runs++
		if r.Exit == 0 {
			p.Reached, p.Secs = true, r.End.Sub(at).Seconds()
			return p
		}
	}
	return p
}

// Baseline is what a lane normally takes between proofs.
type Baseline struct {
	Samples  int
	MedSecs  float64 // median time from the previous proof (or the first run) to a proof
	MedRuns  float64 // median counted runs per proof
	Measured bool    // Samples > 0
}

// LaneBaseline measures the stretches of recs that ended before before: each
// passing run closes one, the stretch being the counted runs since the previous
// pass (or since the first run of the lane) and the time from that point to
// the pass. Records after before are not read, so an activation is compared
// with what its lane did earlier, never with itself.
func LaneBaseline(recs []history.Record, before time.Time) Baseline {
	var secs, runs []float64
	var start time.Time
	n := 0
	for _, r := range recs {
		if !counted(r) || !r.End.Before(before) {
			continue
		}
		if start.IsZero() { // the lane's first run: its own start
			start = r.End.Add(-time.Duration(r.RunSecs * float64(time.Second)))
		}
		n++
		if r.Exit == 0 {
			secs, runs = append(secs, r.End.Sub(start).Seconds()), append(runs, float64(n))
			start, n = r.End, 0
		}
	}
	return Baseline{Samples: len(secs), MedSecs: median(secs), MedRuns: median(runs), Measured: len(secs) > 0}
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	if len(s)%2 == 1 {
		return s[len(s)/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}
