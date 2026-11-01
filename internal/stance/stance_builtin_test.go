package stance

import (
	"strings"
	"testing"
)

func TestStanceBuiltinsAreWellFormed(t *testing.T) {
	all, err := Builtins()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 5 {
		t.Fatalf("want 5 built-ins, got %d", len(all))
	}
	for _, name := range []string{"breakthrough", "converge", "skeptic", "calm", "surgeon"} {
		s, ok := Find(all, name)
		if !ok {
			t.Fatalf("missing built-in %q", name)
		}
		if s.Description == "" || len(s.Enter.Keywords) == 0 || s.CooldownMinutes <= 0 {
			t.Errorf("%s: description, keywords and cooldown must be set: %+v", name, s)
		}
		if len(s.Body) > 1536 {
			t.Errorf("%s: body is %d bytes, the limit for built-ins is 1.5 KiB", name, len(s.Body))
		}
	}
}

// The breakthrough stance must keep the ideas it exists for.
func TestStanceBreakthroughCoversItsCore(t *testing.T) {
	s := builtinNamed(t, "breakthrough")
	for _, want := range []string{"from scratch", "at least three", "radical", "status quo", "REAL", "INERTIA", "Research", "Steelman", "Pre-mortem", "reversible", "measurable", "migration", "trade-offs", "human decides"} {
		if !strings.Contains(s.Body, want) {
			t.Errorf("breakthrough body lacks %q", want)
		}
	}
	if st := s.Enter.Stall; st == nil || st.Minutes != 40 || st.Runs != 8 || !st.WithoutProof {
		t.Errorf("stall = %+v", st)
	}
	if s.Enter.SameFailureClassAcrossTrees != 3 || s.Enter.ChurnReverts != 2 || !s.Exit.DecisionRecord || s.Exit.TimeboxMinutes != 30 {
		t.Errorf("triggers = %+v %+v", s.Enter, s.Exit)
	}
}
