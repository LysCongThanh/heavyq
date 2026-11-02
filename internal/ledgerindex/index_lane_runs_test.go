package ledgerindex

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
)

func TestLaneRunsFromIndexAndFromTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history-2026-11.jsonl")
	at := time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC)
	for i, lane := range []string{"a", "b", "a", "a"} {
		at = at.Add(time.Minute)
		r := history.Record{End: at, Lane: lane, Cmd: "go test", Exit: i % 2}
		if err := Append(path, r); err != nil {
			t.Fatal(err)
		}
	}
	got, src, err := LaneRuns(path, "a")
	if err != nil || src != FromIndex || len(got) != 3 || got[0].End.After(got[2].End) {
		t.Fatalf("index: %d records from %s, %v", len(got), src, err)
	}
	if got, _, _ := LaneRuns(path, "nobody"); len(got) != 0 {
		t.Errorf("unknown lane has %d records", len(got))
	}
	// A line written behind the index's back makes it stale: the tail answers.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"end":"2026-11-02T09:00:00Z","lane":"a","cmd":"x"}` + "\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	got, src, err = LaneRuns(path, "a")
	if err != nil || src != FromTail || len(got) != 4 {
		t.Fatalf("tail: %d records from %s, %v", len(got), src, err)
	}
}
