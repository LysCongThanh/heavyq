package history_test

import (
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/history/historytest"
)

// bigLedger writes the 100k-line synthetic ledger (about 55 MB) to a temp dir
// that the test removes.
func bigLedger(tb testing.TB) string {
	tb.Helper()
	p := filepath.Join(tb.TempDir(), "history-2026-11.jsonl")
	if _, err := historytest.Write(p, historytest.Options{N: 100000, End: time.Now()}); err != nil {
		tb.Fatal(err)
	}
	return p
}

func BenchmarkReadTail2000(b *testing.B) {
	p := bigLedger(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if recs, err := history.ReadTail(p, history.TailLimit{Max: 2000}); err != nil || len(recs) != 2000 {
			b.Fatal(len(recs), err)
		}
	}
}

func BenchmarkReadLastFullScan(b *testing.B) {
	p := bigLedger(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if recs, err := history.ReadLast(p, 2000); err != nil || len(recs) != 2000 {
			b.Fatal(len(recs), err)
		}
	}
}

// The tail of a 100k-line ledger comes back in well under the full scan, and
// the last 2,000 records in about 5 ms. The timing is the best of nine reads
// (a busy shared machine only ever makes a read slower) with 2x headroom; it
// is skipped under -short and the race detector, which make timings
// meaningless.
func TestReadTailLatencyOn100kLedger(t *testing.T) {
	if testing.Short() || historytest.RaceEnabled {
		t.Skip("timing test: needs a full run without -race")
	}
	p := bigLedger(t)
	best := func(read func()) time.Duration {
		var ds []time.Duration
		for i := 0; i < 9; i++ {
			start := time.Now()
			read()
			ds = append(ds, time.Since(start))
		}
		sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
		t.Logf("  runs: best %v, median %v", ds[0], ds[len(ds)/2])
		return ds[0]
	}
	tail := best(func() { _, _ = history.ReadTail(p, history.TailLimit{Max: 2000}) })
	full := best(func() { _, _ = history.ReadLast(p, 2000) })
	t.Logf("last 2000 of 100k lines: tail %v, full scan %v", tail, full)
	if tail > 10*time.Millisecond {
		t.Fatalf("tail read of 2000 records took %v, target < 5 ms", tail)
	}
	if tail*10 > full {
		t.Fatalf("tail %v is not 10x faster than the full scan %v", tail, full)
	}
}
