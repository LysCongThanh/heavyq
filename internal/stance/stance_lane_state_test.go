package stance

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func testStance(name string, cooldown, timebox int) Stance {
	return Stance{Name: name, CooldownMinutes: cooldown, Exit: Exit{TimeboxMinutes: timebox}}
}

func req(s Stance, trig Trigger, session string) EnterRequest {
	return EnterRequest{Stance: s, Trigger: trig, Why: "because", Session: session}
}

func TestEnterCooldownCapAndPrecedence(t *testing.T) {
	bt, calm := testStance("breakthrough", 60, 30), testStance("calm", 0, 0)
	st := &State{Lane: "wt"}
	at := func(m int) time.Time { return t0.Add(time.Duration(m) * time.Minute) }

	evs, skip := st.Enter(req(bt, TriggerFailureClass, "s1"), at(0))
	if skip != "" || len(evs) != 1 || evs[0].Op != OpEnter || st.Active == nil || st.Active.Stance != "breakthrough" {
		t.Fatalf("first enter: %v %q %+v", evs, skip, st.Active)
	}
	if !st.Active.Expires.Equal(at(30)) {
		t.Errorf("expires = %v, want the 30 minute time box", st.Active.Expires)
	}
	// State change only: the same stance again is silent, and evidence never interrupts a stance.
	if _, skip := st.Enter(req(bt, TriggerFailureClass, "s1"), at(1)); skip != SkipActive {
		t.Errorf("same stance: skip %q", skip)
	}
	if _, skip := st.Enter(req(calm, TriggerChurn, "s1"), at(1)); skip != SkipBusy {
		t.Errorf("evidence over an active stance: skip %q", skip)
	}
	// A keyword may replace the active stance, and the replaced one is logged.
	evs, skip = st.Enter(req(calm, TriggerKeyword, "s1"), at(2))
	if skip != "" || len(evs) != 2 || evs[0].Op != OpExit || evs[0].Reason != ExitReplaced || st.Active.Stance != "calm" {
		t.Fatalf("keyword replace: %v %q", evs, skip)
	}
	st.Exit(ExitManual, at(3))
	// Cooldown: breakthrough was left at minute 2 (replaced), 60 minutes apply.
	if _, skip := st.Enter(req(bt, TriggerKeyword, "s1"), at(30)); skip != SkipCooldown {
		t.Errorf("inside the cooldown: skip %q", skip)
	}
	if _, skip := st.Enter(req(bt, TriggerKeyword, "s1"), at(63)); skip != "" {
		t.Errorf("after the cooldown: skip %q", skip)
	}
	// An explicit `stance enter` ignores the cooldown.
	st.Exit(ExitManual, at(64))
	if _, skip := st.Enter(req(bt, TriggerManual, "s1"), at(65)); skip != "" {
		t.Errorf("manual inside the cooldown: skip %q", skip)
	}

	// The cap: at most 3 activations of a stance per session, then silence; a new session starts again.
	st = &State{Lane: "wt"}
	for i := 0; i < MaxActivationsPerSession; i++ {
		if _, skip := st.Enter(req(calm, TriggerKeyword, "s1"), at(i*10)); skip != "" {
			t.Fatalf("activation %d: skip %q", i+1, skip)
		}
		st.Exit(ExitManual, at(i*10+1))
	}
	if _, skip := st.Enter(req(calm, TriggerKeyword, "s1"), at(100)); skip != SkipCap {
		t.Errorf("4th activation: skip %q", skip)
	}
	if _, skip := st.Enter(req(calm, TriggerKeyword, "s2"), at(100)); skip != "" {
		t.Errorf("new session: skip %q", skip)
	}
	if st.Counts["calm"] != 1 || st.Session != "s2" {
		t.Errorf("counts after a new session: %v in %q", st.Counts, st.Session)
	}
}

