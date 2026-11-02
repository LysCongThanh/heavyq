package stance

import (
	"fmt"
	"sort"
	"strings"
)

// Trigger says which evidence put a lane into a stance.
type Trigger string

const (
	// TriggerKeyword: the user's prompt named it. Explicit intent.
	TriggerKeyword Trigger = "keyword"
	// TriggerFailureClass: the same failure class on several trees.
	TriggerFailureClass Trigger = "same_failure_class_across_trees"
	// TriggerChurn: reverts or rewrites of the same files without a proof.
	TriggerChurn Trigger = "churn_reverts"
	// TriggerManual: `heavyq stance enter`.
	TriggerManual Trigger = "manual"
)

// Evidence is everything the triggers read, collected by the caller (the
// ledger index, git, the prompt) so that Evaluate stays pure. A zero field
// is "no evidence of that kind". Elapsed time is deliberately not a field:
// time alone is never a reason to interrupt an agent.
type Evidence struct {
	Prompt  string
	Failure FailureClass
	Churn   Churn
}

// Match is one stance whose enter rule is satisfied.
type Match struct {
	Stance  string
	Trigger Trigger
	// Why is the evidence in one line, the "why now" of the injection.
	Why string
}

// Evaluate returns the stances whose enter rules ev satisfies, best first: the
// user's keyword before any evidence, then a failure class, then churn; by name
// within a kind. A stance matches by its first satisfied rule only, so a lane is
// never offered the same stance twice. Stall rules are ignored (status line only).
func Evaluate(all []Stance, ev Evidence) []Match {
	var out []Match
	for _, s := range all {
		if kw, ok := MatchKeyword(ev.Prompt, s.Enter.Keywords); ok {
			out = append(out, Match{s.Name, TriggerKeyword, fmt.Sprintf("you asked for it (%q in your prompt)", kw)})
			continue
		}
		if n := s.Enter.SameFailureClassAcrossTrees; n > 0 && ev.Failure.Trees >= n {
			out = append(out, Match{s.Name, TriggerFailureClass,
				fmt.Sprintf("same failure class on %d trees: %s", ev.Failure.Trees, ev.Failure.Label)})
			continue
		}
		if n := s.Enter.ChurnReverts; n > 0 && ev.Churn.Count() >= n {
			out = append(out, Match{s.Name, TriggerChurn, churnWhy(ev.Churn)})
		}
	}
	rank := map[Trigger]int{TriggerKeyword: 0, TriggerFailureClass: 1, TriggerChurn: 2}
	sort.SliceStable(out, func(i, j int) bool {
		if rank[out[i].Trigger] != rank[out[j].Trigger] {
			return rank[out[i].Trigger] < rank[out[j].Trigger]
		}
		return out[i].Stance < out[j].Stance
	})
	return out
}

// NeedsEvidence says which evidence the stances ask for, so a caller reads
// only what some enter rule uses (git is not free, a prompt is).
func NeedsEvidence(all []Stance) (failure, churn bool) {
	for _, s := range all {
		failure = failure || s.Enter.SameFailureClassAcrossTrees > 0
		churn = churn || s.Enter.ChurnReverts > 0
	}
	return failure, churn
}

func churnWhy(c Churn) string {
	var parts []string
	if c.Reverts > 0 {
		parts = append(parts, fmt.Sprintf("%d reverts", c.Reverts))
	}
	if c.Rewrites > 0 {
		parts = append(parts, fmt.Sprintf("%d rewrites", c.Rewrites))
	}
	return fmt.Sprintf("%s of the same files without a proof since: %s", strings.Join(parts, " and "), strings.Join(c.Files, ", "))
}
