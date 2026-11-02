package ledgerindex

import (
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
)

// The index built record by record is byte-for-byte the index rebuilt from the
// ledger, over random sequences that also rotate the month, append behind the
// index's back (an older heavyq), and reindex in the middle. Run with the real
// bounds and with tiny ones that force truncation and eviction.
func TestIncrementalIndexEqualsRebuild(t *testing.T) {
	for _, small := range []bool{false, true} {
		for seed := int64(1); seed <= 25; seed++ {
			if small {
				smallBounds(t)
			}
			rng := rand.New(rand.NewSource(seed))
			dir := t.TempDir()
			at := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
			for i, n := 0, 20+rng.Intn(120); i < n; i++ {
				at = at.Add(time.Duration(1+rng.Intn(20)) * time.Hour)
				path := filepath.Join(dir, "history-"+at.Format("2006-01")+".jsonl")
				rec := randomRecord(rng, at)
				switch rng.Intn(12) {
				case 0:
					if err := history.Append(path, rec); err != nil { // not through the index
						t.Fatal(err)
					}
				case 1:
					if _, _, err := Reindex(path); err != nil {
						t.Fatal(err)
					}
				default:
					if err := Append(path, rec); err != nil {
						t.Fatal(err)
					}
				}
			}
			path := filepath.Join(dir, "history-"+at.Format("2006-01")+".jsonl")
			if err := Append(path, randomRecord(rng, at)); err != nil { // bring it up to date
				t.Fatal(err)
			}
			incremental := snapshotDir(t, Dir(path))
			if _, _, err := Reindex(path); err != nil {
				t.Fatal(err)
			}
			label := "seed " + string(rune('0'+seed/10)) + string(rune('0'+seed%10))
			if small {
				label += " small bounds"
			}
			sameSnapshots(t, label, incremental, snapshotDir(t, Dir(path)))
			if t.Failed() {
				return
			}
			if small {
				LaneRecords, TreeRunsKept, maxOutcomes, maxLanes, maxTrees = 50, 32, 64, 128, 256
			}
		}
	}
}

func TestAppendIndexesLaneAndTree(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history-2026-11.jsonl")
	at := time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC)
	rec := history.Record{End: at, Lane: "api", Cmd: "go test", RunID: "r1", Repo: "R", Tree: "T", Exit: 1}
	for i := 0; i < 3; i++ {
		rec.End, rec.Exit = at.Add(time.Duration(i)*time.Minute), []int{1, 1, 0}[i]
		if err := Append(path, rec); err != nil {
			t.Fatal(err)
		}
	}
	last, src, err := LastOfLane(path, "api")
	if err != nil || src != FromIndex || last == nil || last.Exit != 0 {
		t.Fatalf("last of lane: %+v %v %v", last, src, err)
	}
	runs, src, err := TreeRuns(path, "R", "T")
	if err != nil || src != FromIndex || len(runs) != 3 {
		t.Fatalf("tree runs: %d %v %v", len(runs), src, err)
	}
	var d treeDoc
	if err := readJSON(treeFile(Dir(path), "R", "T"), &d); err != nil {
		t.Fatal(err)
	}
	if len(d.Outcomes) != 1 || d.Outcomes[0].Key != "r1" || d.Outcomes[0].Pass != 1 || d.Outcomes[0].Fail != 2 || d.Outcomes[0].Exit != 0 {
		t.Fatalf("outcomes: %+v", d.Outcomes)
	}
	if none, src, err := TreeRuns(path, "R", "other"); err != nil || src != FromIndex || len(none) != 0 {
		t.Fatalf("unknown tree: %v %v %v", none, src, err)
	}
}

// Records appended behind the index's back make it stale: readers say so by
// falling back to the tail (and still find the record), and the next Append
// catches up from the recorded offset without a rebuild.
func TestStaleIndexFallsBackThenCatchesUp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history-2026-11.jsonl")
	at := time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC)
	mk := func(i int, tree string) history.Record {
		return history.Record{End: at.Add(time.Duration(i) * time.Minute), Lane: "a", Cmd: "x", Repo: "R", Tree: tree}
	}
	if err := Append(path, mk(0, "T1")); err != nil {
		t.Fatal(err)
	}
	if err := history.Append(path, mk(1, "T2")); err != nil {
		t.Fatal(err)
	}
	runs, src, err := TreeRuns(path, "R", "T2")
	if err != nil || src != FromTail || len(runs) != 1 {
		t.Fatalf("stale read: %d %v %v", len(runs), src, err)
	}
	if st, _ := StatsOf(path); st.State != "stale" {
		t.Fatalf("state %q, want stale", st.State)
	}
	before := snapshotDir(t, Dir(path))
	if err := Append(path, mk(2, "T3")); err != nil {
		t.Fatal(err)
	}
	if runs, src, _ := TreeRuns(path, "R", "T2"); src != FromIndex || len(runs) != 1 {
		t.Fatalf("after catch-up: %d %v", len(runs), src)
	}
	if st, _ := StatsOf(path); st.State != "fresh" || st.Records != 3 || len(before) == 0 {
		t.Fatalf("stats after catch-up: %+v", st)
	}
}

