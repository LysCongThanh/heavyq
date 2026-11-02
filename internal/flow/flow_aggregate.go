package flow

import (
	"sort"
	"time"

	"github.com/LysCongThanh/heavyq/internal/itemstatus"
)

// Fleet is the lead time of all timelines inside the window, by state.
type Fleet struct {
	LeadS    float64           `json:"lead_s"`
	ByStateS map[State]float64 `json:"by_state_s"`
	// FlowEfficiency is working / lead time (0 without lead time).
	FlowEfficiency float64 `json:"flow_efficiency"`
}

// Throughput is the primary metric: proven items per active hour. Without
// pull requests "proven" is the proof, not the merge.
type Throughput struct {
	ProvenPerHour    float64 `json:"proven_per_hour"`
	P50ClaimToProofS float64 `json:"p50_claim_to_proof_s"`
	WindowS          float64 `json:"window_s"`
	N                int     `json:"n"` // items proven inside the window
	// LastProof is when the most recent item in the window was proven, null when none was: the statusline and the
	// forecast flag "at risk" when nothing has been proven for twice the median while lanes are working.
	LastProof *time.Time `json:"last_proof"`
}

// Constraint is the state holding the largest share of lead time. State is ""
// when there is no lead time to look at.
type Constraint struct {
	State State   `json:"state"`
	Share float64 `json:"share"`
	// Significant: the share is at least SignificantShare and above the state's
	// own baseline (an EWMA of its past shares, see Baseline), so a share that
	// is large but normal is not news.
	Significant bool `json:"significant"`
}

// aggregate computes the fleet totals, the constraint and the throughput over
// [now-window, now].
func aggregate(in Input, tls []Timeline, holds map[string]*itemHolds) (Fleet, Constraint, Throughput) {
	from := in.Now.Add(-in.window())
	f := Fleet{ByStateS: map[State]float64{}}
	var all []Span
	for _, t := range tls {
		for _, s := range clip(t.Spans, from, in.Now) {
			f.ByStateS[s.State] += s.Duration().Seconds()
			f.LeadS += s.Duration().Seconds()
			all = append(all, s)
		}
	}
	if f.LeadS > 0 {
		f.FlowEfficiency = f.ByStateS[Working] / f.LeadS
	}
	return f, constraintOf(f, in.Baseline), throughputOf(in, holds, from, unionSeconds(all))
}

// constraintOf picks the state with the largest share of lead time. Working is
// the work itself, not a wait, so it is never the constraint (its share is the
// flow efficiency). Ties go to the state of higher priority, which is the one a
// person can act on soonest. base may be nil (no history): the threshold decides.
func constraintOf(f Fleet, base *Baseline) Constraint {
	if f.LeadS <= 0 {
		return Constraint{}
	}
	var states []State
	for s := range f.ByStateS {
		if s != Working && !s.IsTerminal() && f.ByStateS[s] > 0 {
			states = append(states, s)
		}
	}
	sort.Slice(states, func(i, j int) bool {
		if f.ByStateS[states[i]] != f.ByStateS[states[j]] {
			return f.ByStateS[states[i]] > f.ByStateS[states[j]]
		}
		return priority[states[i]] > priority[states[j]]
	})
	if len(states) == 0 {
		return Constraint{}
	}
	share := f.ByStateS[states[0]] / f.LeadS
	return Constraint{State: states[0], Share: share, Significant: share >= SignificantShare && share > base.Of(states[0])}
}

// throughputOf counts the items proven inside the window per hour the fleet was
// active (the wall-clock union of all its spans), and the median time from the
// claim an item's proof followed to the proof.
func throughputOf(in Input, holds map[string]*itemHolds, from time.Time, activeS float64) Throughput {
	tp := Throughput{WindowS: in.window().Seconds()}
	var lat []float64
	for _, f := range in.Items {
		st := f.Status
		if st.State != itemstatus.Proven || st.At.Before(from) || st.At.After(in.Now) {
			continue
		}
		tp.N++
		if tp.LastProof == nil || st.At.After(*tp.LastProof) {
			t := st.At.UTC()
			tp.LastProof = &t
		}
		if ih := holds[st.ID]; ih != nil {
			if c, ok := ih.lastClaim(st.At); ok {
				lat = append(lat, st.At.Sub(c).Seconds())
			}
		}
	}
	if activeS >= 60 {
		tp.ProvenPerHour = float64(tp.N) / (activeS / 3600)
	}
	tp.P50ClaimToProofS = median(lat)
	return tp
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	if n := len(s); n%2 == 0 {
		return (s[n/2-1] + s[n/2]) / 2
	}
	return s[len(s)/2]
}
