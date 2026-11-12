package flow

import (
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/itemstatus"
	"github.com/LysCongThanh/heavyq/internal/lease"
)

// completion is when an item was finished inside the window: the first time (what the claim-to-proof lead time
// ends at) and the last (the newest sign of progress, for the stall check).
type completion struct{ first, last time.Time }

// completions are the items finished in [from, now], by every trace a finish leaves, so that a proof run on
// code that has since changed, or by someone who never claimed the item, still counts:
//
//   - the item reads proven now (its proof passes on this code);
//   - a `done` event was recorded (the lead ticked it, with or without a claim);
//   - its gate passed in the ledger (see proofPasses), whatever the item reads like today.
//
// An item is one completion however often it was proved.
func completions(in Input, from time.Time) map[string]completion {
	out := map[string]completion{}
	known := make(map[string]bool, len(in.Items))
	note := func(id string, at time.Time) {
		if !known[id] || at.Before(from) || at.After(in.Now) {
			return
		}
		c, ok := out[id]
		if !ok || at.Before(c.first) {
			c.first = at
		}
		if !ok || at.After(c.last) {
			c.last = at
		}
		out[id] = c
	}
	for _, f := range in.Items {
		known[f.Status.ID] = true
	}
	for _, f := range in.Items {
		if f.Status.State == itemstatus.Proven {
			note(f.Status.ID, f.Status.At)
		}
		for _, at := range proofPasses(f, in.Records) {
			note(f.Status.ID, at)
		}
	}
	for _, e := range in.Leases {
		if e.Op == lease.OpDone {
			note(e.Item, e.At)
		}
	}
	return out
}

// proofPasses are the times the item's gate was whole: on one tree, every command of the gate passed. A cache
// replay is not a run, a run that never ran to its end proves nothing, and a pass is dated by the run that
// completed the gate, so a replay later on the same tree does not move it.
func proofPasses(f ItemFact, recs []history.Record) []time.Time {
	if f.Gate == "" {
		return nil
	}
	want := map[string]bool{}
	for _, c := range f.ProofCmds {
		want[c] = true
	}
	need := max(len(want), 1)
	byTree := map[string]map[string]time.Time{} // tree -> command -> when it first passed there
	for _, r := range recs {
		if r.Gate != f.Gate || r.Tree == "" || r.Subdir != "" || r.Exit != 0 || r.Outcome() != history.OutcomeRan ||
			r.Cache == "hit" || r.Cache == "coalesced" {
			continue
		}
		cmd := r.CmdHash
		if cmd == "" {
			cmd = r.Cmd
		}
		if byTree[r.Tree] == nil {
			byTree[r.Tree] = map[string]time.Time{}
		}
		if prev, ok := byTree[r.Tree][cmd]; !ok || r.End.Before(prev) {
			byTree[r.Tree][cmd] = r.End
		}
	}
	var out []time.Time
	for _, cmds := range byTree {
		if len(cmds) < need {
			continue
		}
		var whole time.Time
		for _, at := range cmds {
			if at.After(whole) {
				whole = at
			}
		}
		out = append(out, whole)
	}
	return out
}
