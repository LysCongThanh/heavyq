// Package mentor breaks repeat failures and measures whether it helps.
//
// A rule looks at the ledger records of one lane (from internal/ledgerindex,
// never a scan) and, when the evidence is clear, produces a short note made of
// structured facts only (exit code, count, tree). Whether the agent actually
// sees the note is decided by a seeded 80/20 draw: 20% of the eligible events
// are held out, logged exactly like the others but left silent, and the
// outcome of every event (did the next heavy run differ?) is resolved from the
// ledger later. The difference between the arms is the rule's lift, and a rule
// that shows no lift mutes itself. See docs/mentor.md.
//
// Everything here is pure or a small file under the state or queue directory.
// The hook wiring (stdin, stdout, config) lives in internal/cli.
package mentor

// Rule ids. They appear in the intervention log, in `heavyq mentor stats` and
// as the argument of `heavyq mentor mute|unmute`.
const (
	// RuleRepeatFailure: the same run (run_id: tree, argv, env) failed again with
	// a deterministic exit code.
	RuleRepeatFailure = "repeat-failure"
)

// Rules lists every rule id, in a stable order.
func Rules() []string { return []string{RuleRepeatFailure} }

// KnownRule reports whether id names a rule.
func KnownRule(id string) bool {
	for _, r := range Rules() {
		if r == id {
			return true
		}
	}
	return false
}

// Arm is the side of the experiment an event was assigned to.
type Arm string

// Arms of the experiment.
const (
	// ArmInject: the note is shown to the agent.
	ArmInject Arm = "inject"
	// ArmHoldout: the event is logged and nothing is shown.
	ArmHoldout Arm = "holdout"
)

// Outcomes of an event, as written to the intervention log.
const (
	// OutcomePending: fewer than OutcomeWindow runs followed and all repeated the failing run.
	OutcomePending = "pending"
	// OutcomeDiffered: a run within the window had another run_id (other argv or tree).
	OutcomeDiffered = "differed"
	// OutcomeSame: OutcomeWindow runs followed and every one repeated the failing run.
	OutcomeSame = "same"
	// OutcomeUnknown: the event's run is no longer in the lane's records, so
	// what followed cannot be told. Unknown events count in neither arm's rate.
	OutcomeUnknown = "unknown"
)
