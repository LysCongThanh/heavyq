package forecast

import (
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 2, 14, 0, 0, 0, time.UTC)

func TestCompute(t *testing.T) {
	min := time.Minute
	tests := []struct {
		name     string
		in       Input
		status   Status
		conf     Confidence
		eta      time.Duration // from t0; ignored for unknown
		done     bool
		reasonIn string
	}{
		{"nothing remains is done and on track", Input{Now: t0, Remaining: 0, N: 9, P50: 30 * min}, OnTrack, ConfMedium, 0, true, ""},
		{"no samples", Input{Now: t0, Remaining: 3}, Unknown, ConfNone, 0, false, "0 of 3"},
		{"two samples is still unknown", Input{Now: t0, Remaining: 3, N: 2, P50: 30 * min}, Unknown, ConfNone, 0, false, "2 of 3"},
		{"three samples forecast, low confidence", Input{Now: t0, Remaining: 3, N: 3, P50: 30 * min}, OnTrack, ConfLow, 90 * min, false, ""},
		{"parallelism divides the work", Input{Now: t0, Remaining: 4, Running: 2, N: 9, P50: 30 * min}, OnTrack, ConfMedium, 60 * min, false, ""},
		{"no running lane counts as one", Input{Now: t0, Remaining: 2, Running: 0, N: 12, P50: 30 * min}, OnTrack, ConfHigh, 60 * min, false, ""},
		{"fits inside the expected end", Input{Now: t0, Remaining: 2, Running: 1, N: 9, P50: 30 * min,
			ExpectedEnd: t0.Add(61 * min)}, OnTrack, ConfMedium, 60 * min, false, ""},
		{"past the expected end", Input{Now: t0, Remaining: 2, Running: 1, N: 9, P50: 30 * min,
			ExpectedEnd: t0.Add(45 * min)}, AtRisk, ConfMedium, 60 * min, false, "15m past the expected end"},
		{"stalled: no proof for over 2x p50 while lanes work", Input{Now: t0, Remaining: 1, Running: 1, N: 9, P50: 30 * min,
			LastProof: t0.Add(-61 * min)}, AtRisk, ConfMedium, 30 * min, false, "no proof for 1h01m"},
		{"a long quiet spell with nobody working is not a stall", Input{Now: t0, Remaining: 1, Running: 0, N: 9, P50: 30 * min,
			LastProof: t0.Add(-5 * time.Hour)}, OnTrack, ConfMedium, 30 * min, false, ""},
		{"exactly 2x p50 is not yet a stall", Input{Now: t0, Remaining: 1, Running: 1, N: 9, P50: 30 * min,
			LastProof: t0.Add(-60 * min)}, OnTrack, ConfMedium, 30 * min, false, ""},
		{"unknown last proof switches the stall check off", Input{Now: t0, Remaining: 1, Running: 1, N: 9, P50: 30 * min}, OnTrack, ConfMedium, 30 * min, false, ""},
		{"zero p50 and no rate cannot forecast", Input{Now: t0, Remaining: 2, N: 9}, Unknown, ConfMedium, 0, false, "9 proofs counted, no rate measured"},
		{"no p50 but a rate: remaining over the fleet rate, lanes not divided again", Input{Now: t0, Remaining: 3, Running: 3, N: 9, PerHour: 3}, OnTrack, ConfMedium, 60 * min, false, ""},
		{"the p50 wins over the rate", Input{Now: t0, Remaining: 2, Running: 1, N: 9, P50: 30 * min, PerHour: 1}, OnTrack, ConfMedium, 60 * min, false, ""},
		{"a rate below the floor is no rate", Input{Now: t0, Remaining: 2, N: 9, PerHour: 0.001}, Unknown, ConfMedium, 0, false, "no rate measured"},
		{"a rate still needs the minimum samples", Input{Now: t0, Remaining: 2, N: 2, PerHour: 3}, Unknown, ConfNone, 0, false, "2 of 3"},
		{"stalled at the rate: no proof for over 2 typical gaps", Input{Now: t0, Remaining: 1, Running: 1, N: 9, PerHour: 3,
			LastProof: t0.Add(-41 * min)}, AtRisk, ConfMedium, 20 * min, false, "typically one in 20m"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Compute(tc.in)
			if got.Status != tc.status || got.Confidence != tc.conf || got.Done != tc.done {
				t.Fatalf("got %+v, want status %s conf %s done %v", got, tc.status, tc.conf, tc.done)
			}
			if tc.status != Unknown && !got.ETA.Equal(t0.Add(tc.eta)) {
				t.Errorf("ETA %v, want %v", got.ETA.Sub(t0), tc.eta)
			}
			if tc.status == Unknown && !got.ETA.IsZero() {
				t.Errorf("unknown must carry no ETA, got %v", got.ETA)
			}
			if !strings.Contains(got.Reason, tc.reasonIn) || (tc.reasonIn == "" && got.Reason != "") {
				t.Errorf("reason %q, want it to contain %q", got.Reason, tc.reasonIn)
			}
		})
	}
}

func TestConfidenceOf(t *testing.T) {
	for n, want := range map[int]Confidence{-1: ConfNone, 0: ConfNone, 2: ConfNone, 3: ConfLow, 5: ConfLow, 6: ConfMedium, 11: ConfMedium, 12: ConfHigh, 90: ConfHigh} {
		if got := ConfidenceOf(n); got != want {
			t.Errorf("ConfidenceOf(%d) = %s, want %s", n, got, want)
		}
	}
}
