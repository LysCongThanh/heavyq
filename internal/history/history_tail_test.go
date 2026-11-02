package history

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// joined returns the Cmd of each record, to compare readers in one line.
func joined(recs []Record) string {
	var out []string
	for _, r := range recs {
		out = append(out, r.Cmd)
	}
	return strings.Join(out, ",")
}

func writeRaw(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func line(cmd string, end time.Time) string {
	return fmt.Sprintf(`{"v":2,"end":%q,"lane":"l","cmd":%q}`+"\n", end.Format(time.RFC3339Nano), cmd)
}

func TestReadTailEmptyAndMissing(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{"empty.jsonl": "", "blank.jsonl": "\n\n\r\n"} {
		p := filepath.Join(dir, name)
		writeRaw(t, p, content)
		if recs, err := ReadTail(p, TailLimit{Max: 5}); err != nil || len(recs) != 0 {
			t.Fatalf("%s: %v %v", name, recs, err)
		}
	}
	if recs, err := ReadTail(filepath.Join(dir, "none.jsonl"), TailLimit{}); err != nil || recs != nil {
		t.Fatalf("missing: %v %v", recs, err)
	}
}

func TestReadTailPartialLastLineAndCRLF(t *testing.T) {
	p := filepath.Join(t.TempDir(), "history.jsonl")
	now := time.Now()
	crlf := strings.ReplaceAll(line("a", now)+line("b", now), "\n", "\r\n")
	// The last line has no line ending: valid JSON is read, a torn write is skipped.
	writeRaw(t, p, crlf+strings.TrimSuffix(line("c", now), "\n"))
	if recs, _ := ReadTail(p, TailLimit{}); joined(recs) != "a,b,c" {
		t.Fatalf("complete unterminated line: %s", joined(recs))
	}
	writeRaw(t, p, crlf+`{"v":2,"end":"2026-11-0`)
	if recs, _ := ReadTail(p, TailLimit{}); joined(recs) != "a,b" {
		t.Fatalf("torn line: %s", joined(recs))
	}
	if recs, _ := ReadTail(p, TailLimit{Max: 1}); joined(recs) != "b" {
		t.Fatalf("max 1: %s", joined(recs))
	}
}

// A line over the limit is dropped whole, whatever chunk boundaries it spans,
// and the records on both sides still load.
func TestReadTailHugeLine(t *testing.T) {
	now := time.Now()
	huge := `{"cmd":"` + strings.Repeat("x", maxLineBytes+10) + `"}` + "\n"
	for _, chunk := range []int64{64 * 1024, 4096, 1000} {
		old := tailChunk
		tailChunk = chunk
		p := filepath.Join(t.TempDir(), "history.jsonl")
		writeRaw(t, p, line("a", now)+line("b", now)+huge+line("c", now))
		recs, err := ReadTail(p, TailLimit{})
		tailChunk = old
		if err != nil || joined(recs) != "a,b,c" {
			t.Fatalf("chunk %d: %s %v", chunk, joined(recs), err)
		}
		// huge line first in the file
		writeRaw(t, p, huge+line("c", now))
		tailChunk = chunk
		recs, err = ReadTail(p, TailLimit{})
		tailChunk = old
		if err != nil || joined(recs) != "c" {
			t.Fatalf("chunk %d, huge first: %s %v", chunk, joined(recs), err)
		}
	}
}

func TestReadTailSpansMonthlyFilesNewestFirst(t *testing.T) {
	dir := t.TempDir()
	monthFile(t, dir, "history-2026-09.jsonl", "s1", "s2")
	monthFile(t, dir, "history-2026-10.jsonl", "o1", "o2")
	monthFile(t, dir, "history-2026-11.jsonl", "n1")
	p := filepath.Join(dir, "history-2026-11.jsonl")
	for max, want := range map[int]string{1: "n1", 2: "o2,n1", 4: "o1,o2,n1" + "", 0: "s1,s2,o1,o2,n1"} {
		recs, err := ReadTail(p, TailLimit{Max: max})
		if err != nil {
			t.Fatal(err)
		}
		if max == 4 {
			want = "s2,o1,o2,n1"
		}
		if got := joined(recs); got != want {
			t.Fatalf("max %d: got %s want %s", max, got, want)
		}
	}
}

func TestReadTailSince(t *testing.T) {
	p := filepath.Join(t.TempDir(), "history.jsonl")
	base := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	var b strings.Builder
	for i := 0; i < 10; i++ {
		b.WriteString(line(fmt.Sprint("r", i), base.Add(time.Duration(i)*time.Minute)))
	}
	writeRaw(t, p, b.String())
	recs, _ := ReadTail(p, TailLimit{Since: base.Add(7 * time.Minute)})
	if got := joined(recs); got != "r7,r8,r9" {
		t.Fatalf("since: %s", got)
	}
	recs, _ = ReadTail(p, TailLimit{Since: base.Add(5 * time.Minute), Max: 2})
	if got := joined(recs); got != "r8,r9" {
		t.Fatalf("since+max: %s", got)
	}
}

// ReadTail(n) equals ReadLast(n) on random ledgers with corrupt lines, at every
// chunk size, so a chunk boundary can fall anywhere in a line.
func TestReadTailMatchesReadLast(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	now := time.Now()
	for iter := 0; iter < 40; iter++ {
		var b strings.Builder
		for i, n := 0, rng.Intn(60); i < n; i++ {
			switch rng.Intn(8) {
			case 0:
				b.WriteString("garbage not json\n")
			case 1:
				b.WriteString("\n")
			case 2:
				b.WriteString(strings.TrimSuffix(line(fmt.Sprint("x", i), now), "\n") + "\r\n")
			default:
				b.WriteString(line(fmt.Sprint("x", i, strings.Repeat("y", rng.Intn(300))), now))
			}
		}
		p := filepath.Join(t.TempDir(), "history.jsonl")
		writeRaw(t, p, b.String())
		for _, chunk := range []int64{3, 64, 500, 64 * 1024} {
			tailChunk = chunk
			for _, n := range []int{0, 1, 5, 1000} {
				want, _ := ReadLast(p, n)
				got, err := ReadTail(p, TailLimit{Max: n})
				if err != nil || joined(got) != joined(want) {
					tailChunk = 64 * 1024
					t.Fatalf("iter %d chunk %d n %d: got %q want %q (%v)", iter, chunk, n, joined(got), joined(want), err)
				}
			}
		}
		tailChunk = 64 * 1024
	}
}