// A missing, corrupt or foreign index never gives a wrong answer: reads fall
// back to the tail, and the next Append rebuilds.
func TestBrokenIndexFallsBackAndHeals(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history-2026-11.jsonl")
	at := time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC)
	rec := history.Record{End: at, Lane: "a", Cmd: "x", Repo: "R", Tree: "T"}
	for i := 0; i < 3; i++ {
		if err := Append(path, rec); err != nil {
			t.Fatal(err)
		}
	}
	dir, total := Dir(path), 3
	for name, damage := range map[string]func(){
		"corrupt tree file": func() { _ = os.WriteFile(treeFile(dir, "R", "T"), []byte("{nope"), 0o600) },
		"corrupt meta":      func() { _ = os.WriteFile(filepath.Join(dir, "meta.json"), []byte("{"), 0o600) },
		"no meta":           func() { _ = os.Remove(filepath.Join(dir, "meta.json")) },
		"future schema":     func() { _ = os.WriteFile(filepath.Join(dir, "meta.json"), []byte(`{"v":99}`), 0o600) },
		"no manifest":       func() { _ = os.Remove(filepath.Join(dir, "manifest.json")) },
	} {
		damage()
		if runs, src, err := TreeRuns(path, "R", "T"); err != nil || len(runs) != total || (name != "no manifest" && src != FromTail) {
			t.Fatalf("%s: %d runs from %s, %v", name, len(runs), src, err)
		}
		if err := Append(path, rec); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		total++
		if _, src, _ := TreeRuns(path, "R", "T"); src != FromIndex {
			t.Fatalf("%s: index not healed by the next Append", name)
		}
	}
}

// The first record of a new month finds the old file's index and carries on:
// no rebuild, and readers already trust the index before that record exists.
func TestIndexSurvivesMonthRotation(t *testing.T) {
	dir := t.TempDir()
	oct, nov := filepath.Join(dir, "history-2026-10.jsonl"), filepath.Join(dir, "history-2026-11.jsonl")
	at := time.Date(2026, 10, 31, 23, 0, 0, 0, time.UTC)
	rec := history.Record{End: at, Lane: "a", Cmd: "x", Repo: "R", Tree: "T"}
	if err := Append(oct, rec); err != nil {
		t.Fatal(err)
	}
	if _, src, _ := TreeRuns(nov, "R", "T"); src != FromIndex {
		t.Fatalf("a new month with no record yet must still read the index, got %s", src)
	}
	rec.End = at.Add(2 * time.Hour)
	if err := Append(nov, rec); err != nil {
		t.Fatal(err)
	}
	runs, src, _ := TreeRuns(nov, "R", "T")
	if src != FromIndex || len(runs) != 2 {
		t.Fatalf("got %d runs from %s", len(runs), src)
	}
}

// Writers in several goroutines (they take the lock through separate file
// descriptions, as separate processes do) leave an index that is the rebuild.
func TestConcurrentAppendsKeepIndexExact(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history-2026-11.jsonl")
	at := time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC)
	var wg sync.WaitGroup
	for g := 0; g < 6; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(int64(g)))
			for i := 0; i < 15; i++ {
				if err := Append(path, randomRecord(rng, at.Add(time.Duration(g*100+i)*time.Second))); err != nil {
					t.Error(err)
				}
			}
		}(g)
	}
	wg.Wait()
	if st, err := StatsOf(path); err != nil || st.State != "fresh" || st.Records != 90 {
		t.Fatalf("stats: %+v %v", st, err)
	}
	incremental := snapshotDir(t, Dir(path))
	if _, _, err := Reindex(path); err != nil {
		t.Fatal(err)
	}
	sameSnapshots(t, "concurrent", incremental, snapshotDir(t, Dir(path)))
}
