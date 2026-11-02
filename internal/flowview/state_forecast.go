package flowview

import (
	"time"

	"github.com/LysCongThanh/heavyq/internal/forecast"
)

// Forecast is the forecast of a state at now: the one adapter from the flow
// state to forecast.Input, so every surface agrees.
func (s *State) Forecast(now time.Time) forecast.Result {
	in := forecast.Input{Now: now, Remaining: max(s.Items.Total-s.Items.Proven, 0), Running: s.Items.Running}
	if t := s.Throughput; t != nil {
		in.N = t.N
		in.P50 = time.Duration(t.P50ClaimToS * float64(time.Second))
		in.LastProof = t.LastProof
	}
	if r := s.RunInfo(); r != nil {
		in.ExpectedEnd = r.ExpectedEnd
	}
	return forecast.Compute(in)
}
