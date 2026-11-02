package mentor

import (
	"testing"
	"time"
)

func sig(runID string, level int, at time.Time) Signal {
	return Signal{Rule: RuleRepeatFailure, Signature: runID, Level: level, At: at}
}

func TestAdmitStateChangeOnly(t *testing.T) {
	st := newLaneState()
	l := Limits{Cap: 10, Cooldown: 0}
	if !st.Admit(sig("A", LevelFact, t0), "s1", l) {
		t.Fatal("the first fact must pass")
	}
	if st.Admit(sig("A", LevelFact, t0.Add(time.Hour)), "s1", l) {
		t.Fatal("the same state must never be repeated aloud")
	}
	if !st.Admit(sig("A", LevelDirective, t0.Add(time.Hour)), "s1", l) {
		t.Fatal("escalation to a directive is a state change")
	}
	if st.Admit(sig("A", LevelDirective, t0.Add(2*time.Hour)), "s1", l) || st.Admit(sig("A", LevelFact, t0.Add(2*time.Hour)), "s1", l) {
		t.Fatal("a lower or equal level must stay silent")
	}
	if !st.Admit(sig("B", LevelFact, t0), "s1", l) {
		t.Fatal("another signature has its own state")
	}
}

func TestAdmitCooldownPerSignature(t *testing.T) {
	st := newLaneState()
	l := Limits{Cap: 10, Cooldown: time.Minute}
	if !st.Admit(sig("A", LevelFact, t0), "s", l) {
		t.Fatal("first")
	}
	if st.Admit(sig("A", LevelDirective, t0.Add(30*time.Second)), "s", l) {
		t.Fatal("inside the cooldown the same signature stays quiet")
	}
	if !st.Admit(sig("B", LevelFact, t0.Add(30*time.Second)), "s", l) {
		t.Fatal("the cooldown is per signature")
	}
	if !st.Admit(sig("A", LevelDirective, t0.Add(61*time.Second)), "s", l) {
		t.Fatal("after the cooldown the escalation passes")
	}
}

func TestAdmitCapPerRulePerSession(t *testing.T) {
	st := newLaneState()
	l := Limits{Cap: 3, Cooldown: 0}
	for i, id := range []string{"A", "B", "C"} {
		if !st.Admit(sig(id, LevelFact, t0), "s1", l) {
			t.Fatalf("event %d is under the cap", i+1)
		}
	}
	if st.Admit(sig("D", LevelFact, t0), "s1", l) {
		t.Fatal("the 4th event of a session must be capped")
	}
	if !st.Admit(sig("D", LevelFact, t0), "s2", l) {
		t.Fatal("a new session starts from nothing")
	}
}

func TestLaneStateRoundTripAndHostileFiles(t *testing.T) {
	s := &Store{Dir: t.TempDir(), LaneDir: t.TempDir()}
	st := s.LoadLane("wt/../x")
	if st.Fires == nil || st.Sigs == nil {
		t.Fatal("a missing file is an empty state")
	}
	st.Admit(sig("A", LevelFact, t0), "s", DefaultLimits)
	st.LastRecord = "id1"
	if err := s.SaveLane("wt/../x", st); err != nil {
		t.Fatal(err)
	}
	got := s.LoadLane("wt/../x")
	if got.LastRecord != "id1" || got.Fires[RuleRepeatFailure] != 1 || got.Sigs["repeat-failure|A"].Level != LevelFact {
		t.Fatalf("round trip: %+v", got)
	}
	if err := writeFileAtomic(s.lanePath("bad"), []byte("{not json")); err != nil {
		t.Fatal(err)
	}
	if bad := s.LoadLane("bad"); bad.LastRecord != "" || bad.Fires == nil {
		t.Fatalf("a corrupt file must read as empty: %+v", bad)
	}
}

func TestSignaturesStayBounded(t *testing.T) {
	st := newLaneState()
	l := Limits{Cap: 1000, Cooldown: 0}
	for i := 0; i < 3*maxSignatures; i++ {
		st.Admit(sig(string(rune('a'+i%26))+string(rune('A'+i/26)), LevelFact, t0.Add(time.Duration(i)*time.Second)), "s", l)
	}
	if len(st.Sigs) > maxSignatures {
		t.Fatalf("%d signatures kept", len(st.Sigs))
	}
}
