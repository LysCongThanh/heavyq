package mentor

import (
	"fmt"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	return &Store{Dir: t.TempDir(), LaneDir: t.TempDir()}
}

// evalAt runs Evaluate with the clock at the end of the newest record.
func evalAt(t *testing.T, s *Store, recs []history.Record, c Config) *Decision {
	t.Helper()
	d, err := s.Evaluate(Input{Lane: "wt", Session: "sess", Records: recs, Now: recs[len(recs)-1].End.Add(time.Second)}, c)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func injectAll() Config {
	c := DefaultConfig()
	c.HoldoutPct = 0
	return c
}

func holdoutAll() Config {
	c := DefaultConfig()
	c.HoldoutPct = 100
	return c
}

func TestEvaluateFactThenDirectiveThenSilence(t *testing.T) {
	s := newStore(t)
	t.Setenv(seedEnv, "s")
	c := injectAll()
	c.Limits.Cooldown = 0
	lane := []history.Record{run(1, "A", 1)}
	if d := evalAt(t, s, lane, c); d != nil {
		t.Fatalf("the 1st failure must be silent: %+v", d)
	}
	lane = append(lane, run(2, "A", 1))
	d := evalAt(t, s, lane, c)
	if d == nil || d.Entry.Level != LevelFact || d.Message == "" || d.Entry.Arm != ArmInject || d.Entry.Seed != "s" {
		t.Fatalf("2nd failure: %+v", d)
	}
	if d := evalAt(t, s, lane, c); d != nil {
		t.Fatal("the same record must be considered once: a following non-heavy command repeats nothing")
	}
	lane = append(lane, run(3, "A", 1))
	if d := evalAt(t, s, lane, c); d == nil || d.Entry.Level != LevelDirective || d.Entry.Streak != 3 {
		t.Fatalf("3rd failure: %+v", d)
	}
	lane = append(lane, run(4, "A", 1))
	if d := evalAt(t, s, lane, c); d != nil {
		t.Fatalf("a 4th failure is no new state: %+v", d)
	}
	if entries, _ := s.ReadEntries(); len(entries) != 2 {
		t.Fatalf("both events must be logged: %d", len(entries))
	}
}

func TestEvaluateCapAcrossSignatures(t *testing.T) {
	s := newStore(t)
	t.Setenv(seedEnv, "s")
	c := injectAll()
	fired := 0
	var lane []history.Record
	for i, id := range []string{"A", "B", "C", "D", "E"} { // five different commands each failing twice
		lane = append(lane, run(2*i+1, id, 1), run(2*i+2, id, 1))
		if d := evalAt(t, s, lane[:2*i+1], c); d != nil {
			t.Fatal("a first failure is silent")
		}
		if d := evalAt(t, s, lane, c); d != nil {
			fired++
		}
	}
	if fired != DefaultLimits.Cap {
		t.Fatalf("fired %d times, the cap is %d per session", fired, DefaultLimits.Cap)
	}
}

func TestEvaluateHoldoutIsLoggedButSilent(t *testing.T) {
	s := newStore(t)
	t.Setenv(seedEnv, "s")
	lane := []history.Record{run(1, "A", 1), run(2, "A", 1)}
	d := evalAt(t, s, lane, holdoutAll())
	if d == nil || d.Message != "" || d.Entry.Arm != ArmHoldout {
		t.Fatalf("holdout: %+v", d)
	}
	got, _ := s.ReadEntries()
	if len(got) != 1 || got[0].Arm != ArmHoldout || got[0].Rule != RuleRepeatFailure || got[0].Seed != "s" ||
		got[0].Event != "id02" || got[0].Outcome != OutcomePending || got[0].Lane != "wt" {
		t.Fatalf("log: %+v", got)
	}
}

func TestEvaluateIgnoresStaleRecordsMutedRulesAndUnidentifiedRuns(t *testing.T) {
	s := newStore(t)
	t.Setenv(seedEnv, "s")
	lane := []history.Record{run(1, "A", 1), run(2, "A", 1)}
	old := lane[1].End.Add(time.Hour)
	if d, err := s.Evaluate(Input{Lane: "wt", Session: "s", Records: lane, Now: old}, injectAll()); err != nil || d != nil {
		t.Fatalf("a record from an hour ago is not this tool call: %+v %v", d, err)
	}
	if err := s.SetMute(RuleRepeatFailure, true, MutedByUser, "", t0); err != nil {
		t.Fatal(err)
	}
	if d := evalAt(t, s, lane, injectAll()); d != nil {
		t.Fatalf("a muted rule must stay silent and log nothing: %+v", d)
	}
	if entries, _ := s.ReadEntries(); len(entries) != 0 {
		t.Fatal("a muted rule logs nothing")
	}
	s2 := newStore(t)
	noID := func(r *history.Record) { r.ID = "" }
	if d := evalAt(t, s2, []history.Record{run(1, "A", 1, noID), run(2, "A", 1, noID)}, injectAll()); d != nil {
		t.Fatal("a record without an id cannot be keyed")
	}
}

func TestEvaluateNeverFiresOnFlakyUnderLoadOrExcludedExits(t *testing.T) {
	cases := map[string][]history.Record{
		"flaky":      {run(1, "A", 0), run(2, "A", 1), run(3, "A", 1)},
		"under_load": {run(1, "A", 1), run(2, "A", 1, under)},
		"124":        {run(1, "A", 124), run(2, "A", 124), run(3, "A", 124)},
		"137":        {run(1, "A", 137), run(2, "A", 137)},
	}
	for name, lane := range cases {
		s := newStore(t)
		t.Setenv(seedEnv, "s")
		if d := evalAt(t, s, lane, injectAll()); d != nil {
			t.Errorf("%s: fired %+v", name, d)
		}
		if entries, _ := s.ReadEntries(); len(entries) != 0 {
			t.Errorf("%s: logged %d events", name, len(entries))
		}
	}
}

func TestEvaluateResolvesOutcomesAndAutoMutes(t *testing.T) {
	s := newStore(t)
	t.Setenv(seedEnv, "s")
	policy := AutoMute{Enabled: true, MinEvents: 2, MinLiftPP: 10}
	// Four lanes, two held out and two shown; on each the agent repeats the same
	// run three more times (cache replays, so they raise no new signal), so every
	// outcome is "same": lift 0 after 2 resolved per arm, and the rule mutes itself.
	for i, cfg := range []Config{holdoutAll(), injectAll(), holdoutAll(), injectAll()} {
		cfg.AutoMute = policy
		lane, rid, base := fmt.Sprintf("wt%d", i), string(rune('A'+i)), 100*(i+1)
		eval := func(recs []history.Record) *Decision {
			d, err := s.Evaluate(Input{Lane: lane, Session: "s", Records: recs, Now: recs[len(recs)-1].End.Add(time.Second)}, cfg)
			if err != nil {
				t.Fatal(err)
			}
			return d
		}
		recs := []history.Record{run(base+1, rid, 1)}
		eval(recs)
		recs = append(recs, run(base+2, rid, 1))
		if eval(recs) == nil {
			t.Fatalf("lane %s: the 2nd failure must raise an event", lane)
		}
		recs = append(recs, run(base+3, rid, 1, replay), run(base+4, rid, 1, replay), run(base+5, rid, 1, replay))
		if eval(recs) != nil {
			t.Fatalf("lane %s: replays raise no new event", lane)
		}
		if i < 3 && s.IsMuted(RuleRepeatFailure) {
			t.Fatalf("muted too early, after %d events", i+1)
		}
	}
	entries, _ := s.ReadEntries()
	for _, e := range entries {
		if e.Outcome != OutcomeSame {
			t.Fatalf("every event repeated the same run, got %+v", e)
		}
	}
	if len(entries) != 4 || !s.IsMuted(RuleRepeatFailure) {
		t.Fatalf("no lift after 2 resolved per arm must mute: %d events, %+v", len(entries), ComputeStats(entries, time.Time{})[0])
	}
}

func TestSweepPendingSettlesWhatTheLanesCanSettle(t *testing.T) {
	s := newStore(t)
	_ = s.AppendEntry(Entry{At: t0, Rule: RuleRepeatFailure, Arm: ArmInject, Event: "id02", Lane: "wt", RunID: "A", Outcome: OutcomePending})
	_ = s.AppendEntry(Entry{At: t0, Rule: RuleRepeatFailure, Arm: ArmHoldout, Event: "id09", Lane: "other", RunID: "A", Outcome: OutcomePending})
	lanes := map[string][]history.Record{
		"wt":    {run(1, "A", 1), run(2, "A", 1), run(3, "B", 0)},
		"other": {run(9, "A", 1)},
	}
	n, err := s.SweepPending(func(l string) ([]history.Record, error) { return lanes[l], nil }, DefaultConfig(), t0.Add(time.Hour))
	if err != nil || n != 1 {
		t.Fatalf("settled %d, %v", n, err)
	}
	got, _ := s.ReadEntries()
	if got[0].Outcome != OutcomeDiffered || got[1].Outcome != OutcomePending {
		t.Fatalf("%+v", got)
	}
}
