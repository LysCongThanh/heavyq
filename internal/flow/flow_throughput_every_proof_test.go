package flow

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/itemstatus"
	"github.com/LysCongThanh/heavyq/internal/lease"
)

// staleDone is an item whose box is ticked and whose proof passed on code that has changed since.
func staleDone(id, gate string, cmds ...string) ItemFact {
	return ItemFact{Status: itemstatus.Status{ID: id, State: itemstatus.Stale, Done: true, Commit: "old1234", At: at(5)}, Gate: gate, ProofCmds: cmds}
}

// gateRun is one passing command of a gate on a tree: what `heavyq gate run` leaves in the ledger.
func gateRun(gate, cmd, tree string, end int, runSecs float64) history.Record {
	return history.Record{End: at(end), Lane: "lead", Gate: gate, Cmd: cmd, CmdHash: "h-" + cmd, Tree: tree, Repo: "r", RunSecs: runSecs, Exit: 0}
}

func throughputOfInput(in Input) Throughput { return Build(in).Throughput }

// Proofs that passed on code which has changed since (the item reads stale) are still proofs: three items proved
// one after the other, none ever claimed, count three. Only the claim-to-proof median needs a claim.
func TestThroughputCountsProofPassesOfStaleItemsWithoutAClaim(t *testing.T) {
	in := Input{Now: at(60), Repo: "/work/app",
		Items: []ItemFact{staleDone("a", "ga", "go test a"), staleDone("b", "gb", "go test b"), staleDone("c", "gc", "go test c")},
		Records: []history.Record{
			gateRun("ga", "go test a", "T1", 10, 120), gateRun("gb", "go test b", "T1", 20, 120), gateRun("gc", "go test c", "T2", 30, 120),
		}}
	tp := throughputOfInput(in)
	if tp.N != 3 {
		t.Fatalf("three proof passes inside the window, N = %d", tp.N)
	}
	if tp.LastProof == nil || !tp.LastProof.Equal(at(30)) {
		t.Errorf("last proof = %v, want the newest pass", tp.LastProof)
	}
	if tp.ProvenPerHour <= 0 {
		t.Errorf("proven per hour = %v: the proofs ran for six minutes, so there is a rate", tp.ProvenPerHour)
	}
	if tp.P50ClaimToProofS != 0 {
		t.Errorf("no claim, no claim-to-proof pair: p50 = %v", tp.P50ClaimToProofS)
	}
}

// A done event is a completion too, with or without a gate to find in the ledger.
func TestThroughputCountsDoneEventsAndKeepsTheClaimMedianToPairs(t *testing.T) {
	ev := func(op, item string, min int) lease.Event {
		return lease.Event{Op: op, Item: item, Lane: "lead", Session: "s", At: at(min)}
	}
	in := Input{Now: at(90), Repo: "/work/app",
		Items: []ItemFact{staleDone("claimed", ""), staleDone("lone-1", ""), staleDone("lone-2", "")},
		Leases: []lease.Event{
			ev(lease.OpClaim, "claimed", 0), ev(lease.OpDone, "claimed", 30), // a pair: 30 minutes
			ev(lease.OpDone, "lone-1", 50), ev(lease.OpDone, "lone-2", 70), // proved on the lead's say, never claimed
		}}
	tp := throughputOfInput(in)
	if tp.N != 3 {
		t.Fatalf("three done events, N = %d", tp.N)
	}
	if tp.P50ClaimToProofS != 1800 {
		t.Errorf("the median is of claim-to-proof pairs only: %v", tp.P50ClaimToProofS)
	}
	if tp.LastProof == nil || !tp.LastProof.Equal(at(70)) {
		t.Errorf("last proof = %v", tp.LastProof)
	}
}

// One item is one completion however many times it was proved; a gate of two commands is passed only when both
// passed on the same tree; a pass before the window is not in it.
func TestThroughputCountsItemsOnceAndWholeGatesOnly(t *testing.T) {
	two := staleDone("two", "g2", "cmd-1", "cmd-2")
	half := staleDone("half", "g3", "cmd-1", "cmd-2")
	old := staleDone("old", "g4", "cmd-1")
	in := Input{Now: at(330), Window: 4 * time.Hour, Repo: "/work/app",
		Items: []ItemFact{two, half, old},
		Records: []history.Record{
			gateRun("g2", "cmd-1", "T1", 300, 60), gateRun("g2", "cmd-2", "T1", 301, 60), // whole on T1
			gateRun("g2", "cmd-1", "T2", 310, 60), gateRun("g2", "cmd-2", "T2", 311, 60), // and again on T2: still one item
			gateRun("g3", "cmd-1", "T1", 302, 60), // the other command never passed on T1
			gateRun("g3", "cmd-2", "T2", 303, 60), // nor did the first on T2: no tree has both
			gateRun("g4", "cmd-1", "T0", 10, 60),  // passed five hours before now
		}}
	if tp := throughputOfInput(in); tp.N != 1 {
		t.Fatalf("only the whole gate counts, once: N = %d", tp.N)
	}
}

// The counts say where done-and-stale items stand: not "open" (nobody is to start them over), not "claimed,
// unproven" (something did prove them): they need one more proof run. A stale item that is not ticked is open.
func TestItemCountsSeparateDoneStaleFromOpen(t *testing.T) {
	st := func(id, state string, done bool) ItemFact {
		return ItemFact{Status: itemstatus.Status{ID: id, State: state, Done: done}}
	}
	in := Input{Now: at(60), Items: []ItemFact{
		st("a", itemstatus.Stale, true), st("b", itemstatus.Stale, true), st("c", itemstatus.Stale, false),
		st("d", itemstatus.Open, false), st("e", itemstatus.ClaimedDoneUnproven, true), st("f", itemstatus.Proven, true),
	}}
	b, err := json.Marshal(Build(in).Items)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]int
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"total": 6, "proven": 1, "running": 0, "blocked": 0, "claimed_unproven": 1, "done_stale": 2, "open": 2}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("items.%s = %d, want %d (%s)", k, got[k], v, b)
		}
	}
}
