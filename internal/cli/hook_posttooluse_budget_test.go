package cli

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/deadline"
	"github.com/LysCongThanh/heavyq/internal/history/historytest"
	"github.com/LysCongThanh/heavyq/internal/ledgerindex"
)

// The mentor hook reads the index, never the ledger: on a 100,000-record ledger
// the work of one call (parse, config, lane records, evaluate, state write)
// stays inside the 20 ms promise of the hook (process start excluded: the
// deadline budget leaves 5 ms for it). Timing test: skipped under -short and
// -race, which make timings meaningless.
func TestMentorHookWorkUnderBudgetOn100kLedger(t *testing.T) {
	if testing.Short() || historytest.RaceEnabled {
		t.Skip("timing test: needs a full run without -race")
	}
	dir := t.TempDir()
	ledger := filepath.Join(dir, "history-2026-11.jsonl")
	t.Setenv("HEAVYQ_HISTORY", ledger)
	t.Setenv("HEAVYQ_DIR", filepath.Join(dir, "q"))
	last, err := historytest.Write(ledger, historytest.Options{N: 100000, End: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ledgerindex.Reindex(ledger); err != nil {
		t.Fatal(err)
	}
	payload := postHook(dir, "heavyq 4 --lane "+last.Lane+" -- go test ./...", "PostToolUse")
	var took []time.Duration
	for i := 0; i < 15; i++ {
		_ = os.RemoveAll(filepath.Join(dir, "q", "mentor")) // every call is a first look at the record
		start := time.Now()
		if _, err := postToolUseNote("", false, []byte(payload)); err != nil {
			t.Fatal(err)
		}
		took = append(took, time.Since(start))
	}
	sort.Slice(took, func(i, j int) bool { return took[i] < took[j] })
	t.Logf("mentor hook work: best %v, median %v", took[0], took[len(took)/2])
	if took[0] > deadline.HookBudget {
		t.Fatalf("the mentor took %v at best, budget is %v", took[0], deadline.HookBudget)
	}
}
