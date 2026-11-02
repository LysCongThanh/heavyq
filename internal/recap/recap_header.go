package recap

import (
	"time"

	"github.com/LysCongThanh/heavyq/internal/forecast"
)

// headerFacts is the forecast and the constraint. Counts come from the items
// just read; throughput, the run's expected end and the constraint from the
// flow state.
func headerFacts(f Facts, remaining, running int, lastProof time.Time) (Forecast, string) {
	in := forecast.Input{Now: f.Now, Remaining: remaining, Running: running, LastProof: lastProof}
	constraint := ""
	if st := f.State; st != nil {
		t := st.Throughput
		in.N, in.P50 = t.N, time.Duration(t.P50ClaimToProofS*float64(time.Second))
		if t.LastProof != nil {
			in.LastProof = *t.LastProof
		}
		if run := st.RunInfo(); run != nil && run.ExpectedEnd != nil {
			in.ExpectedEnd = *run.ExpectedEnd
		}
		if c := st.Constraint; c.Significant && c.State != "" && c.State != "none" {
			constraint = string(c.State)
		}
	}
	res := forecast.Compute(in)
	out := Forecast{Status: string(res.Status), Confidence: string(res.Confidence), Done: res.Done, Reason: res.Reason, Samples: in.N}
	if !res.ETA.IsZero() {
		t := res.ETA
		out.ETA = &t
	}
	return out, constraint
}

// best picks the single action that would help most, in the order a person
// would triage: unblock, then what is slowing everything, then repeats, then
// the work that is nearly done, then start the next.
func best(r Recap, firstOpen string) *Best {
	switch {
	case len(r.Blocked) > 0:
		b := r.Blocked[0]
		return &Best{Command: b.Action, Why: clean(b.Text, 70)}
	case r.Forecast.Status == string(forecast.AtRisk):
		return &Best{Command: "heavyq top", Why: "at risk: " + r.Forecast.Reason}
	case len(r.Wasted) > 0:
		w := r.Wasted[0]
		return &Best{Command: w.Action, Why: "the same failure repeated: " + w.Text}
	case len(r.ClaimedUnproven) > 0:
		c := r.ClaimedUnproven[0]
		return &Best{Command: c.Action, Why: c.Text}
	}
	for _, p := range append(append([]Line{}, r.ProvenNotMerged...), r.Merged...) {
		if p.tick {
			return &Best{Command: p.Action, Why: p.ID + " is proven, tick it"}
		}
	}
	if firstOpen != "" && len(r.InFlight) == 0 {
		return &Best{Command: "heavyq item claim --next", Why: "nothing is in flight"}
	}
	return nil
}
