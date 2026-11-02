package mentor

import (
	"fmt"
	"math"
	"time"
)

// ArmStats counts the events of one arm. Rate is the share of resolved
// events whose outcome was "differed", 0 when none is resolved.
type ArmStats struct {
	Events   int     `json:"events"`
	Resolved int     `json:"resolved"` // differed + same: the events a rate can be told from
	Differed int     `json:"differed"`
	Rate     float64 `json:"rate"`
}

// RuleStats is the experiment of one rule so far.
type RuleStats struct {
	Rule    string   `json:"rule"`
	Inject  ArmStats `json:"inject"`
	Holdout ArmStats `json:"holdout"`
	// LiftPP is inject rate minus holdout rate in percentage points; nil
	// until both arms have a resolved event.
	LiftPP *float64 `json:"lift_pp,omitempty"`
	Muted  bool     `json:"muted"`
	// MutedBy is "auto" or "user" when Muted; MutedWhy says why.
	MutedBy  string `json:"muted_by,omitempty"`
	MutedWhy string `json:"muted_why,omitempty"`
}

// ComputeStats counts the (folded) entries per rule and arm, for the events
// at or after since (zero: all). It lists every known rule, events or not.
func ComputeStats(entries []Entry, since time.Time) []RuleStats {
	out := make([]RuleStats, 0, len(Rules()))
	for _, rule := range Rules() {
		rs := RuleStats{Rule: rule}
		for _, e := range entries {
			if e.Rule != rule || e.At.Before(since) {
				continue
			}
			a := &rs.Inject
			if e.Arm == ArmHoldout {
				a = &rs.Holdout
			}
			a.Events++
			switch e.Outcome {
			case OutcomeDiffered:
				a.Resolved++
				a.Differed++
			case OutcomeSame:
				a.Resolved++
			}
		}
		for _, a := range []*ArmStats{&rs.Inject, &rs.Holdout} {
			if a.Resolved > 0 {
				a.Rate = float64(a.Differed) / float64(a.Resolved)
			}
		}
		if rs.Inject.Resolved > 0 && rs.Holdout.Resolved > 0 {
			lift := math.Round((rs.Inject.Rate-rs.Holdout.Rate)*100*1e6) / 1e6 // rounded: 0.6-0.5 is not exactly 0.1
			rs.LiftPP = &lift
		}
		out = append(out, rs)
	}
	return out
}

// AutoMute is the policy that silences a rule that does not help.
type AutoMute struct {
	Enabled bool
	// MinEvents is how many resolved events each arm needs before the lift is judged.
	MinEvents int
	// MinLiftPP is the lift, in percentage points, below which the rule is muted.
	MinLiftPP float64
}

// DefaultAutoMute is the kill rule of the research design: under 10 points of
// lift after 200 resolved events per arm.
var DefaultAutoMute = AutoMute{Enabled: true, MinEvents: 200, MinLiftPP: 10}

// ShouldMute applies the policy to one rule's stats and says why.
func (p AutoMute) ShouldMute(rs RuleStats) (bool, string) {
	if !p.Enabled || rs.Muted || rs.LiftPP == nil ||
		rs.Inject.Resolved < p.MinEvents || rs.Holdout.Resolved < p.MinEvents {
		return false, ""
	}
	if *rs.LiftPP >= p.MinLiftPP {
		return false, ""
	}
	return true, fmt.Sprintf("lift %.1f pp < %.0f pp after %d/%d resolved events (inject/holdout)",
		*rs.LiftPP, p.MinLiftPP, rs.Inject.Resolved, rs.Holdout.Resolved)
}
