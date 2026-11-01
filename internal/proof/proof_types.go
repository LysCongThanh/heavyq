// Package proof answers one question: has this exact code passed this
// command? It is derived entirely from the run ledger. There is no separate
// "proof" store to trust or to drift: a proof is a ledger record.
//
// A record is a run of a gate command on a tree when all of these hold:
//   - same repository, and the same tree id as the code being asked about
//     (an unknown tree, "", is never equal to anything);
//   - it ran the exact command heavyq runs for the gate (`sh -c <command>`),
//     from the work tree root. That is decided by cmd_hash, the sha256 of the
//     argv, when the record has one, and by the redacted command string
//     (which the ledger cuts at 200 bytes) when it is older;
//   - it ran to its end: no gave_up, cancelled or interrupted result. A cache
//     hit or a coalesced run counts, because the ledger only gives those a
//     tree id when the result belongs to that tree.
//
// The command is proven when its runs on the tree all agree that it passes:
// the latest run exited 0, and no run failed for a reason the ledger cannot
// explain. A command with both a pass and an unexplained failure on the same
// tree is flaky and not proven. A failure the ledger can explain by the
// machine (the run was admitted past a resource limit, or the load or free
// RAM at start was beyond the gate's limits) is under_load: it fails the
// command while it is the latest run, but it does not make the command flaky,
// so a clean rerun proves it. Everything not provable from the ledger is
// "missing".
package proof

import "time"

// State is the proof state of one gate command.
type State string

// States of a gate command.
const (
	Proven  State = "proven"  // the latest run on this tree passed, and none failed unexplained
	Failed  State = "failed"  // not proven because of a run on this tree that failed (see Class)
	Missing State = "missing" // no usable run on this tree (or it cannot be told)
)

// Class says what kind of failure a Failed command is. It is empty for
// commands that are not failed.
type Class string

// Classes of a failed command.
const (
	// ClassFlaky: on this tree the command both passed and failed, with a
	// failure the machine does not explain.
	ClassFlaky Class = "flaky"
	// ClassUnderLoad: the latest run failed while the machine was over its
	// limits when it was admitted. Rerun before debugging.
	ClassUnderLoad Class = "under_load"
	// ClassFailed: the latest run failed with nothing to explain it away.
	ClassFailed Class = "failed"
)

// Subject is the code a proof is asked about.
type Subject struct {
	Repo   string // gitutil.Checkout.Repo
	Tree   string // gitutil.Checkout.Tree; "" = unknown, nothing can be proven
	Commit string
	Dirty  bool
	// Why says why Tree is unknown (not a repository, no commit, a submodule
	// with its own changes, ...). Only meaningful when Tree is "".
	Why string
}

// Command is the status of one gate command on a Subject. The JSON tags are
// the `heavyq gate status --json` schema (docs/gates.md).
type Command struct {
	Cmd     string    `json:"cmd"`
	State   State     `json:"state"`
	Class   Class     `json:"class,omitempty"`   // why it failed: flaky | under_load | failed; only when State is failed
	At      time.Time `json:"at,omitzero"`       // end of the deciding run
	Lane    string    `json:"lane,omitempty"`    // lane of the deciding run
	RunSecs float64   `json:"run_secs"`          // run time of the deciding run (0 for a cache hit)
	Cache   string    `json:"cache,omitempty"`   // hit | coalesced | miss | skip | ""
	Exit    *int      `json:"exit,omitempty"`    // exit code of the deciding run
	Machine string    `json:"machine,omitempty"` // where the deciding run ran
	Passes  int       `json:"passes,omitempty"`  // runs on this tree that exited 0 (cache replays of a run are not counted again)
	Fails   int       `json:"fails,omitempty"`   // runs on this tree that did not
	Reason  string    `json:"reason,omitempty"`  // why it is missing, flaky or under load, when known
}

// Gate is the status of a whole gate: Proven iff every command is.
type Gate struct {
	Gate     string    `json:"gate"`
	Proven   bool      `json:"proven"`
	Tree     string    `json:"tree"`
	Commit   string    `json:"commit,omitempty"`
	Dirty    bool      `json:"dirty"`
	Reason   string    `json:"reason,omitempty"` // set when nothing can be proven at all
	Commands []Command `json:"commands"`
}
