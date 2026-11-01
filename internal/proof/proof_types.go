// Package proof answers one question: has this exact code passed this
// command? It is derived entirely from the run ledger. There is no separate
// "proof" store to trust or to drift: a proof is a ledger record.
//
// A record proves a gate command when all of these hold:
//   - same repository, and the same tree id as the code being asked about
//     (an unknown tree, "", is never equal to anything);
//   - it ran the exact command heavyq runs for the gate (`sh -c <command>`),
//     from the work tree root;
//   - it ran to its end (no gave_up, cancelled or interrupted result) with
//     exit 0. A cache hit or a coalesced run counts, because the ledger only
//     gives those a tree id when the result belongs to that tree.
//
// Of the matching, conclusive records the latest one decides: a command that
// passed and then failed on the same tree is failed (flaky tests do not get a
// green check). Everything not provable from the ledger is "missing".
package proof

import "time"

// State is the proof state of one gate command.
type State string

// States of a gate command.
const (
	Proven  State = "proven"  // the latest conclusive run on this tree passed
	Failed  State = "failed"  // the latest conclusive run on this tree failed
	Missing State = "missing" // no usable run on this tree (or it cannot be told)
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
	At      time.Time `json:"at,omitzero"`       // end of the deciding run
	Lane    string    `json:"lane,omitempty"`    // lane of the deciding run
	RunSecs float64   `json:"run_secs"`          // run time of the deciding run (0 for a cache hit)
	Cache   string    `json:"cache,omitempty"`   // hit | coalesced | miss | skip | ""
	Exit    *int      `json:"exit,omitempty"`    // exit code of the deciding run
	Machine string    `json:"machine,omitempty"` // where the deciding run ran
	Reason  string    `json:"reason,omitempty"`  // why it is missing, when known
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
