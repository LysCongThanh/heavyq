package flow

import (
	"sort"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/itemstatus"
	"github.com/LysCongThanh/heavyq/internal/lease"
)

// completion is when an item was finished inside the window: every time it was, oldest first. The newest is the
// latest sign of progress (the stall check); the lead time ends at the first one after the claim it is paired with.
type completion struct{ ats []time.Time }

// last is the newest finish.
func (c completion) last() time.Time { return c.ats[len(c.ats)-1] }

// firstAfter is the first finish at or after t.
func (c completion) firstAfter(t time.Time) (time.Time, bool) {
	for _, at := range c.ats {
		if !at.Before(t) {
			return at, true
		}
	}
	return time.Time{}, false
}

// completions are the items finished in [from, now], by every trace a finish leaves, so that a proof run on
// code that has since changed, or by someone who never claimed the item, still counts:
//
//   - the item reads proven now (its proof passes on this code);
//   - a `done` event was recorded (the lead ticked it, with or without a claim);
//   - the item reads stale (its proof passed on code that has since changed) and its gate passed in the ledger
//     (see proofPasses).
//
// Only a stale item is judged by the passes of its gate on any tree. A claimed, open or blocked item is not
// finished, whatever else passed the same gate: on a gate several lanes share, another lane's pass on its own
// tree is not this item's proof, and counting it would finish work still in flight. An item is one completion
// however often it was proved.
func completions(in Input, from time.Time) map[string]completion {
	out := map[string]completion{}
	known := make(map[string]bool, len(in.Items))
	note := func(id string, at time.Time) {
		if !known[id] || at.Before(from) || at.After(in.Now) {
			return
		}
		c := out[id]
		c.ats = append(c.ats, at)
		out[id] = c
	}
	for _, f := range in.Items {
		known[f.Status.ID] = true
	}
	for _, f := range in.Items {
		switch f.Status.State {
		case itemstatus.Proven:
			note(f.Status.ID, f.Status.At)
		case itemstatus.Stale:
			for _, at := range proofPasses(f, in.Records) {
				note(f.Status.ID, at)
			}
		}
	}
	for _, e := range in.Leases {
		if e.Op == lease.OpDone {
			note(e.Item, e.At)
		}
	}
	for id, c := range out {
		sort.Slice(c.ats, func(i, j int) bool { return c.ats[i].Before(c.ats[j]) })
		out[id] = c
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
	for _, h := range f.ProofHashes {
		want[h] = true
	}
	need := max(len(want), 1)
	byTree := map[string]map[string]time.Time{} // tree -> command -> when it first passed there
	for _, r := range recs {
		if r.Gate != f.Gate || r.Tree == "" || r.Subdir != "" || r.Exit != 0 || r.Outcome() != history.OutcomeRan ||
			r.Cache == "hit" || r.Cache == "coalesced" || (len(want) > 0 && !want[r.CmdHash]) {
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
