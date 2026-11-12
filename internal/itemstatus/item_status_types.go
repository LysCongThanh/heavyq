// Package itemstatus derives the status of a work item from facts, and only
// from facts: the anchor line (declared proof, needs, checkbox), the lease
// store, the events log and the proofs in the run ledger. A ticked box never
// makes an item proven; a proof does.
//
// Everything here is a pure function of its input (no I/O, no clock): callers
// gather the facts (internal/cli) and pass Now.
package itemstatus

import (
	"time"

	"github.com/LysCongThanh/heavyq/internal/anchor"
	"github.com/LysCongThanh/heavyq/internal/lease"
)

// States of an item. The JSON and text output use these words.
const (
	// Open: nobody holds it and nothing proves it.
	Open = "open"
	// Claimed: a live lease holds it (Lane, Expires).
	Claimed = "claimed"
	// Proven: its proof passes on the code it was evaluated on (Commit, At).
	Proven = "proven"
	// Stale: it was proven on an older tree; the code changed since.
	Stale = "stale"
	// Blocked: it needs something a human must provide (Needs).
	Blocked = "blocked"
	// ClaimedDoneUnproven: the box is ticked (or a plan-file item was marked
	// done) but nothing proves it.
	ClaimedDoneUnproven = "claimed_done_unproven"
)

// Status is the derived state of one item.
type Status struct {
	ID    string `json:"id"`
	State string `json:"state"`
	// Lane and Session: the holder of a live lease, or the lane that blocked
	// the item. Proven items keep the lane that still holds them.
	Lane    string    `json:"lane,omitempty"`
	Session string    `json:"session,omitempty"`
	Expires time.Time `json:"expires,omitzero"` // lease expiry, when a live lease holds it
	// Commit (short) and At: when and on which commit the proof passed. For
	// Stale they say where it passed before the code changed.
	Commit string    `json:"commit,omitempty"`
	At     time.Time `json:"at,omitzero"`
	Dirty  bool      `json:"dirty,omitempty"` // the proven code had uncommitted edits
	Needs  string    `json:"needs,omitempty"`
	// Done: the box is ticked, or the done was recorded in the events log only
	// (a plan-file item, or an anchor item whose tick `item sync` still owes).
	// It says nothing about the proof: a done item can be proven, stale or unproven.
	Done bool `json:"done,omitempty"`
	// Reason explains a proof that is not there: no proof declared, it failed
	// on this code, it never ran, or its gate does not exist.
	Reason string `json:"reason,omitempty"`
}

// Input is everything Derive looks at for one item.
type Input struct {
	Item  anchor.Item
	Lease *lease.Lease // the item's lease file, live or expired; nil when none
	Mark  Mark         // what the events log says (block, done)
	Proof Evidence     // the item's proof on the code it was evaluated on
	Now   time.Time
}

// Mark is what the events log says about an item since its last claim.
type Mark struct {
	Blocked bool
	Needs   string // the need given to `item block`
	Lane    string // the lane that blocked or finished it
	Done    bool   // `item done` was recorded (the only record for plan-file items)
	Commit  string // the commit a done was proven on
	At      time.Time
	// Deferred: the done was recorded without ticking the box (`[items] tick`,
	// or parallel work trees) and `item sync` has not ticked it yet: until then
	// the event is the only record of the done of an anchor item.
	Deferred bool
}

// Evidence is the proof state of one item on one tree.
type Evidence struct {
	Declared bool // the item names a proof
	Proven   bool
	Failed   bool // a command of the proof failed on this tree: it is red, not just unproven
	Commit   string
	At       time.Time
	Dirty    bool
	// Older: the proof passed on an older tree of this repository (the code
	// has changed since). nil when it never did, or when it fails now.
	Older  *Run
	Reason string
}

// Run is a passing run of a proof on an earlier tree.
type Run struct {
	Commit string
	At     time.Time
}