func TestOffMutesUntilManualEnter(t *testing.T) {
	calm := testStance("calm", 0, 0)
	st := &State{Lane: "wt"}
	st.Enter(req(calm, TriggerKeyword, "s"), t0)
	evs := st.Off("s", t0.Add(time.Minute))
	if len(evs) != 2 || evs[0].Reason != ExitOff || evs[1].Op != OpOff || st.Active != nil || !st.Muted {
		t.Fatalf("off: %v %+v", evs, st)
	}
	if _, skip := st.Enter(req(calm, TriggerKeyword, "s"), t0.Add(time.Hour)); skip != SkipMuted {
		t.Errorf("muted lane: skip %q", skip)
	}
	if _, skip := st.Enter(req(calm, TriggerManual, "s"), t0.Add(time.Hour)); skip != "" || st.Muted {
		t.Errorf("manual enter unmutes: skip %q muted %v", skip, st.Muted)
	}
}

func TestSettleEndsAStancePastItsTimebox(t *testing.T) {
	st := &State{Lane: "wt"}
	st.Enter(req(testStance("breakthrough", 0, 30), TriggerManual, "s"), t0)
	if st.Settle(t0.Add(29*time.Minute)) != nil || st.Active == nil {
		t.Fatal("settled inside the time box")
	}
	ev := st.Settle(t0.Add(30 * time.Minute))
	if ev == nil || ev.Reason != ExitTimebox || st.Active != nil {
		t.Fatalf("settle at the end of the time box: %v %+v", ev, st.Active)
	}
	st.Enter(req(testStance("calm", 0, 0), TriggerManual, "s"), t0)
	if st.Settle(t0.Add(1000*time.Hour)) != nil {
		t.Error("a stance without a time box never expires")
	}
}

func TestStoreRoundTripEventsAndConcurrentUpdates(t *testing.T) {
	common := t.TempDir()
	s, err := OpenStore(common)
	if err != nil {
		t.Fatal(err)
	}
	if st, err := s.Load("wt"); err != nil || st.Active != nil || st.Lane != "wt" {
		t.Fatalf("fresh lane: %+v %v", st, err)
	}
	calm := testStance("calm", 0, 0)
	if err := s.Update("wt", func(st *State) ([]Event, error) {
		evs, _ := st.Enter(req(calm, TriggerKeyword, "s"), t0)
		return evs, nil
	}); err != nil {
		t.Fatal(err)
	}
	st, _ := s.Load("wt")
	if st.Active == nil || st.Active.Stance != "calm" || !st.Active.Since.Equal(t0) {
		t.Fatalf("saved state: %+v", st.Active)
	}
	if fi, err := os.Stat(filepath.Join(common, "heavyq", "stance", "lanes", "wt.json")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("lane file: %v %v", fi, err)
	}
	// An update that returns no events leaves the file alone; an error saves nothing.
	if err := s.Update("wt", func(st *State) ([]Event, error) { st.Muted = true; return nil, nil }); err != nil {
		t.Fatal(err)
	}
	if st, _ := s.Load("wt"); st.Muted {
		t.Error("a change without events was saved")
	}

	// 8 processes' worth of writers on one lane: every update lands, none is lost.
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s2, _ := OpenStore(common)
			if err := s2.Update("wt", func(st *State) ([]Event, error) {
				if st.Counts == nil {
					st.Counts = map[string]int{}
				}
				st.Counts["x"]++
				return []Event{{At: t0, Op: OpExit, Lane: "wt", Stance: "x"}}, nil
			}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if st, _ := s.Load("wt"); st.Counts["x"] != 8 {
		t.Errorf("counts = %v, want 8 updates", st.Counts)
	}
	if evs, err := s.Events(); err != nil || len(evs) != 9 {
		t.Errorf("events: %d, %v; want 1 enter + 8 exits", len(evs), err)
	}
	if _, err := s.Load("../escape"); err == nil {
		t.Error("a lane name with a path must be refused")
	}
	if _, err := OpenStore(filepath.Join(common, "nope")); err == nil {
		t.Error("a missing common dir must be refused")
	}
}
