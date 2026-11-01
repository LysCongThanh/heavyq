package snapshot

import (
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
)

func TestComputeInsights(t *testing.T) {
	to := time.Date(2026, 11, 1, 22, 0, 0, 0, time.UTC)
	from := to.Add(-time.Hour)
	at := func(m int) time.Time { return from.Add(time.Duration(m) * time.Minute) }
	recs := []history.Record{
		{End: at(-5), Lane: "old", WaitSecs: 999},                            // before the window
		{End: at(1), Lane: "a", WaitSecs: 10, RunSecs: 60, CPUSecs: 200},     // ran ok
		{End: at(2), Lane: "a", WaitSecs: 30, RunSecs: 30, Exit: 1},          // failed
		{End: at(31), Lane: "b", WaitSecs: 600, Result: "gave_up", Exit: 75}, // gave up
		{End: at(32), Lane: "b", Cache: "hit"},
		{End: at(33), Lane: "b", Cache: "coalesced"},
		{End: at(40), Lane: "b", WaitSecs: 900, RunSecs: 10, Forced: true},
		{End: at(41), Lane: "c", Result: "cancelled", Exit: 78},
		{End: at(42), Lane: "c", Result: "interrupted", Exit: 130}, // stopped while waiting: not a run
		{End: at(59), Machine: "local", Lane: "c", WaitSecs: 5, RunSecs: 5, Exit: 130, Result: "interrupted"},
	}
	in := ComputeInsights(recs, from, to, 2)
	if in.Runs != 4 || in.Failed != 1 || in.GaveUp != 1 || in.Cancelled != 1 || in.Interrupted != 2 ||
		in.Forced != 1 || in.CacheHits != 1 || in.Coalesced != 1 {
		t.Fatalf("counts = %+v", in)
	}
	if in.WaitP50S != 10 || in.WaitP95S != 900 || in.WaitMaxS != 900 {
		t.Fatalf("waits p50 %.0f p95 %.0f max %.0f", in.WaitP50S, in.WaitP95S, in.WaitMaxS)
	}
	if in.RunS != 105 || in.CPUS != 200 {
		t.Fatalf("run %.0f cpu %.0f", in.RunS, in.CPUS)
	}
	if len(in.Buckets) != 2 || in.Buckets[0].Runs != 2 || in.Buckets[1].Runs != 2 || in.Buckets[1].GaveUp != 1 {
		t.Fatalf("buckets = %+v", in.Buckets)
	}
	if in.Lanes[0].Lane != "a" || in.Lanes[0].Runs != 2 || in.Lanes[1].Lane != "b" || in.Lanes[1].GaveUp != 1 {
		t.Fatalf("lanes = %+v", in.Lanes)
	}
}
