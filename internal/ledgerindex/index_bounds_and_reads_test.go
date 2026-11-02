package ledgerindex

import (
	"math/rand"
	"path/filepath"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
)

func TestIndexBoundsAndEvictionSignal(t *testing.T) {
	smallBounds(t)
	path := filepath.Join(t.TempDir(), "history-2026-11.jsonl")
	at := time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC)
	for i := 0; i < 12; i++ { // 12 trees, room for 5; one lane, room for 5 records
		rec := history.Record{End: at.Add(time.Duration(i) * time.Minute), Lane: "a", Cmd: "x", Repo: "R", Tree: string(rune('A' + i))}
		if err := Append(path, rec); err != nil {
			t.Fatal(err)
		}
	}
	st, _ := StatsOf(path)
	if st.Trees != 5 || st.Lanes != 1 || !st.Evicted {
		t.Fatalf("stats: %+v", st)
	}
	var d laneDoc
	if err := readJSON(laneFile(Dir(path), "a"), &d); err != nil || len(d.Records) != 5 {
		t.Fatalf("lane keeps %d records, %v", len(d.Records), err)
	}
	// The newest tree is indexed; the oldest was evicted, so the index must
	// not claim "no runs": it falls back to the tail, which still has them.
	if runs, src, _ := TreeRuns(path, "R", "L"); src != FromIndex || len(runs) != 1 {
		t.Fatalf("newest tree: %d %v", len(runs), src)
	}
	if runs, src, _ := TreeRuns(path, "R", "A"); src != FromTail || len(runs) != 1 {
		t.Fatalf("evicted tree: %d %v", len(runs), src)
	}
	if runs, src, _ := TreeRuns(path, "R", "never"); src != FromTail || len(runs) != 0 {
		t.Fatalf("unknown tree with evictions: %d %v", len(runs), src)
	}
}

func TestRecentAndLastOfLaneMatchTheTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history-2026-11.jsonl")
	rng := rand.New(rand.NewSource(3))
	at := time.Now().Add(-70 * time.Minute)
	for i := 0; i < 60; i++ {
		at = at.Add(time.Minute)
		if err := Append(path, randomRecord(rng, at)); err != nil {
			t.Fatal(err)
		}
	}
	since := time.Now().Add(-30 * time.Minute)
	got, src, err := Recent(path, since)
	if err != nil || src != FromIndex {
		t.Fatalf("recent: %v %v", src, err)
	}
	want, _ := history.ReadTail(path, history.TailLimit{Since: since})
	if len(got) != len(want) || len(got) == 0 {
		t.Fatalf("recent: %d records, tail has %d", len(got), len(want))
	}
	for i := range got {
		if got[i].ID != want[i].ID {
			t.Fatalf("record %d: %s vs %s", i, got[i].ID, want[i].ID)
		}
	}
	for _, lane := range []string{"lane0", "lane1", "lane2", "none"} {
		fromIdx, src, err := LastOfLane(path, lane)
		all, _ := history.ReadLast(path, 0)
		var wantLast *history.Record
		for i := len(all) - 1; i >= 0 && wantLast == nil; i-- {
			if all[i].Lane == lane {
				wantLast = &all[i]
			}
		}
		if err != nil || src != FromIndex || (fromIdx == nil) != (wantLast == nil) || (wantLast != nil && fromIdx.ID != wantLast.ID) {
			t.Fatalf("lane %s: %+v vs %+v (%v %v)", lane, fromIdx, wantLast, src, err)
		}
	}
}

func TestReindexRepairsAndReportsStats(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history-2026-11.jsonl")
	at := time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC)
	rng := rand.New(rand.NewSource(5))
	for i := 0; i < 20; i++ { // a ledger written before the index existed
		if err := history.Append(path, randomRecord(rng, at.Add(time.Duration(i)*time.Minute))); err != nil {
			t.Fatal(err)
		}
	}
	if st, _ := StatsOf(path); st.State != "missing" {
		t.Fatalf("before: %+v", st)
	}
	st, took, err := Reindex(path)
	if err != nil || st.State != "fresh" || st.Records != 20 || st.Lanes != 3 || st.Trees == 0 || st.Bytes == 0 || took <= 0 {
		t.Fatalf("after: %+v %v %v", st, took, err)
	}
	if runs, src, _ := TreeRuns(path, "repo0", "tree0"); src != FromIndex || len(runs) == 0 {
		t.Fatalf("read after reindex: %d %v", len(runs), src)
	}
}
