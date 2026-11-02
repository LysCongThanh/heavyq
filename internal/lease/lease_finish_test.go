package lease

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestFinishDoneReleasesAndLogs(t *testing.T) {
	s := newStore(t)
	a := owner(t, "laneA", "s1")
	if _, err := s.Claim("task-1", a); err != nil {
		t.Fatal(err)
	}
	ran := false
	err := s.Finish("task-1", a, Event{Op: OpDone, Commit: "abc1234"}, func() error { ran = true; return nil })
	if err != nil || !ran {
		t.Fatalf("finish = %v, ran %v", err, ran)
	}
	if ls, _ := s.List(); len(ls) != 0 {
		t.Fatalf("lease still there: %+v", ls)
	}
	evs := wantOps(t, s, OpClaim, OpDone)
	if e := evs[1]; e.Item != "task-1" || e.Lane != "laneA" || e.Session != "s1" || e.Commit != "abc1234" || e.At.IsZero() {
		t.Fatalf("done event = %+v", e)
	}
}

func TestFinishRefusesLiveForeignHolderAndKeepsLeaseOnFnError(t *testing.T) {
	s := newStore(t)
	a, b := owner(t, "laneA", "s1"), owner(t, "laneB", "s2")
	if _, err := s.Claim("task-1", a); err != nil {
		t.Fatal(err)
	}
	ran := false
	err := s.Finish("task-1", b, Event{Op: OpBlock, Needs: "x"}, func() error { ran = true; return nil })
	if !errors.Is(err, ErrHeld) || ran {
		t.Fatalf("foreign finish = %v, ran %v", err, ran)
	}
	boom := errors.New("boom")
	if err := s.Finish("task-1", a, Event{Op: OpDone}, func() error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("fn error = %v", err)
	}
	if ls, _ := s.List(); len(ls) != 1 {
		t.Fatalf("lease must survive a failed fn: %+v", ls)
	}
	wantOps(t, s, OpClaim) // no event for a failed finish
	if err := s.Finish("task-1", a, Event{Op: "claim"}, nil); err == nil {
		t.Fatal("op claim must be refused")
	}
}

func TestFinishTakesStaleAndMissingLeases(t *testing.T) {
	s := newStore(t)
	now := time.Now()
	s.now = func() time.Time { return now }
	if _, err := s.Claim("task-1", owner(t, "laneA", "s1")); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * DefaultTTL)
	if err := s.Finish("task-1", owner(t, "laneB", "s2"), Event{Op: OpBlock, Needs: "a human"}, nil); err != nil {
		t.Fatalf("stale holder must not stop a finish: %v", err)
	}
	if err := s.Finish("nobody-has-this", owner(t, "laneB", "s2"), Event{Op: OpDone}, nil); err != nil {
		t.Fatalf("no lease: %v", err)
	}
	evs := wantOps(t, s, OpClaim, OpBlock, OpDone)
	if evs[1].Needs != "a human" {
		t.Fatalf("block event = %+v", evs[1])
	}
}

func TestEventsReadsTailAndSkipsGarbage(t *testing.T) {
	s := newStore(t)
	if evs, err := s.Events(1 << 20); err != nil || len(evs) != 0 {
		t.Fatalf("missing log = %v, %v", evs, err)
	}
	a := owner(t, "laneA", "s1")
	for _, id := range []string{"i-1", "i-2", "i-3"} {
		if _, err := s.Claim(id, a); err != nil {
			t.Fatal(err)
		}
	}
	f, err := os.OpenFile(s.eventsPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("not json\n{\"op\":\"claim\"}\n"); err != nil { // the second line has no item: not an event
		t.Fatal(err)
	}
	f.Close()
	all, err := s.Events(0)
	if err != nil || len(all) != 3 {
		t.Fatalf("all = %d, %v", len(all), err)
	}
	b, _ := os.ReadFile(s.eventsPath)
	lines := strings.SplitAfter(string(b), "\n")
	window := int64(len(lines[2]) + len(lines[3]) + len(lines[4]) + 5) // cuts into the second line
	tail, err := s.Events(window)
	if err != nil || len(tail) != 1 || tail[0].Item != "i-3" {
		t.Fatalf("tail = %+v, %v", tail, err)
	}
}

func TestRenewLaneExtendsOnlyThatLane(t *testing.T) {
	s := newStore(t)
	now := time.Now()
	s.now = func() time.Time { return now }
	a, b := owner(t, "laneA", "s1"), owner(t, "laneB", "s2")
	for _, c := range []struct {
		id string
		o  Owner
	}{{"i-1", a}, {"i-2", a}, {"i-3", b}} {
		if _, err := s.Claim(c.id, c.o); err != nil {
			t.Fatal(err)
		}
	}
	now = now.Add(20 * time.Minute)
	n, err := s.RenewLane("laneA")
	if err != nil || n != 2 {
		t.Fatalf("renewed %d, %v", n, err)
	}
	if n, err := s.RenewLane("nobody"); err != nil || n != 0 {
		t.Fatalf("empty lane: %d, %v", n, err)
	}
	now = now.Add(20 * time.Minute) // 40 min after the claims: B's lease is out, A's is not
	ls, _ := s.List()
	for _, l := range ls {
		if want := l.Lane == "laneB"; l.Stale != want {
			t.Fatalf("%s (lane %s) stale = %v, want %v", l.Item, l.Lane, l.Stale, want)
		}
	}
	wantOps(t, s, OpClaim, OpClaim, OpClaim) // renewals by activity write no event
}
