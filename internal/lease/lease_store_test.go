package lease

import (
	"errors"
	"fmt"
	"os/exec"
	"testing"
	"time"
)

func TestClaimRenewRelease(t *testing.T) {
	s := newStore(t)
	a := owner(t, "laneA", "s1")
	l, err := s.Claim("task-1", a)
	if err != nil {
		t.Fatal(err)
	}
	if l.Lane != "laneA" || l.Pid != a.Pid || l.Start != a.Start || !l.Expires.After(l.Acquired) {
		t.Fatalf("lease = %+v", l)
	}
	if d := l.Expires.Sub(l.Acquired); d != DefaultTTL {
		t.Fatalf("ttl = %v", d)
	}

	b := owner(t, "laneB", "s2")
	_, err = s.Claim("task-1", b)
	var held *HeldError
	if !errors.Is(err, ErrHeld) || !errors.As(err, &held) || held.Holder.Lane != "laneA" {
		t.Fatalf("claim by other = %v", err)
	}

	// Same lane+session, different pid/start: renew and refresh the process.
	a2 := a
	a2.Pid, a2.Start = a.Pid, a.Start+"x"
	l2, err := s.Claim("task-1", a2)
	if err != nil {
		t.Fatal(err)
	}
	if !l2.Acquired.Equal(l.Acquired) || l2.Start != a2.Start {
		t.Fatalf("same-owner claim: %+v", l2)
	}

	if _, err := s.Renew("task-1", b); !errors.Is(err, ErrHeld) {
		t.Fatalf("renew by other = %v", err)
	}
	if _, err := s.Renew("nope", a); !errors.Is(err, ErrNotFound) {
		t.Fatalf("renew missing = %v", err)
	}
	if _, err := s.Renew("task-1", a); err != nil {
		t.Fatal(err)
	}

	if err := s.Release("task-1", b, false); !errors.Is(err, ErrHeld) {
		t.Fatalf("release by other = %v", err)
	}
	if err := s.Release("task-1", a, false); err != nil {
		t.Fatal(err)
	}
	if err := s.Release("task-1", a, false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("double release = %v", err)
	}
	if _, err := s.Claim("task-1", b); err != nil {
		t.Fatalf("claim after release: %v", err)
	}
	wantOps(t, s, "claim", "renew", "renew", "release", "claim")
}

func TestExpiredLeaseIsReclaimed(t *testing.T) {
	s := newStore(t)
	clock := time.Now()
	s.now = func() time.Time { return clock }
	a, b := owner(t, "laneA", "s1"), owner(t, "laneB", "s2")
	if _, err := s.Claim("x", a); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(DefaultTTL - time.Second)
	if _, err := s.Claim("x", b); !errors.Is(err, ErrHeld) {
		t.Fatalf("before expiry: %v", err)
	}
	clock = clock.Add(2 * time.Second)
	ls, _ := s.List()
	if len(ls) != 1 || !ls[0].Stale {
		t.Fatalf("list = %+v", ls)
	}
	if _, ok, err := s.ClaimNext([]string{"x"}, b); err != nil || !ok {
		t.Fatalf("reclaim: ok=%v err=%v", ok, err)
	}
	evs := wantOps(t, s, "claim", "reclaim")
	if r := evs[1]; r.Reason != "expired" || r.PrevLane != "laneA" || r.Lane != "laneB" {
		t.Fatalf("reclaim event = %+v", r)
	}
	// The old holder lost it: renew now reports the new holder.
	if _, err := s.Renew("x", a); !errors.Is(err, ErrHeld) {
		t.Fatalf("renew after reclaim = %v", err)
	}
}

func TestRenewRevivesOwnExpiredLease(t *testing.T) {
	s := newStore(t)
	clock := time.Now()
	s.now = func() time.Time { return clock }
	a := owner(t, "laneA", "s1")
	if _, err := s.Claim("x", a); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(2 * DefaultTTL)
	l, err := s.Renew("x", a)
	if err != nil || !l.Expires.After(clock) {
		t.Fatalf("renew expired own lease: %+v %v", l, err)
	}
}

func TestDeadOwnerIsReclaimed(t *testing.T) {
	s := newStore(t)
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	dead, err := NewOwner("ghost", "gs", cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Claim("job", dead); err != nil {
		t.Fatal(err)
	}
	live := owner(t, "laneB", "s2")
	if _, err := s.Claim("job", live); !errors.Is(err, ErrHeld) {
		t.Fatalf("owner still alive: %v", err)
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait() // a real, reaped process: its pid no longer exists

	ls, _ := s.List()
	if len(ls) != 1 || !ls[0].Stale {
		t.Fatalf("dead owner not stale: %+v", ls)
	}
	l, err := s.Claim("job", live)
	if err != nil || l.Lane != "laneB" {
		t.Fatalf("reclaim: %+v %v", l, err)
	}
	if evs := wantOps(t, s, "claim", "reclaim"); evs[1].Reason != "dead" {
		t.Fatalf("reclaim event = %+v", evs[1])
	}
}

func TestForceRelease(t *testing.T) {
	s := newStore(t)
	a, b := owner(t, "laneA", "s1"), owner(t, "laneB", "s2")
	if _, err := s.Claim("x", a); err != nil {
		t.Fatal(err)
	}
	if err := s.Release("x", b, true); err != nil {
		t.Fatal(err)
	}
	if ls, _ := s.List(); len(ls) != 0 {
		t.Fatalf("still leased: %+v", ls)
	}
	evs := wantOps(t, s, "claim", "release")
	if !evs[1].Forced || evs[1].Lane != "laneB" {
		t.Fatalf("release event = %+v", evs[1])
	}
}

func TestClaimNextOrderAndExhaustion(t *testing.T) {
	s := newStore(t)
	items := []string{"a", "b", "c"}
	for i, want := range []string{"a", "b", "c"} {
		o := owner(t, fmt.Sprintf("lane%d", i), "s")
		l, ok, err := s.ClaimNext(items, o)
		if err != nil || !ok || l.Item != want {
			t.Fatalf("want %s: %+v ok=%v err=%v", want, l, ok, err)
		}
	}
	// A holder that asks again gets its own item back (renewed), not a second one.
	o0 := owner(t, "lane0", "s")
	if l, ok, err := s.ClaimNext(items, o0); err != nil || !ok || l.Item != "a" {
		t.Fatalf("repeat claim: %+v ok=%v err=%v", l, ok, err)
	}
	wantOps(t, s, "claim", "claim", "claim", "renew")
	late := owner(t, "late", "s")
	if _, ok, err := s.ClaimNext(items, late); err != nil || ok {
		t.Fatalf("exhausted: ok=%v err=%v", ok, err)
	}
	if _, ok, err := s.ClaimNext(nil, late); err != nil || ok {
		t.Fatalf("empty: ok=%v err=%v", ok, err)
	}
}
