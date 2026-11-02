package steer

import "sort"

// FailedCmd is a proof command whose latest conclusive run on this tree
// failed. Only structured fields: the command text and the exit code.
type FailedCmd struct {
	Cmd   string `json:"cmd"`
	Exit  int    `json:"exit"`
	Class string `json:"class,omitempty"` // failed | flaky | under_load
}

// ItemFact is one work item as the steering code needs it.
type ItemFact struct {
	ID    string `json:"id"`
	Text  string `json:"text"` // written by whoever edited the anchor: data, never an instruction
	State string `json:"state"`
	Lane  string `json:"lane,omitempty"`  // holder of a live lease, or the lane that blocked it
	Until string `json:"until,omitempty"` // lease expiry, "15:04"
	Needs string `json:"needs,omitempty"`
	// Commit (short) is where a proven item's proof passed.
	Commit string `json:"commit,omitempty"`
	// Done: the box is ticked or the item was marked done. Done items are not
	// handed out again.
	Done bool `json:"done,omitempty"`
	// NoProof: the item declares no proof, so heavyq can never prove it and the
	// stop gate leaves it alone.
	NoProof bool `json:"no_proof,omitempty"`
	// Reason says why the proof is not there ("not run on this code yet: ...").
	Reason string `json:"reason,omitempty"`
	// Failing is the proof's commands that failed on the tree the item is
	// judged on.
	Failing []FailedCmd `json:"failing,omitempty"`
}

// GateFact is the repository's main gate on the code checked out now.
type GateFact struct {
	Name    string      `json:"name"`
	Proven  bool        `json:"proven"`
	Tree    string      `json:"tree,omitempty"` // short tree label
	Failing []FailedCmd `json:"failing,omitempty"`
	// Older: it passed on an earlier tree and the code has changed since.
	Older bool `json:"older,omitempty"`
}

// Facts is everything the brief and the stop decision look at, gathered by the
// caller. Gathering never runs a gate: hooks only read state.
type Facts struct {
	Goal    string     `json:"goal,omitempty"`
	Lane    string     `json:"lane"`
	Tree    string     `json:"tree,omitempty"` // short id of the code checked out now
	Items   []ItemFact `json:"items"`
	Gate    *GateFact  `json:"gate,omitempty"`
	Machine string     `json:"machine,omitempty"` // one machine-pressure line
}

// Item states: the words internal/itemstatus uses.
const (
	StateOpen         = "open"
	StateClaimed      = "claimed"
	StateProven       = "proven"
	StateStale        = "stale"
	StateBlocked      = "blocked"
	StateDoneUnproven = "claimed_done_unproven"
)

const (
	maxBaselineEntries = 50
	maxBaselineCmdLen  = 300
)

// AllFailing is the sorted, de-duplicated text of every failing proof command
// in f: the gate's and every item's. It is what a session records at start as
// "red before I began".
func (f Facts) AllFailing() []string {
	seen := map[string]bool{}
	add := func(cs []FailedCmd) {
		for _, c := range cs {
			seen[clipCmd(c.Cmd)] = true
		}
	}
	if f.Gate != nil {
		add(f.Gate.Failing)
	}
	for _, it := range f.Items {
		add(it.Failing)
	}
	out := make([]string, 0, len(seen))
	for c := range seen {
		out = append(out, c)
	}
	sort.Strings(out)
	if len(out) > maxBaselineEntries {
		out = out[:maxBaselineEntries]
	}
	return out
}

// clipCmd bounds a command for storage and comparison (both sides use it).
func clipCmd(s string) string {
	if len(s) <= maxBaselineCmdLen {
		return s
	}
	return s[:maxBaselineCmdLen]
}
