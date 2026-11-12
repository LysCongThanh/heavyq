// Package flowview is what the status line, the recap and the notifier need
// around the materialised flow state (<git common dir>/heavyq/flow/state.json):
// the file-system repository finder, which keeps the status line free of git,
// and the adapters from a *flow.Snapshot to the forecast and the notifier. The
// state itself is internal/flow's Snapshot and internal/flowstate reads it; there
// is no second parser. Nothing here computes flow: that is `heavyq flow
// refresh`'s job.
package flowview

import (
	"time"

	"github.com/LysCongThanh/heavyq/internal/flow"
	"github.com/LysCongThanh/heavyq/internal/forecast"
)

// ForecastInput is the one adapter from the flow state to forecast.Input, so
// every surface agrees. throughput.last_proof is what tells a stall (nothing
// proven for twice the median while lanes work) from a late ETA; the run's
// expected end bounds an unattended run.
func ForecastInput(s *flow.Snapshot, now time.Time) forecast.Input {
	in := forecast.Input{Now: now, Remaining: max(s.Items.Total-s.Items.Proven, 0), Running: s.Items.Running}
	t := s.Throughput
	in.N = t.N
	in.P50 = time.Duration(t.P50ClaimToProofS * float64(time.Second))
	in.PerHour = t.ProvenPerHour
	if t.LastProof != nil {
		in.LastProof = *t.LastProof
	}
	if r := s.RunInfo(); r != nil && r.ExpectedEnd != nil {
		in.ExpectedEnd = *r.ExpectedEnd
	}
	return in
}

// Forecast is the forecast of a state at now.
func Forecast(s *flow.Snapshot, now time.Time) forecast.Result {
	return forecast.Compute(ForecastInput(s, now))
}
