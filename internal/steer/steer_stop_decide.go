package steer

import (
	"sort"
	"strings"
)

// Event is the hook event heavyq answers. One command serves all four; the
// event says which rules apply.
type Event string

// The events of the stop family.
const (
	EventStop          Event = "Stop"
	EventSubagentStop  Event = "SubagentStop"
	EventTaskCompleted Event = "TaskCompleted"
	EventTeammateIdle  Event = "TeammateIdle"
)

// ParseEvent accepts exactly the four names.
func ParseEvent(s string) (Event, bool) {
	switch e := Event(s); e {
	case EventStop, EventSubagentStop, EventTaskCompleted, EventTeammateIdle:
		return e, true
	}
	return "", false
}

// GuardBlockCap is how many times guard mode blocks one session. After that
// the agent may stop, with a note.
const GuardBlockCap = 3

// Action is what the hook does.
type Action int

// The outcomes of DecideStop.
const (
	// Allow: say nothing (or only Note), let the agent stop.
	Allow Action = iota
	// Block: keep the agent working; Text is the reason it is given.
	Block
	// Continue: the non-blocking claim-vs-ledger continuation; Text is the
	// context the agent gets once.
	Continue
)

// StopInput is everything DecideStop looks at.
type StopInput struct {
	Event Event
	Mode  Mode
	// StopHookActive is the hook input's flag: Claude Code is already continuing
	// because a stop hook asked it to.
	StopHookActive bool
	LastMessage    string // last_assistant_message (Stop, SubagentStop)
	Facts          Facts
	State          SessionState // the session record before this decision
}

// StopDecision is the outcome, and the session record to store with it.
type StopDecision struct {
	Action Action
	Text   string // block reason or continuation context
	Note   string // for the user, when a block is released
	State  SessionState
	Why    string // one line for hook.log
}

// blocker is an unproven item of this lane (or the stale gate) and the exact
// command that resolves it.
type blocker struct {
	id   string // item id; "" for the gate
	why  string
	next string
}

// DecideStop is the stop gate: a pure function of the facts and the session
// record.
//
//   - off: allow.
//   - guard gates Stop only; destroy gates all four events.
//   - A blocker is an item this lane holds that its proof does not prove on this
//     tree. Blocked and released items are not held, so they never block. An
//     item whose proof was red before the session began (the baseline) never
//     blocks, nor does a red item when no baseline was recorded.
//   - guard: at most GuardBlockCap blocks per session, and never again on an
//     unchanged situation while stop_hook_active is set. destroy has no cap.
//   - With nothing to block on, a final message that says the tests pass while
//     the ledger shows a plain failure on this tree gets one non-blocking
//     continuation per session.
func DecideStop(in StopInput) StopDecision {
	d := StopDecision{State: in.State}
	if in.Mode == Off {
		d.Why = "steer is off"
		return d
	}
	if in.Event == EventStop || in.Mode == Destroy {
		if bl := blockers(in); len(bl) > 0 {
			return decideBlock(in, d, bl)
		}
	}
	if in.Event == EventStop || in.Event == EventSubagentStop {
		if !in.StopHookActive && !in.State.ClaimNudged && ClaimsDone(in.LastMessage) {
			if c, next, ok := contradiction(in); ok {
				d.Action, d.State.ClaimNudged = Continue, true
				d.Text = claimText(c, next)
				d.Why = "claim contradicted by the ledger: " + c.Cmd
				return d
			}
		}
	}
	d.Why = "nothing to prove"
	return d
}

func decideBlock(in StopInput, d StopDecision, bl []blocker) StopDecision {
	sig := signature(in.Facts, bl)
	if in.Mode == Guard {
		switch {
		case in.StopHookActive && in.State.LastSig == sig:
			d.Why = "stop_hook_active and nothing changed since the last block"
			return d
		case in.State.Blocks >= GuardBlockCap:
			d.Note = releaseNote(bl)
			d.Why = "guard cap reached"
			return d
		}
	}
	d.Action = Block
	d.State.Blocks++
	d.State.LastSig = sig
	d.Text = blockText(in, bl, d.State.Blocks)
	d.Why = "blocked: " + blockerIDs(bl)
	return d
}

// blockers lists what keeps this lane from stopping.
func blockers(in StopInput) []blocker {
	f, st := in.Facts, in.State
	var out []blocker
	held := false
	for _, it := range f.Items {
		if it.Lane != f.Lane || it.Lane == "" {
			continue
		}
		switch it.State {
		case StateProven:
			held = true
			continue
		case StateClaimed, StateDoneUnproven, StateStale:
			held = true
		default:
			continue
		}
		if it.NoProof || preExisting(it.Failing, st) {
			continue
		}
		if len(it.Failing) > 0 && !st.HasBaseline {
			continue // red, and nobody recorded whose fault: do not guess
		}
		out = append(out, blocker{id: it.ID, why: itemWhy(it), next: "heavyq item done " + it.ID + " --run"})
	}
	if len(out) == 0 && held {
		if g := f.Gate; g != nil && !g.Proven && g.Older && len(g.Failing) == 0 {
			out = append(out, blocker{why: "the code changed since gate " + g.Name + " last passed", next: "heavyq gate run " + g.Name})
		}
	}
	return out
}

// preExisting: a command of the proof was already failing at session start, so
// the proof cannot pass until someone fixes what this session did not break.
func preExisting(failing []FailedCmd, st SessionState) bool {
	for _, c := range failing {
		if st.IsBaseline(c.Cmd) {
			return true
		}
	}
	return false
}

func itemWhy(it ItemFact) string {
	if len(it.Failing) > 0 {
		c := it.Failing[0]
		return "failed on this code: " + oneLine(c.Cmd, 80) + " (exit " + itoa(c.Exit) + ")"
	}
	if it.Reason != "" {
		return oneLine(it.Reason, 100)
	}
	return "not proven on this tree"
}

// contradiction finds a plain failure (not flaky, not under load) on this tree
// that is not pre-existing: in the gate, else in an item this lane holds.
func contradiction(in StopInput) (FailedCmd, string, bool) {
	f, st := in.Facts, in.State
	if g := f.Gate; g != nil {
		for _, c := range g.Failing {
			if c.Class == "failed" && !st.IsBaseline(c.Cmd) {
				return c, "heavyq gate run " + g.Name, true
			}
		}
	}
	for _, it := range f.Items {
		if it.Lane != f.Lane || it.Lane == "" {
			continue
		}
		for _, c := range it.Failing {
			if c.Class == "failed" && !st.IsBaseline(c.Cmd) {
				return c, "heavyq item done " + it.ID + " --run", true
			}
		}
	}
	return FailedCmd{}, "", false
}

// signature identifies a situation: the code and the set of blockers.
func signature(f Facts, bl []blocker) string {
	ids := make([]string, 0, len(bl))
	for _, b := range bl {
		ids = append(ids, b.id+"|"+b.why)
	}
	sort.Strings(ids)
	return f.Tree + "#" + strings.Join(ids, ",")
}

func blockerIDs(bl []blocker) string {
	ids := make([]string, 0, len(bl))
	for _, b := range bl {
		if b.id == "" {
			ids = append(ids, "gate")
		} else {
			ids = append(ids, b.id)
		}
	}
	return strings.Join(ids, ",")
}
