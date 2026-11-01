package history

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultPathRotatesByMonthUnlessPinned(t *testing.T) {
	t.Setenv("HEAVYQ_HISTORY", "")
	t.Setenv("XDG_STATE_HOME", "/state")
	nov := time.Date(2026, 11, 1, 0, 30, 0, 0, time.FixedZone("UTC+7", 7*3600)) // still October in UTC
	if got, want := defaultPathAt(nov), "/state/heavyq/history-2026-10.jsonl"; got != want {
		t.Fatalf("got %s want %s", got, want)
	}
	if got, want := defaultPathAt(nov.Add(24*time.Hour)), "/state/heavyq/history-2026-11.jsonl"; got != want {
		t.Fatalf("got %s want %s", got, want)
	}
	t.Setenv("HEAVYQ_HISTORY", "/x/pinned.jsonl")
	if got := defaultPathAt(nov); got != "/x/pinned.jsonl" {
		t.Fatalf("HEAVYQ_HISTORY must be used exactly, got %s", got)
	}
}

// monthFile writes records "<month>-<i>" (Cmd) into history-<month>.jsonl.
func monthFile(t *testing.T, dir, name string, cmds ...string) {
	t.Helper()
	for i, c := range cmds {
		r := Record{End: time.Date(2026, 1, 1+i, 0, 0, 0, 0, time.UTC), Cmd: c}
		if err := Append(filepath.Join(dir, name), r); err != nil {
			t.Fatal(err)
		}
	}
}

func cmds(recs []Record) []string {
	var out []string
	for _, r := range recs {
		out = append(out, r.Cmd)
	}
	return out
}

func TestReadLastSpansRotatedFilesNewestLast(t *testing.T) {
	dir := t.TempDir()
	// Creation order is deliberately not name order.
	monthFile(t, dir, "history-2026-11.jsonl", "nov-1", "nov-2")
	monthFile(t, dir, "history.jsonl", "legacy-1", "legacy-2") // the pre-rotation file
	monthFile(t, dir, "history-2026-09.jsonl", "sep-1")
	monthFile(t, dir, "history-2026-10.jsonl", "oct-1", "oct-2")
	if err := os.WriteFile(filepath.Join(dir, "history-notes.jsonl"), []byte(`{"cmd":"stranger"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Any monthly name addresses the set, even one that does not exist yet.
	current := filepath.Join(dir, "history-2026-12.jsonl")

	all, err := ReadLast(current, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"legacy-1", "legacy-2", "sep-1", "oct-1", "oct-2", "nov-1", "nov-2"}
	if fmt.Sprint(cmds(all)) != fmt.Sprint(want) {
		t.Fatalf("all: %v want %v", cmds(all), want)
	}
	for n := 1; n <= len(want)+2; n++ {
		got, err := ReadLast(current, n)
		if err != nil {
			t.Fatal(err)
		}
		exp := want[max(len(want)-n, 0):]
		if fmt.Sprint(cmds(got)) != fmt.Sprint(exp) {
			t.Fatalf("n=%d: %v want %v", n, cmds(got), exp)
		}
	}
}

func TestPinnedFileIsNeverRotatedOrWidened(t *testing.T) {
	dir := t.TempDir()
	monthFile(t, dir, "history-2026-10.jsonl", "oct")
	monthFile(t, dir, "history.jsonl", "pinned-1", "pinned-2")
	got, err := ReadLast(filepath.Join(dir, "history.jsonl"), 0)
	if err != nil || fmt.Sprint(cmds(got)) != "[pinned-1 pinned-2]" {
		t.Fatalf("a path that is not history-YYYY-MM.jsonl must read exactly that file: %v %v", cmds(got), err)
	}
}

func TestRotatedSetMissingDirIsEmpty(t *testing.T) {
	recs, err := ReadLast(filepath.Join(t.TempDir(), "nope", "history-2026-11.jsonl"), 5)
	if err != nil || len(recs) != 0 {
		t.Fatalf("%v %v", recs, err)
	}
}
