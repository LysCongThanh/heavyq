package flow

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/itemstatus"
	"github.com/LysCongThanh/heavyq/internal/lease"
)

var t0 = time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)

func at(min int) time.Time { return t0.Add(time.Duration(min) * time.Minute) }

func TestBlockSinceAndReasonKeepTheStrongestClass(t *testing.T) {
	evs := []Event{
		{At: at(0), Kind: EventBlocked, Lane: "a", Class: ClassIdlePrompt, Reason: "idle_prompt: waiting"},
		{At: at(1), Kind: EventBlocked, Lane: "a", Class: ClassPermissionRequest, Reason: "permission_request: Bash(rm -rf x)"},
		{At: at(2), Kind: EventBlocked, Lane: "a", Class: ClassPermissionPrompt, Reason: "permission_prompt: later and weaker"},
	}
	b := foldBlocks(evs, nil, at(5))
	if len(b) != 1 || !b[0].Open() || !b[0].Since.Equal(at(0)) || b[0].Reason != "permission_request: Bash(rm -rf x)" {
		t.Fatalf("%+v", b)
	}
}

func TestBlockClearedByEventAndByNextRun(t *testing.T) {
	evs := []Event{
		{At: at(0), Kind: EventBlocked, Lane: "a", Class: ClassPermissionRequest, Reason: "r"},
		{At: at(3), Kind: EventCleared, Lane: "a"},
		{At: at(10), Kind: EventBlocked, Lane: "a", Class: ClassIdlePrompt, Reason: "idle"},
		{At: at(20), Kind: EventBlocked, Lane: "b", Class: ClassIdlePrompt, Reason: "idle"},
	}
	// A run of lane a ended at 14 after waiting 1 minute: the agent went on at 12:30... (start of its wait).
	recs := []history.Record{{Lane: "a", End: at(14), RunSecs: 60, WaitSecs: 60}}
	got := foldBlocks(evs, recs, at(30))
	if len(got) != 3 {
		t.Fatalf("%+v", got)
	}
	if !got[0].Until.Equal(at(3)) {
		t.Errorf("cleared by event: %+v", got[0])
	}
	if want := at(14).Add(-2 * time.Minute); !got[1].Until.Equal(want) {
		t.Errorf("cleared by the next run: until %v, want %v", got[1].Until, want)
	}
	if !got[2].Open() || got[2].Lane != "b" {
		t.Errorf("lane b stays open: %+v", got[2])
	}
}

func TestAbandonedBlockIsDropped(t *testing.T) {
	evs := []Event{{At: at(0), Kind: EventBlocked, Lane: "a", Class: ClassIdlePrompt}}
	if b := foldBlocks(evs, nil, at(0).Add(blockMaxAge+time.Minute)); len(b) != 0 {
		t.Fatalf("a block nobody answered for over a day is a dead session: %+v", b)
	}
}

func TestStopFailureBlocksTheLane(t *testing.T) {
	evs := []Event{{At: at(0), Kind: EventStopFailure, Lane: "a", Detail: "overloaded", Reason: "stop_failure: overloaded"}}
	b := foldBlocks(evs, nil, at(1))
	if len(b) != 1 || b[0].Class != ClassStopFailure || !b[0].Open() {
		t.Fatalf("%+v", b)
	}
}

