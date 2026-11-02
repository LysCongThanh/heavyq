package mentor

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// entries makes n events of one arm: differed of them with outcome differed,
// the rest same, all at time at.
func entries(arm Arm, n, differed int, at time.Time) []Entry {
	out := make([]Entry, n)
	for i := range out {
		o := OutcomeSame
		if i < differed {
			o = OutcomeDiffered
		}
		out[i] = Entry{At: at, Rule: RuleRepeatFailure, Arm: arm, Event: fmt.Sprintf("%s-%d-%s", arm, i, at.Format("150405")), Outcome: o}
	}
	return out
}

func TestComputeStatsAndLift(t *testing.T) {
	var all []Entry
	all = append(all, entries(ArmInject, 100, 70, t0)...) // 70%
	all = append(all, entries(ArmHoldout, 50, 20, t0)...) // 40%
	all = append(all, Entry{At: t0, Rule: RuleRepeatFailure, Arm: ArmInject, Event: "p", Outcome: OutcomePending},
		Entry{At: t0, Rule: RuleRepeatFailure, Arm: ArmHoldout, Event: "u", Outcome: OutcomeUnknown})
	rs := ComputeStats(all, time.Time{})[0]
	if rs.Inject.Events != 101 || rs.Inject.Resolved != 100 || rs.Inject.Differed != 70 || rs.Holdout.Events != 51 || rs.Holdout.Resolved != 50 {
		t.Fatalf("counts: %+v", rs)
	}
	if rs.LiftPP == nil || *rs.LiftPP < 29.99 || *rs.LiftPP > 30.01 {
		t.Fatalf("lift = %v, want 30 pp", rs.LiftPP)
	}
	if only := ComputeStats(entries(ArmInject, 10, 5, t0), time.Time{})[0]; only.LiftPP != nil {
		t.Fatal("no lift without a resolved holdout event")
	}
	if late := ComputeStats(all, t0.Add(time.Hour))[0]; late.Inject.Events != 0 {
		t.Fatal("events before since must not count")
	}
}

func TestAutoMutePolicy(t *testing.T) {
	p := DefaultAutoMute
	mk := func(nI, dI, nH, dH int) RuleStats {
		var all []Entry
		all = append(all, entries(ArmInject, nI, dI, t0)...)
		all = append(all, entries(ArmHoldout, nH, dH, t0)...)
		return ComputeStats(all, time.Time{})[0]
	}
	cases := []struct {
		name string
		rs   RuleStats
		on   bool
		mute bool
	}{
		{"no lift after 200 per arm mutes", mk(200, 110, 200, 100), true, true}, // 55% vs 50% = 5 pp
		{"lift of 10 pp is kept", mk(200, 120, 200, 100), true, false},          // exactly 10 pp
		{"199 in an arm is too few", mk(199, 100, 200, 100), true, false},
		{"199 in the holdout is too few", mk(200, 100, 199, 100), true, false},
		{"negative lift mutes", mk(200, 60, 200, 100), true, true},
		{"disabled never mutes", mk(200, 100, 200, 100), false, false},
	}
	for _, c := range cases {
		p.Enabled = c.on
		mute, why := p.ShouldMute(c.rs)
		if mute != c.mute || (mute && why == "") {
			t.Errorf("%s: mute=%v why=%q", c.name, mute, why)
		}
	}
}

func TestEvaluateAutoMuteMutesAndUnmuteStartsOver(t *testing.T) {
	s := &Store{Dir: filepath.Join(t.TempDir(), "mentor")}
	old := t0
	for _, e := range append(entries(ArmInject, 200, 100, old), entries(ArmHoldout, 200, 100, old)...) { // 0 pp
		if err := s.AppendEntry(e); err != nil {
			t.Fatal(err)
		}
	}
	muted, err := s.EvaluateAutoMute(DefaultAutoMute, t0.Add(time.Hour))
	if err != nil || len(muted) != 1 || !s.IsMuted(RuleRepeatFailure) {
		t.Fatalf("muted %v %v, IsMuted=%v", muted, err, s.IsMuted(RuleRepeatFailure))
	}
	if m := s.Mutes()[RuleRepeatFailure]; m.By != MutedByAuto || m.Reason == "" {
		t.Fatalf("mute entry: %+v", m)
	}
	// The user turns it back on: the old events no longer count against it.
	if err := s.SetMute(RuleRepeatFailure, false, MutedByUser, "", t0.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if muted, err := s.EvaluateAutoMute(DefaultAutoMute, t0.Add(3*time.Hour)); err != nil || len(muted) != 0 || s.IsMuted(RuleRepeatFailure) {
		t.Fatalf("after unmute: %v %v", muted, err)
	}
	// Disabled: nothing is ever muted automatically.
	s2 := &Store{Dir: filepath.Join(t.TempDir(), "mentor")}
	for _, e := range append(entries(ArmInject, 200, 100, old), entries(ArmHoldout, 200, 100, old)...) {
		_ = s2.AppendEntry(e)
	}
	off := DefaultAutoMute
	off.Enabled = false
	if muted, _ := s2.EvaluateAutoMute(off, t0); len(muted) != 0 || s2.IsMuted(RuleRepeatFailure) {
		t.Fatal("auto_mute = false must never mute")
	}
}

func TestSetMuteRefusesUnknownRules(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	if err := s.SetMute("no-such-rule", true, MutedByUser, "", t0); err == nil {
		t.Fatal("an unknown rule must be refused")
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "mute.json")); err == nil {
		t.Fatal("nothing may be stored for an unknown rule")
	}
	if err := s.SetMute(RuleRepeatFailure, true, MutedByUser, "", t0); err != nil || !s.IsMuted(RuleRepeatFailure) {
		t.Fatal("known rule must mute")
	}
}

func TestLogFoldKeepsTheLastLinePerEventAndSkipsCorruptLines(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	e := Entry{At: t0, Rule: RuleRepeatFailure, Arm: ArmInject, Event: "id1", Outcome: OutcomePending}
	_ = s.AppendEntry(e)
	e2 := Entry{At: t0, Rule: RuleRepeatFailure, Arm: ArmHoldout, Event: "id2", Outcome: OutcomePending}
	_ = s.AppendEntry(e2)
	f, err := os.OpenFile(s.logPath(), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("{torn line\n")
	f.Close()
	e.Outcome = OutcomeDiffered
	_ = s.AppendEntry(e)
	got, err := s.ReadEntries()
	if err != nil || len(got) != 2 || got[0].Event != "id1" || got[0].Outcome != OutcomeDiffered || got[1].Outcome != OutcomePending {
		t.Fatalf("%+v %v", got, err)
	}
	if empty, err := (&Store{Dir: t.TempDir()}).ReadEntries(); err != nil || len(empty) != 0 {
		t.Fatalf("a missing log is empty: %v %v", empty, err)
	}
}
