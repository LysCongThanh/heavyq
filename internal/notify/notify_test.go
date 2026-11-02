package notify

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 2, 14, 0, 0, 0, time.Local)

func facts() Facts { return Facts{Now: now, Updated: now.Add(-5 * time.Second), Total: 8, Proven: 3} }

func need(key string) Need { return Need{Key: key, Text: key + ": permission_prompt"} }

func TestEvaluateOncePerStateChange(t *testing.T) {
	f := facts()
	f.Needs = []Need{need("doc-7")}
	st, msg := Evaluate(nil, f, 5)
	if msg == nil || !strings.Contains(msg.Body, "needs you: doc-7") {
		t.Fatalf("a new need must push, got %v", msg)
	}
	// The same state, evaluated again and again: silent.
	for i := 0; i < 3; i++ {
		var m *Message
		st, m = Evaluate(&st, f, 5)
		if m != nil {
			t.Fatalf("repeat %d pushed %q", i, m.Body)
		}
	}
	if st.Sent != 1 {
		t.Errorf("sent = %d, want 1", st.Sent)
	}
	// It resolves, then comes back: that is a new state change.
	f.Needs = nil
	st, m := Evaluate(&st, f, 5)
	if m != nil || len(st.Needs) != 0 {
		t.Fatalf("a resolved need is not news: %v %v", m, st.Needs)
	}
	f.Needs = []Need{need("doc-7")}
	if _, m := Evaluate(&st, f, 5); m == nil {
		t.Error("a need that returns must push again")
	}
}

func TestEvaluateAtRiskAndAllProven(t *testing.T) {
	f := facts()
	st, m := Evaluate(nil, f, 5)
	if m != nil {
		t.Fatalf("a calm start is silent: %v", m)
	}
	f.AtRisk, f.AtRiskWhy, f.ETA = true, "ETA is 15m past the expected end", now.Add(time.Hour)
	st, m = Evaluate(&st, f, 5)
	if m == nil || !strings.Contains(m.Body, "at risk: ETA is 15m past the expected end, ETA ~15:00") {
		t.Fatalf("at risk must push once: %v", m)
	}
	if _, m = Evaluate(&st, f, 5); m != nil {
		t.Fatalf("still at risk is not news: %v", m)
	}
	f.AtRisk = false
	st, _ = Evaluate(&st, f, 5)
	f.AtRisk = true
	if _, m = Evaluate(&st, f, 5); m == nil {
		t.Error("at risk again after recovering must push")
	}

	f = facts()
	st, _ = Evaluate(nil, f, 5)
	f.AllProven, f.Proven = true, 8
	st, m = Evaluate(&st, f, 5)
	if m == nil || m.Body != "all 8 items proven" {
		t.Fatalf("all proven must push: %v", m)
	}
	if _, m = Evaluate(&st, f, 5); m != nil {
		t.Errorf("all proven twice: %v", m)
	}
	// Already finished when first seen: quiet.
	if _, m = Evaluate(nil, f, 5); m != nil {
		t.Errorf("a repository that is already done must not push on first look: %v", m)
	}
}

func TestEvaluateCoalescesBudgetAndStale(t *testing.T) {
	f := facts()
	f.Needs = []Need{need("a"), need("b"), need("c"), need("d")}
	f.AtRisk = true
	_, m := Evaluate(nil, f, 5)
	if m == nil || strings.Count(m.Body, "\n") != 2 || !strings.Contains(m.Body, "+3 more") {
		t.Fatalf("one message, at most 3 lines: %q", m)
	}

	// Budget: the second change is swallowed and stays swallowed.
	f = facts()
	f.Needs = []Need{need("a")}
	st, m := Evaluate(nil, f, 1)
	if m == nil {
		t.Fatal("first push expected")
	}
	f.Needs = append(f.Needs, need("b"))
	st, m = Evaluate(&st, f, 1)
	if m != nil || st.Suppressed != 1 {
		t.Fatalf("over budget must stay silent: %v %+v", m, st)
	}
	if _, m = Evaluate(&st, f, 1); m != nil {
		t.Error("a swallowed change must not come back")
	}
	// A new day resets the budget.
	f.Now = f.Now.Add(24 * time.Hour)
	f.Updated = f.Now
	f.Needs = append(f.Needs, need("c"))
	if _, m = Evaluate(&st, f, 1); m == nil {
		t.Error("a new day has a new budget")
	}

	// Budget 0 pushes nothing; a stale state changes nothing.
	f = facts()
	f.Needs = []Need{need("a")}
	if _, m = Evaluate(nil, f, 0); m != nil {
		t.Error("budget 0 is off")
	}
	f.Updated = f.Now.Add(-MaxAge - time.Second)
	if st, m = Evaluate(nil, f, 5); m != nil || len(st.Needs) != 0 {
		t.Errorf("stale state: %v %+v", m, st)
	}
}

type recorder struct {
	n    atomic.Int32
	body atomic.Value
}

func (*recorder) Name() string { return "rec" }
func (r *recorder) Send(m Message) error {
	r.n.Add(1)
	r.body.Store(m.Body)
	return nil
}

// Dedupe on disk: the same need announced from many concurrent status lines,
// and again from later ones, is delivered exactly once.
func TestCheckDedupesAcrossProcessesOfOneRepo(t *testing.T) {
	common := t.TempDir()
	rec := &recorder{}
	f := facts()
	f.Needs = []Need{need("doc-7")}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			Check(common, f, Options{Budget: 5, Channels: Fixed(rec)})
		}()
	}
	wg.Wait()
	for i := 0; i < 5; i++ {
		Check(common, f, Options{Budget: 5, Channels: Fixed(rec)})
	}
	if rec.n.Load() != 1 {
		t.Fatalf("delivered %d times, want 1", rec.n.Load())
	}
	// A different repository (another common dir) has its own state.
	Check(t.TempDir(), f, Options{Budget: 5, Channels: Fixed(rec)})
	if rec.n.Load() != 2 {
		t.Errorf("a second repository must notify on its own, got %d", rec.n.Load())
	}
}

func TestCheckKeepsStateWhenNoChannelDelivers(t *testing.T) {
	common := t.TempDir()
	f := facts()
	f.Needs = []Need{need("doc-7")}
	out := Check(common, f, Options{Budget: 5})
	if out.Message == nil || out.Err != ErrNoChannel {
		t.Fatalf("outcome %+v, want a message and ErrNoChannel", out)
	}
	rec := &recorder{}
	if out := Check(common, f, Options{Budget: 5, Channels: Fixed(rec)}); out.Message != nil || rec.n.Load() != 0 {
		t.Error("a notice that failed to deliver is not retried every status line")
	}
}

func TestCheckIgnoresStaleState(t *testing.T) {
	common := t.TempDir()
	f := facts()
	f.Needs = []Need{need("doc-7")}
	f.Updated = now.Add(-time.Hour)
	if out := Check(common, f, Options{Budget: 5, Channels: Fixed(&recorder{})}); out.Message != nil {
		t.Errorf("stale facts pushed: %+v", out)
	}
	if readState(Dir(common)) != nil {
		t.Error("stale facts must not even create a record")
	}
}

func TestBudgetFromEnv(t *testing.T) {
	for v, want := range map[string]int{"": 5, "x": 5, "-1": 5, "0": 0, "12": 12} {
		if got := BudgetFromEnv(func(string) string { return v }); got != want {
			t.Errorf("budget %q = %d, want %d", v, got, want)
		}
	}
}
