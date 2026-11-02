package snapshot

import "time"

// Exception kinds: the only four things that put a card on the dashboard's
// Tower. Each one is a fact heavyq measured, never something an agent said.
const (
	KindRedGate = "red_gate" // a lane's gate fails on the code it has checked out now
	KindLoop    = "loop"     // the same command failed again and again on the same code
	KindStarved = "starved"  // a job waited close to the bypass delay, or gave up
	KindMachine = "machine"  // unreachable, paused, or crowded out by processes outside the queue
)

// Severities.
const (
	SeverityCritical = "critical" // the lane is stuck until someone acts
	SeverityWarning  = "warning"  // capacity or fairness: worth a look
)

// Action kinds. cancel, raise and resume are control-API operations the
// dashboard can perform with --control; run is a command only a human or an
// agent can start, so it is always shown as text to copy.
const (
	ActionCancel = "cancel"
	ActionRaise  = "raise"
	ActionResume = "resume"
	ActionRun    = "run"
)

// Failure classes (set by the flaky classifier of issue #30). A failure of
// one of these classes is noise, not an exception.
const (
	ClassFlaky     = "flaky"
	ClassUnderLoad = "under_load"
)

// Gate states of a lane (LaneGate.State).
const (
	GateProven   = "proven"   // every command of the gate passed on the lane's tree
	GateUnproven = "unproven" // some command has no conclusive run on it yet
	GateFailed   = "failed"   // some command's latest conclusive run failed
	GateSkipped  = "skipped"  // not checked; Reason says why
)

// Exception is one thing that needs a human, with the evidence and at most one
// action. Added to Snapshot within schema 1.
type Exception struct {
	// ID is stable while the exception lasts and changes when its evidence
	// does (another tree, more failures), so a dismissal does not hide news.
	ID       string   `json:"id"`
	Kind     string   `json:"kind"`
	Severity string   `json:"severity"`
	Lane     string   `json:"lane,omitempty"`
	Machine  string   `json:"machine,omitempty"`
	Text     string   `json:"text"` // one sentence
	Evidence Evidence `json:"evidence"`
	Action   *Action  `json:"action,omitempty"`
}

// Evidence is what heavyq saw. Fields that do not apply are absent.
type Evidence struct {
	At     time.Time `json:"at,omitzero"` // when it last happened
	Tree   string    `json:"tree,omitempty"`
	Cmd    string    `json:"cmd,omitempty"` // secrets redacted
	Count  int       `json:"count,omitempty"`
	Gate   string    `json:"gate,omitempty"`
	Detail string    `json:"detail,omitempty"`
}

// Action is the one thing to do about an exception. Command is always set:
// it is what to type when the dashboard has no --control.
type Action struct {
	Kind    string `json:"kind"`
	Target  string `json:"target,omitempty"` // ticket id, machine name, or gate name
	Value   int    `json:"value,omitempty"`  // raise: the priority to ask for
	Command string `json:"command"`
}

// LaneGate is the proof state of the main gate of the work tree a lane ran in
// last. It feeds the red-gate exception and the "N unproven" count. Computed
// by the builder (it needs git and heavyq.toml); this package only reads it.
type LaneGate struct {
	Lane   string `json:"lane"`
	Gate   string `json:"gate,omitempty"`
	Tree   string `json:"tree,omitempty"`
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"` // why it was skipped
	// Failed lists the gate commands whose latest conclusive run on Tree failed.
	Failed []GateFailure `json:"failed,omitempty"`
	// Worktree is where to run the gate again. Not part of the JSON: it is a
	// path of this machine, and only the suggested command uses it.
	Worktree string `json:"-"`
}

// GateFailure is one failed gate command.
type GateFailure struct {
	Cmd   string    `json:"cmd"` // secrets redacted
	At    time.Time `json:"at"`
	Count int       `json:"count"` // conclusive failures of it on this tree
	Class string    `json:"class,omitempty"`
}
