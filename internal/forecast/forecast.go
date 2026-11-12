// Package forecast answers the one question a person has while agents work:
// "will it be done when I need it, and is it on track?". It is pure arithmetic
// on numbers the flow state already holds (no I/O, no clock, no git), so the
// status line, the recap and the notifier all give the same answer.
//
//	ETA = now + remaining items x recent p50 claim-to-proof / parallelism
//
// The p50 is per item and per lane, so the lanes working at once divide the
// remaining work (at least one lane: someone will have to start). When no claim
// ever led to a proof there is no p50 (proofs by someone who never claimed): the
// fleet's measured throughput stands in for it, ETA = now + remaining / rate, and
// the rate already counts the lanes.
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

// minPerHour is the slowest measured rate a forecast is made from: below it (one item in a hundred hours) the
// number says nothing about the work left.
const minPerHour = 0.01

// StallFactor: no proof for longer than this many p50s, while lanes are
// working, is at risk even when the ETA still fits.
const StallFactor = 2

// Input is everything a forecast looks at.
type Input struct {
	Now       time.Time
	Remaining int           // items not proven yet
	Running   int           // lanes working right now; below 1 counts as 1
	P50       time.Duration // recent median claim-to-proof of one item
	N         int           // proofs the P50 (or the rate) rests on
	// PerHour is the measured fleet throughput, items finished per active hour. It is used only when there is no
	// P50 (0): the rate is the whole fleet's, so it is not divided by the lanes. 0 when none was measured.
	PerHour float64
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
	if in.N < MinSamples {
		return Result{Status: Unknown, Confidence: conf, Reason: fmt.Sprintf("%d of %d finishes measured", max(in.N, 0), MinSamples)}
	}
	per, lanes := in.P50, max(in.Running, 1)
	if per <= 0 && in.PerHour >= minPerHour {
		per, lanes = time.Duration(float64(time.Hour)/in.PerHour), 1
	}
	if per <= 0 {
		return Result{Status: Unknown, Confidence: conf, Reason: fmt.Sprintf("%d finishes counted, no rate measured yet", in.N)}
	}
	left := time.Duration(int64(in.Remaining) * int64(per) / int64(lanes))
	res := Result{Status: OnTrack, Confidence: conf, Left: left, ETA: in.Now.Add(left)}
	if !in.ExpectedEnd.IsZero() && res.ETA.After(in.ExpectedEnd) {
		res.Status = AtRisk
		res.Reason = "ETA is " + roundDur(res.ETA.Sub(in.ExpectedEnd)) + " past the expected end"
		return res
	}
	if in.Running > 0 && !in.LastProof.IsZero() {
		if since := in.Now.Sub(in.LastProof); since > StallFactor*per {
			res.Status = AtRisk
			res.Reason = "no proof for " + roundDur(since) + " (typically one in " + roundDur(per) + ")"
			if in.P50 > 0 {
				res.Reason = "no proof for " + roundDur(since) + " (p50 " + roundDur(in.P50) + ")"
			}
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
