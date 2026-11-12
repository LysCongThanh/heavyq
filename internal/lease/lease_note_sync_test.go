package lease

import "testing"

func TestNoteSyncAppendsOneEventUnderTheLockAndTouchesNoLease(t *testing.T) {
	s := newStore(t)
	a, b := owner(t, "laneA", "s1"), owner(t, "main", "")
	if _, err := s.Claim("task-2", a); err != nil { // someone else's live lease stays
		t.Fatal(err)
	}
	if err := s.Finish("task-1", a, Event{Op: OpDone, Commit: "abc1234", Deferred: true}, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.WithLock(func() error { return s.NoteSync("task-1", b, "abc1234") }); err != nil {
		t.Fatal(err)
	}
	evs := wantOps(t, s, OpClaim, OpDone, OpSync)
	if !evs[1].Deferred || evs[2].Deferred {
		t.Fatalf("only the done is deferred: %+v", evs)
	}
	if e := evs[2]; e.Item != "task-1" || e.Lane != "main" || e.Commit != "abc1234" || e.At.IsZero() {
		t.Fatalf("sync event = %+v", e)
	}
	if ls, _ := s.List(); len(ls) != 1 || ls[0].Item != "task-2" {
		t.Fatalf("leases = %+v", ls)
	}
	if err := s.NoteSync("../escape", b, ""); err == nil {
		t.Fatal("a bad item id must be refused")
	}
	if err := s.NoteSync("task-1", Owner{}, ""); err == nil {
		t.Fatal("an owner without a lane must be refused")
	}
	wantOps(t, s, OpClaim, OpDone, OpSync) // the refusals wrote nothing
}
