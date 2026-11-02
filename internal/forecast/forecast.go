// Package forecast answers the one question a person has while agents work:
// "will it be done when I need it, and is it on track?". It is pure arithmetic
// on numbers the flow state already holds (no I/O, no clock, no git), so the
// status line, the recap and the notifier all give the same answer.
//
//	ETA = now + remaining items x recent p50 claim-to-proof / parallelism
//
// The p50 is per item and per lane, so the lanes working at once divide the
// remaining work (at least one lane: someone will have to start).
package forecast

import (
	"fmt"
	"time"
)

// Status is the verdict. unknown is honest: with fewer than MinSamples proofs
// there is no measured throughput to forecast from.
type Status string

// Verdicts.
const (
	OnTrack Status = "on_track"
	AtRisk  Status = "at_risk"
	Unknown Status = "unknown"
)

// Confidence labels how much the estimate rests on: the number of proofs in
// the throughput window.
type Confidence string

// Labels, by sample count (see ConfidenceOf).
const (
	ConfNone   Confidence = "none"   // fewer than MinSamples
	ConfLow    Confidence = "low"    // 3 to 5
	ConfMedium Confidence = "medium" // 6 to 11
	ConfHigh   Confidence = "high"   // 12 or more
)

// MinSamples is how many proofs a forecast needs. Below it the status is unknown.
const MinSamples = 3

// StallFactor: no proof for longer than this many p50s, while lanes are
// working, is at risk even when the ETA still fits.
const StallFactor = 2

// Input is everything a forecast looks at.
type Input struct {
	Now       time.Time
	Remaining int           // items not proven yet
	Running   int           // lanes working right now; below 1 counts as 1
	P50       time.Duration // recent median claim-to-proof of one item
	N         int           // proofs the P50 rests on
	// LastProof is when the latest item was proven; zero when unknown, which
	// switches the stall check off.
	LastProof time.Time
	// ExpectedEnd is the end a run promised (preflight); zero when none.
	ExpectedEnd time.Time
}

// Result is a forecast.
type Result struct {
	Status     Status
	Confidence Confidence
	Done       bool          // nothing remains; ETA is Now
	ETA        time.Time     // zero for Unknown
	Left       time.Duration // ETA - Now, 0 for Unknown
	Reason     string        // why it is at risk or unknown, one short phrase; "" when on track
}

// ConfidenceOf maps a sample count to its label.
func ConfidenceOf(n int) Confidence {
	switch {
	case n < MinSamples:
		return ConfNone
	case n < 6:
		return ConfLow
	case n < 12:
		return ConfMedium
	}
	return ConfHigh
}

// Compute forecasts the end of the remaining items.
func Compute(in Input) Result {
	conf := ConfidenceOf(in.N)
	if in.Remaining <= 0 {
		return Result{Status: OnTrack, Confidence: conf, Done: true, ETA: in.Now}
	}
	if in.N < MinSamples || in.P50 <= 0 {
		return Result{Status: Unknown, Confidence: conf, Reason: fmt.Sprintf("%d of %d proofs measured", max(in.N, 0), MinSamples)}
	}
	lanes := max(in.Running, 1)
	left := time.Duration(int64(in.Remaining) * int64(in.P50) / int64(lanes))
	res := Result{Status: OnTrack, Confidence: conf, Left: left, ETA: in.Now.Add(left)}
	if !in.ExpectedEnd.IsZero() && res.ETA.After(in.ExpectedEnd) {
		res.Status = AtRisk
		res.Reason = "ETA is " + roundDur(res.ETA.Sub(in.ExpectedEnd)) + " past the expected end"
		return res
	}
	if in.Running > 0 && !in.LastProof.IsZero() {
		if since := in.Now.Sub(in.LastProof); since > StallFactor*in.P50 {
			res.Status = AtRisk
			res.Reason = "no proof for " + roundDur(since) + " (p50 " + roundDur(in.P50) + ")"
		}
	}
	return res
}

// roundDur shows a duration at the precision a person plans with.
func roundDur(d time.Duration) string {
	switch {
	case d < time.Minute:
		return d.Round(time.Second).String()
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Round(time.Minute)/time.Minute))
	}
	m := int(d.Round(time.Minute) / time.Minute)
	if m%60 == 0 {
		return fmt.Sprintf("%dh", m/60)
	}
	return fmt.Sprintf("%dh%02dm", m/60, m%60)
}
