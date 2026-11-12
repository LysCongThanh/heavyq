package flowview

import (
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/forecast"
)

// Proofs nobody claimed first leave no claim-to-proof median (it is 0), but they are throughput: the forecast
// reads the measured rate instead of staying unknown, and says what it rests on.
func TestForecastFromThroughputWhenThereIsNoClaimMedian(t *testing.T) {
	body := strings.NewReplacer(
		`"proven_per_hour":2.1,"p50_claim_to_proof_s":1800`, `"proven_per_hour":3,"p50_claim_to_proof_s":0`,
		`"n":9}`, `"n":9,"last_proof":"2026-11-02T13:50:00Z"}`,
	).Replace(sample)
	f := Forecast(readSample(t, body), now)
	// 3 remaining items at a fleet rate of 3 per active hour: one hour, and the rate already counts the lanes.
	if f.Status != forecast.OnTrack || f.ETA != now.Add(time.Hour) || f.Confidence != forecast.ConfMedium {
		t.Errorf("forecast: %+v", f)
	}
	// A stall is still a stall: nothing proven for twice the typical gap (20 minutes at 3 per hour) while lanes work.
	stalled := strings.Replace(body, `"last_proof":"2026-11-02T13:50:00Z"`, `"last_proof":"2026-11-02T13:10:00Z"`, 1)
	if f := Forecast(readSample(t, stalled), now); f.Status != forecast.AtRisk || !strings.Contains(f.Reason, "no proof for") {
		t.Errorf("50 minutes without a proof at 3 an hour: %+v", f)
	}
	// Enough proofs but no measured rate (no active time) is unknown, and does not claim "9 of 3 finishes measured".
	none := strings.Replace(body, `"proven_per_hour":3`, `"proven_per_hour":0`, 1)
	f = Forecast(readSample(t, none), now)
	if f.Status != forecast.Unknown || strings.Contains(f.Reason, "9 of 3") {
		t.Errorf("no rate: %+v", f)
	}
}
