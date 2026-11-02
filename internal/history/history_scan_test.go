package history

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestEachWalksTheRotatedSetOldestFirst(t *testing.T) {
	dir := t.TempDir()
	monthFile(t, dir, "history.jsonl", "legacy")
	monthFile(t, dir, "history-2026-10.jsonl", "o1", "o2")
	monthFile(t, dir, "history-2026-11.jsonl", "n1")
	var got []string
	last, size, err := Each(filepath.Join(dir, "history-2026-11.jsonl"), func(r Record) { got = append(got, r.Cmd) })
	if err != nil || strings.Join(got, ",") != "legacy,o1,o2,n1" {
		t.Fatalf("got %v, %v", got, err)
	}
	if filepath.Base(last) != "history-2026-11.jsonl" || size == 0 {
		t.Fatalf("ended at %s (%d bytes)", last, size)
	}
	// A month with no file yet is not listed: the walk ends at the newest file that exists.
	last, _, _ = Each(filepath.Join(dir, "history-2026-12.jsonl"), func(Record) {})
	if filepath.Base(last) != "history-2026-11.jsonl" {
		t.Fatalf("ended at %s", last)
	}
}

func TestEachFromContinuesAtAnOffset(t *testing.T) {
	p := filepath.Join(t.TempDir(), "history.jsonl")
	monthFile(t, filepath.Dir(p), "history.jsonl", "a", "b")
	var first []string
	size, err := EachFrom(p, 0, func(r Record) { first = append(first, r.Cmd) })
	if err != nil || len(first) != 2 {
		t.Fatalf("%v %v", first, err)
	}
	monthFile(t, filepath.Dir(p), "history.jsonl", "c")
	var next []string
	end, err := EachFrom(p, size, func(r Record) { next = append(next, r.Cmd) })
	if err != nil || strings.Join(next, ",") != "c" || end <= size {
		t.Fatalf("got %v end %d (was %d), %v", next, end, size, err)
	}
	// A file that is smaller than the offset, or gone, is not the file the offset came from.
	if _, err := EachFrom(p, end+100, func(Record) {}); err == nil {
		t.Fatal("offset past the end must be an error")
	}
	if _, err := EachFrom(filepath.Join(filepath.Dir(p), "gone.jsonl"), 5, func(Record) {}); err == nil {
		t.Fatal("a vanished file with an offset must be an error")
	}
	if n, err := EachFrom(filepath.Join(filepath.Dir(p), "gone.jsonl"), 0, func(Record) {}); err != nil || n != 0 {
		t.Fatalf("a file that never existed is empty: %d %v", n, err)
	}
}