func TestPaintHigherPriorityWinsAndAdjacentMerge(t *testing.T) {
	got := paint([]Span{{Working, at(0), at(10)}, {Proving, at(2), at(4)}, {Working, at(10), at(12)}, {BlockedHuman, at(3), at(5)}, {Working, at(30), at(30)}})
	want := []Span{{Working, at(0), at(2)}, {Proving, at(2), at(3)}, {BlockedHuman, at(3), at(5)}, {Working, at(5), at(12)}}
	if len(got) != len(want) {
		t.Fatalf("%+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("span %d: %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestSubtract(t *testing.T) {
	got := subtract(interval{at(0), at(10)}, []interval{{at(2), at(3)}, {at(8), at(20)}})
	if len(got) != 2 || got[0] != (interval{at(0), at(2)}) || got[1] != (interval{at(3), at(8)}) {
		t.Fatalf("%+v", got)
	}
}

// Runs and waits are counted once: a block on a lane that holds an item belongs to the item, not to a second session timeline.
func TestBlockedStretchIsCountedOnce(t *testing.T) {
	in := Input{Now: at(60),
		Leases: []lease.Event{{At: at(0), Op: lease.OpClaim, Item: "x", Lane: "a", Session: "s"}},
		Hooks:  []Event{{At: at(10), Kind: EventBlocked, Lane: "a", Session: "s", Class: ClassPermissionRequest, Reason: "r"}},
		Items:  []ItemFact{{Status: itemstatus.Status{ID: "x", State: itemstatus.Claimed, Lane: "a", Session: "s"}}}}
	tls := Timelines(in)
	if len(tls) != 1 || tls[0].Kind != KindItem {
		t.Fatalf("want one item timeline: %+v", tls)
	}
	snap := Build(in)
	if got := snap.Fleet.ByStateS[BlockedHuman]; got != 50*60 {
		t.Errorf("blocked seconds %v, want 3000 (counted once)", got)
	}
	if len(snap.NeedsYou) != 1 || snap.NeedsYou[0].Item != "x" {
		t.Errorf("a hook block names the item its lane holds: %+v", snap.NeedsYou)
	}
}

func TestRunsWithoutItemTagAttachToTheItemTheLaneHeld(t *testing.T) {
	in := Input{Now: at(60),
		Leases:  []lease.Event{{At: at(0), Op: lease.OpClaim, Item: "x", Lane: "a", Session: "s"}, {At: at(30), Op: lease.OpRelease, Item: "x", Lane: "a", Session: "s"}},
		Records: []history.Record{{End: at(10), Lane: "a", RunSecs: 60, Exit: 0, Cmd: "make"}}}
	tls := Timelines(in)
	if len(tls) != 1 || tls[0].Key != "x" {
		t.Fatalf("%+v", tls)
	}
}

func TestSessionKeyFallsBackToLane(t *testing.T) {
	in := Input{Now: at(60), Records: []history.Record{{End: at(10), Lane: "misc", RunSecs: 60, Cmd: "make"}}}
	tls := Timelines(in)
	if len(tls) != 1 || tls[0].Kind != KindSession || tls[0].Key != "lane:misc" {
		t.Fatalf("%+v", tls)
	}
}

func TestConstraintIsTheLargestNonWorkingShareAndSignificantAtThirtyPercent(t *testing.T) {
	f := Fleet{LeadS: 100, ByStateS: map[State]float64{Working: 60, Queued: 29, Rework: 11}}
	if c := constraintOf(f, nil); c.State != Queued || c.Significant || c.Share != 0.29 {
		t.Errorf("working is never the constraint, and 29%% is not news: %+v", c)
	}
	f = Fleet{LeadS: 100, ByStateS: map[State]float64{Working: 40, CIWait: 30, Queued: 30}}
	if c := constraintOf(f, nil); c.State != CIWait || !c.Significant {
		t.Errorf("a tie goes to the state of higher priority; 30%% is significant: %+v", c)
	}
	if c := constraintOf(Fleet{}, nil); c.State != "" || c.Significant {
		t.Errorf("no lead time, no constraint: %+v", c)
	}
}

func TestExternalSpansArePaintedIntoTheItemTimeline(t *testing.T) {
	in := Input{Now: at(60),
		Leases:   []lease.Event{{At: at(0), Op: lease.OpClaim, Item: "x", Lane: "a", Session: "s"}, {At: at(10), Op: lease.OpDone, Item: "x", Lane: "a", Session: "s"}},
		External: []External{{Item: "x", Span: Span{CIWait, at(10), at(40)}}}}
	snap := Build(in)
	if snap.Fleet.ByStateS[CIWait] != 30*60 || snap.Constraint.State != CIWait || !snap.Constraint.Significant {
		t.Fatalf("a collector's ci_wait must reach the fleet and the constraint: %+v %+v", snap.Fleet, snap.Constraint)
	}
}

func TestThroughputCountsProvenItemsPerActiveHourWithMedianClaimToProof(t *testing.T) {
	mk := func(id string, claim, proof int) (lease.Event, ItemFact) {
		return lease.Event{At: at(claim), Op: lease.OpClaim, Item: id, Lane: "a", Session: "s"},
			ItemFact{Status: itemstatus.Status{ID: id, State: itemstatus.Proven, At: at(proof)}}
	}
	var evs []lease.Event
	var facts []ItemFact
	for _, c := range [][3]interface{}{{"a", 0, 10}, {"b", 10, 40}, {"c", 40, 100}} {
		e, f := mk(c[0].(string), c[1].(int), c[2].(int))
		evs = append(evs, e, lease.Event{At: f.Status.At, Op: lease.OpDone, Item: e.Item, Lane: "a", Session: "s"})
		facts = append(facts, f)
	}
	facts = append(facts, ItemFact{Status: itemstatus.Status{ID: "old", State: itemstatus.Proven, At: at(-600)}}) // outside the window
	snap := Build(Input{Now: at(120), Leases: evs, Items: facts})
	tp := snap.Throughput
	if tp.N != 3 || tp.P50ClaimToProofS != 30*60 || tp.WindowS != 4*3600 {
		t.Fatalf("%+v", tp)
	}
	if tp.LastProof == nil || !tp.LastProof.Equal(at(100)) {
		t.Errorf("last_proof is the newest proof in the window: %v", tp.LastProof)
	}
	if want := 3.0 / 1.6666666666666667; tp.ProvenPerHour < want-0.01 || tp.ProvenPerHour > want+0.01 { // 100 active minutes
		t.Errorf("proven per active hour %v, want %v", tp.ProvenPerHour, want)
	}
	if snap.Items.Proven != 4 || snap.Items.Total != 4 {
		t.Errorf("%+v", snap.Items)
	}
}

// The materialised document is a contract other agents read: keys and null/empty conventions are fixed.
func TestSnapshotJSONShapeIsTheContract(t *testing.T) {
	snap := Build(Input{Now: at(0), Repo: "/r"})
	b, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"schema", "repo", "updated", "goal", "run", "items", "needs_you", "constraint", "throughput", "lanes", "session"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing key %q in %s", k, b)
		}
	}
	if string(m["run"]) != "null" || string(m["session"]) != "null" || string(m["needs_you"]) != "[]" {
		t.Errorf("run and session are null and needs_you is an empty array: %s", b)
	}
	var tp map[string]json.RawMessage
	_ = json.Unmarshal(m["throughput"], &tp)
	if string(tp["last_proof"]) != "null" {
		t.Errorf("last_proof is null without a proof: %s", tp["last_proof"])
	}
	for _, k := range []string{"proven_per_hour", "p50_claim_to_proof_s", "window_s", "n", "last_proof"} {
		if _, ok := tp[k]; !ok {
			t.Errorf("throughput lacks %q", k)
		}
	}
}
