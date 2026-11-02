// Package flow derives how work moves: a timeline of states per work item (else
// per session span), fleet aggregates, throughput and the current constraint.
//
// Everything here is a pure function of its input (no I/O, no clock): callers
// gather the facts from the ledger, the lease events log, itemstatus and the
// hook events (internal/cli) and pass Now. Timelines are derived, never
// declared: nothing writes a state, so a missing source only drops its states
// (an offline or gh-less setup still works). The materialised state.json is
// written by internal/flowstate; the design is in the autonomy-loop plan, §3.1.
package flow

import (
	"sort"
	"time"
)

// State is one state of a timeline.
type State string

// The states of a timeline. The words are API: state.json and the status line
// use them.
const (
	Queued       State = "queued"        // waiting in the heavyq queue (ledger wait_s)
	Working      State = "working"       // a lease is held; edits and runs between proofs
	Proving      State = "proving"       // a run of the item's proof or gate
	Rework       State = "rework"        // from a failed proof to the next proof attempt
	BlockedHuman State = "blocked_human" // `item block`, a permission or idle prompt, waiting on a person
	// CIWait, MergeWait and ConflictRework come from gh and git. This package
	// never calls either: an optional collector supplies them as Input.External.
	CIWait         State = "ci_wait"         // PR head checks pending
	MergeWait      State = "merge_wait"      // checks green, not merged
	ConflictRework State = "conflict_rework" // merge-main commits on the branch, conflict files
	// DoneProven and ClaimedUnproven end a timeline: they carry no duration.
	DoneProven      State = "done_proven"
	ClaimedUnproven State = "claimed_unproven"
)

// priority orders states where candidate spans overlap: the higher wins, so
// waiting on a person beats everything and plain working loses to all.
var priority = map[State]int{
	BlockedHuman: 8, Proving: 7, ConflictRework: 6, CIWait: 5, MergeWait: 4, Rework: 3, Queued: 2, Working: 1,
}

// IsTerminal reports whether s ends a timeline (no duration, no share of lead time).
func (s State) IsTerminal() bool { return s == DoneProven || s == ClaimedUnproven }

// Span is [Start, End) in one state. End is not before Start.
type Span struct {
	State State
	Start time.Time
	End   time.Time
}

// Duration is the length of the span.
func (s Span) Duration() time.Duration { return s.End.Sub(s.Start) }

// Timeline kinds.
const (
	KindItem    = "item"    // a work item (anchor line)
	KindSession = "session" // runs no item claimed: an agent session (else a lane)
)

// Timeline is the derived history of one unit of work, spans in time order and
// without overlap.
type Timeline struct {
	Kind    string
	Key     string // item id; session id, else "lane:<lane>"
	Lane    string
	Session string
	Spans   []Span
	// Final is DoneProven or ClaimedUnproven when the item's status says so, else "".
	Final State
	// FinalAt is when the proof passed (DoneProven), else when the claim was made.
	FinalAt time.Time
}

// Current is the state at now, and since when. ok is false when the timeline is
// not active at now: its last span ended more than idle ago.
func (t Timeline) Current(now time.Time, idle time.Duration) (State, time.Time, bool) {
	if len(t.Spans) == 0 {
		return "", time.Time{}, false
	}
	last := t.Spans[len(t.Spans)-1]
	if now.Sub(last.End) > idle {
		return "", time.Time{}, false
	}
	return last.State, last.Start, true
}

// sortSpans orders by start, then by end.
func sortSpans(s []Span) {
	sort.SliceStable(s, func(i, j int) bool {
		if !s[i].Start.Equal(s[j].Start) {
			return s[i].Start.Before(s[j].Start)
		}
		return s[i].End.Before(s[j].End)
	})
}
