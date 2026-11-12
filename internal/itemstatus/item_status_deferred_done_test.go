package itemstatus

import (
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/anchor"
	"github.com/LysCongThanh/heavyq/internal/lease"
)

// An anchor item that `item done` finished without ticking it (parallel work trees, #119) is done by
// its event alone, until a sync event says the box carries it: then the box is the truth again.
func TestDeferredDoneIsDoneUntilSynced(t *testing.T) {
	older := &Run{Commit: "old1234", At: t0.Add(-time.Hour)}
	ev := func(op string, deferred bool) lease.Event {
		return lease.Event{Op: op, Item: "x", Lane: "api", At: t0, Commit: "abc1234", Deferred: deferred}
	}
	item := anchor.Item{ID: "x"}
	derive := func(m Mark, it anchor.Item, p Evidence) Status {
		return Derive(Input{Item: it, Mark: m, Proof: p, Now: t0})
	}
	deferred := Marks([]lease.Event{ev(lease.OpDone, true)})["x"]
	if !deferred.Done || !deferred.Deferred || deferred.Commit != "abc1234" {
		t.Fatalf("mark = %+v", deferred)
	}
	if st := derive(deferred, item, provenEv()); st.State != Proven || !st.Done {
		t.Fatalf("proven and done: %+v", st)
	}
	if st := derive(deferred, item, Evidence{Declared: true, Older: older}); st.State != Stale || !st.Done {
		t.Fatalf("code moved on: %+v", st)
	}
	st := derive(deferred, item, Evidence{Declared: true, Reason: "not run on this code yet: go test"})
	if st.State != ClaimedDoneUnproven || !st.Done || st.Reason != "marked done, but unproven: not run on this code yet: go test" {
		t.Fatalf("never ran here: %+v", st)
	}
	if byName, next := Claimable(st, false); byName || next {
		t.Fatalf("a done item is never picked: %v %v", byName, next)
	}

	synced := Marks([]lease.Event{ev(lease.OpDone, true), ev(lease.OpSync, false)})["x"]
	if !synced.Done || synced.Deferred {
		t.Fatalf("sync: %+v", synced)
	}
	if st := derive(synced, item, provenEv()); st.Done {
		t.Fatalf("an unticked box after a sync is not done (the box counts again): %+v", st)
	}
	if st := derive(synced, anchor.Item{ID: "x", Checked: true}, provenEv()); !st.Done {
		t.Fatalf("a ticked box is done: %+v", st)
	}
	// A done that ticked the box (not deferred) never makes an unticked box done.
	plain := Marks([]lease.Event{ev(lease.OpDone, false)})["x"]
	if st := derive(plain, item, provenEv()); st.Done {
		t.Fatalf("a hand-unticked box reopens the item: %+v", st)
	}
	if st := derive(plain, anchor.Item{ID: "x", ReadOnly: true}, provenEv()); !st.Done {
		t.Fatalf("a plan-file item is done by its event: %+v", st)
	}
	// A claim answers a deferred done; a sync with no done before it is nothing.
	if m := Marks([]lease.Event{ev(lease.OpDone, true), ev(lease.OpClaim, false)}); len(m) != 0 {
		t.Fatalf("claim must clear it: %+v", m)
	}
	if m := Marks([]lease.Event{ev(lease.OpSync, false)}); len(m) != 0 {
		t.Fatalf("sync alone: %+v", m)
	}
}
