package receipts

import (
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
)

var (
	t1 = time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	t0 = t1.Add(-7 * 24 * time.Hour)
)

// rec is a finished heavy run an hour before the window ends.
func rec(mod func(*history.Record)) history.Record {
	r := history.Record{End: t1.Add(-time.Hour), Machine: "local", Lane: "a", RunSecs: 5, RunID: "r0"}
	if mod != nil {
		mod(&r)
	}
	return r
}

func TestComputeCountsOnlyWhatTheLedgerProves(t *testing.T) {
	loaded := func(r *history.Record) { r.Cores, r.Load1, r.MaxLoad, r.FreeMB, r.JobMB = 8, 12, 8, 9000, 1000 }
	recs := []history.Record{
		rec(func(r *history.Record) { r.RunID = "a" }),                                  // ran, passed
		rec(func(r *history.Record) { r.RunID = "a"; r.Exit = 1 }),                      // same run id failed: flaky
		rec(func(r *history.Record) { r.RunID = "b"; r.Exit = 1; loaded(r) }),           // failed under load, not flaky
		rec(func(r *history.Record) { r.RunID = "b" }),                                  // passes later: load explains the failure
		rec(func(r *history.Record) { r.WaitSecs = 0.4 }),                               // jitter, not held back
		rec(func(r *history.Record) { r.WaitSecs = 30; r.Lane = "b" }),                  // held back
		rec(func(r *history.Record) { r.WaitSecs = 90; r.Lane = "c"; r.Forced = true }), // held back
		rec(func(r *history.Record) { r.Cache = "hit"; r.Lane = "d" }),
		rec(func(r *history.Record) { r.Cache = "coalesced" }),
		rec(func(r *history.Record) { r.Machine = "box1"; r.Pools = "port=4100,4101 pg=pg2" }),
		rec(func(r *history.Record) { r.Machine = "box2"; r.Pools = "port=4102" }),
		rec(func(r *history.Record) { r.Machine = "box1" }),
		rec(func(r *history.Record) { r.Light = true; r.Lane = "light-only" }),                 // light: ignored
		rec(func(r *history.Record) { r.Result = history.OutcomeGaveUp; r.Lane = "gone" }),     // never ran
		rec(func(r *history.Record) { r.Result = history.OutcomeCancelled }),                   // never ran
		rec(func(r *history.Record) { r.Result = history.OutcomeInterrupted; r.Machine = "" }), // died waiting
		rec(func(r *history.Record) { r.End = t0.Add(-time.Second); r.Lane = "old" }),          // before the window
		rec(func(r *history.Record) { r.End = t1; r.Lane = "future" }),                         // window is half open
	}
	got := Compute(recs, t0, t1)
	want := Receipts{
		From: t0, To: t1,
		Runs: 10, DuplicatesAvoided: 2, CacheHits: 1, Coalesced: 1, HeldBack: 2,
		Flaky: 1, UnderLoad: 1, RemoteRuns: 3, RemoteMachines: 2, PoolValues: 4, Lanes: 4,
		WaitP50S: 0, WaitP95S: 90,
	}
	if got != want {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func TestComputeEmptyAndOnlyReuse(t *testing.T) {
	if got := Compute(nil, t0, t1); got != (Receipts{From: t0, To: t1}) {
		t.Fatalf("empty ledger: %+v", got)
	}
	got := Compute([]history.Record{rec(func(r *history.Record) { r.Cache = "hit" })}, t0, t1)
	if got.Runs != 0 || got.DuplicatesAvoided != 1 || got.Lanes != 1 || got.WaitP95S != 0 {
		t.Fatalf("reuse only: %+v", got)
	}
}

func TestFlakyNeedsAnUnexplainedFailureAndAPassOnOneRunID(t *testing.T) {
	cases := map[string][]history.Record{
		"fail only":        {rec(func(r *history.Record) { r.Exit = 1 })},
		"pass only":        {rec(nil), rec(nil)},
		"different ids":    {rec(nil), rec(func(r *history.Record) { r.RunID = "x"; r.Exit = 1 })},
		"no run id":        {rec(func(r *history.Record) { r.RunID = "" }), rec(func(r *history.Record) { r.RunID = ""; r.Exit = 1 })},
		"replay is no run": {rec(func(r *history.Record) { r.Exit = 1 }), rec(func(r *history.Record) { r.Cache = "hit" })},
	}
	for name, recs := range cases {
		if got := Compute(recs, t0, t1).Flaky; got != 0 {
			t.Errorf("%s: flaky = %d, want 0", name, got)
		}
	}
	both := []history.Record{rec(nil), rec(func(r *history.Record) { r.Exit = 3 })}
	if got := Compute(both, t0, t1).Flaky; got != 1 {
		t.Errorf("pass and fail on one id: flaky = %d, want 1", got)
	}
}

func TestPoolValues(t *testing.T) {
	for in, want := range map[string]int{
		"": 0, "pg=pg1": 1, "port=4100,4101": 2, "port=4100,4101 pg=pg2 browser=b1": 4, "junk": 0, "x=": 0,
	} {
		if got := poolValues(in); got != want {
			t.Errorf("poolValues(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestParseWindow(t *testing.T) {
	for name, want := range map[string]time.Duration{"": 7 * 24 * time.Hour, "day": 24 * time.Hour, "week": 7 * 24 * time.Hour, "month": 30 * 24 * time.Hour} {
		if _, d, err := ParseWindow(name); err != nil || d != want {
			t.Errorf("ParseWindow(%q) = %v, %v", name, d, err)
		}
	}
	if _, _, err := ParseWindow("year"); err == nil {
		t.Error("an unknown window must be refused")
	}
}
