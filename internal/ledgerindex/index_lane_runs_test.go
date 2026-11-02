package ledgerindex

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
)

func TestLaneRunsIndexAndFallbackAgree(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history-2026-11.jsonl")
	at := time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC)
	for i := 0; i < 6; i++ {
		lane := []string{"a", "b"}[i%2]
		rec := history.Record{ID: history.NewID(at), End: at.Add(time.Duration(i) * time.Minute), Lane: lane, Cmd: "x"}
		if err := Append(path, rec); err != nil {
			t.Fatal(err)
		}
	}
	fromIndex, src, err := LaneRuns(path, "a")
	if err != nil || src != FromIndex || len(fromIndex) != 3 {
		t.Fatalf("index: %d %v %v", len(fromIndex), src, err)
	}
	if none, src, err := LaneRuns(path, "never"); err != nil || src != FromIndex || len(none) != 0 {
		t.Fatalf("unknown lane: %d %v %v", len(none), src, err)
	}
	// Break the index: the answer comes from the ledger tail and is the same.
	if err := os.Remove(filepath.Join(Dir(path), "meta.json")); err != nil {
		t.Fatal(err)
	}
	fromTail, src, err := LaneRuns(path, "a")
	if err != nil || src != FromTail || len(fromTail) != len(fromIndex) {
		t.Fatalf("tail: %d %v %v", len(fromTail), src, err)
	}
	for i := range fromTail {
		if fromTail[i].ID != fromIndex[i].ID {
			t.Fatalf("record %d differs: %s vs %s", i, fromTail[i].ID, fromIndex[i].ID)
		}
	}
}
