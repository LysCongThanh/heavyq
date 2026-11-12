package flow

import (
	"testing"

	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/itemstatus"
	"github.com/LysCongThanh/heavyq/internal/lease"
)

func leaseAt(op, item, lane string, min int) lease.Event {
	return lease.Event{Op: op, Item: item, Lane: lane, Session: "s-" + lane, At: at(min)}
}

// Work in flight is not finished work: another lane passing the gate they share, on its own tree, finishes
// neither the item a lane still holds nor the one nobody has touched. Only an item that reads proven (or stale,
// proved once on older code) or was marked done is a completion.
func TestThroughputDoesNotCountItemsInFlightOnASharedGate(t *testing.T) {
	in := Input{Now: at(60), Repo: "/work/app",
		Items: []ItemFact{
			{Status: itemstatus.Status{ID: "wip", State: itemstatus.Claimed, Lane: "a"}, Gate: "fast", ProofCmds: []string{"go test"}},
			{Status: itemstatus.Status{ID: "untouched", State: itemstatus.Open}, Gate: "fast", ProofCmds: []string{"go test"}},
			{Status: itemstatus.Status{ID: "waiting", State: itemstatus.Blocked, Needs: "a key"}, Gate: "fast", ProofCmds: []string{"go test"}},
		},
		Leases:  []lease.Event{leaseAt(lease.OpClaim, "wip", "a", 0)},
		Records: []history.Record{gateRun("fast", "go test", "TB", 5, 60)}}
	tp := throughputOfInput(in)
	if tp.N != 0 || tp.LastProof != nil || tp.P50ClaimToProofS != 0 {
		t.Fatalf("another lane's pass finished nothing: %+v", tp)
	}
	c := Build(in).Items
	if c.Running != 1 || c.Open != 1 || c.Blocked != 1 || c.Proven != 0 {
		t.Fatalf("counts: %+v", c)
	}
}

// The lead time pairs a claim with the finish that followed it, not with the first finish in the window: on a
// shared gate a pass from before the claim (other code, another lane) is not what the claim led to.
func TestLeadTimeIsClaimToTheFinishAfterIt(t *testing.T) {
	in := Input{Now: at(90), Repo: "/work/app",
		Items:  []ItemFact{{Status: itemstatus.Status{ID: "x", State: itemstatus.Proven, At: at(50), Commit: "abc1234"}, Gate: "fast", ProofCmds: []string{"go test"}}},
		Leases: []lease.Event{leaseAt(lease.OpClaim, "x", "a", 20), leaseAt(lease.OpDone, "x", "a", 50)},
		Records: []history.Record{
			gateRun("fast", "go test", "T0", 10, 60), // a pass before the claim, on the code of that time
			gateRun("fast", "go test", "T1", 50, 60),
		}}
	tp := throughputOfInput(in)
	if tp.N != 1 || tp.P50ClaimToProofS != 1800 {
		t.Fatalf("claim at 20, proof at 50: N=%d p50=%v, want 1 and 1800", tp.N, tp.P50ClaimToProofS)
	}
	// The same for an item that reads stale now: its passes on older trees (10 and 50) are both completions, the
	// pair is still 20 -> 50.
	in.Items[0].Status = itemstatus.Status{ID: "x", State: itemstatus.Stale, Done: true, At: at(50)}
	if tp := throughputOfInput(in); tp.N != 1 || tp.P50ClaimToProofS != 1800 {
		t.Fatalf("stale item: N=%d p50=%v, want 1 and 1800", tp.N, tp.P50ClaimToProofS)
	}
}

// A pass of a command the gate no longer has does not make a tree whole: the gate lists one command now, and
// the tree on which only the old command ever passed is not a finish.
func TestGatePassOfADroppedCommandIsNotAFinish(t *testing.T) {
	item := staleDone("a", "ga", "go test", "go vet")
	in := Input{Now: at(60), Repo: "/work/app", Items: []ItemFact{item},
		Records: []history.Record{
			gateRun("ga", "go test", "T1", 10, 60), gateRun("ga", "old lint", "T1", 11, 60), // 2 commands passed, one of them dropped
		}}
	if tp := throughputOfInput(in); tp.N != 0 {
		t.Fatalf("a tree made of an old command is not whole: %+v", tp)
	}
	in.Records = append(in.Records, gateRun("ga", "go vet", "T1", 12, 60))
	if tp := throughputOfInput(in); tp.N != 1 || tp.LastProof == nil || !tp.LastProof.Equal(at(12)) {
		t.Fatalf("both current commands passed on T1: %+v", tp)
	}
}
