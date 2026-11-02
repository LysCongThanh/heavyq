package mentor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssignIsDeterministicBySeed(t *testing.T) {
	for i := 0; i < 200; i++ {
		ev := fmt.Sprintf("ev%d", i)
		first := Assign("seed-1", RuleRepeatFailure, ev, 20)
		if again := Assign("seed-1", RuleRepeatFailure, ev, 20); again != first {
			t.Fatalf("event %s: the same seed must give the same arm", ev)
		}
	}
	differs := 0
	for i := 0; i < 500; i++ {
		ev := fmt.Sprintf("ev%d", i)
		if Assign("seed-1", RuleRepeatFailure, ev, 20) != Assign("seed-2", RuleRepeatFailure, ev, 20) {
			differs++
		}
	}
	if differs == 0 {
		t.Fatal("another seed must give other draws")
	}
}

func TestAssignSplitsEightyTwenty(t *testing.T) {
	const n = 20000
	holdout := 0
	for i := 0; i < n; i++ {
		if Assign("seed-x", RuleRepeatFailure, fmt.Sprintf("event-%d", i), DefaultHoldoutPct) == ArmHoldout {
			holdout++
		}
	}
	if pct := float64(holdout) * 100 / n; pct < 18.5 || pct > 21.5 {
		t.Fatalf("holdout share = %.2f%%, want about 20", pct)
	}
}

func TestAssignExtremes(t *testing.T) {
	for i := 0; i < 100; i++ {
		ev := fmt.Sprintf("e%d", i)
		if Assign("s", RuleRepeatFailure, ev, 0) != ArmInject {
			t.Fatal("0% holdout must always inject")
		}
		if Assign("s", RuleRepeatFailure, ev, 100) != ArmHoldout {
			t.Fatal("100% holdout (shadow mode) must never inject")
		}
	}
}

func TestSeedIsStoredOnceAndOverridable(t *testing.T) {
	s := &Store{Dir: filepath.Join(t.TempDir(), "mentor")}
	a, err := s.Seed()
	if err != nil || a == "" {
		t.Fatalf("seed: %q %v", a, err)
	}
	if b, _ := s.Seed(); b != a {
		t.Fatalf("the seed must be stable: %q then %q", a, b)
	}
	if fi, err := os.Stat(filepath.Join(s.Dir, "seed")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("seed file: %v %v", fi, err)
	}
	t.Setenv(seedEnv, "fixed-seed.1")
	if v, err := s.Seed(); err != nil || v != "fixed-seed.1" {
		t.Fatalf("env seed: %q %v", v, err)
	}
	t.Setenv(seedEnv, "bad seed; rm")
	if _, err := s.Seed(); err == nil || !strings.Contains(err.Error(), seedEnv) {
		t.Fatalf("an unsafe seed must be refused, got %v", err)
	}
}
