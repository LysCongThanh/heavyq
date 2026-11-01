package history

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// maxFuzzInput bounds every fuzz input so one execution stays fast.
const maxFuzzInput = 64 << 10

// FuzzReadLast writes fuzzed bytes as a ledger file and reads it back with
// ReadLast. Corrupt lines are skipped, never fatal: no panic, no error for a
// readable file, at most n records, and a good line appended after the junk
// is always found.
func FuzzReadLast(f *testing.F) {
	good := `{"v":2,"id":"01J00000000000000000000000","end":"2026-01-02T03:04:05Z","machine":"local","lane":"api","prio":3,"wait_s":1.5,"run_s":2,"exit":0,"cmd":"go test ./..."}`
	for _, s := range []string{
		good + "\n",
		good + "\nnot json\n" + good + "\n",
		"\n\n\n",
		"{\"end\":\"bad time\"}\n" + good,
		`{"exit":"zero"}` + "\n" + `[1,2]` + "\n" + `null` + "\n" + `"s"` + "\n",
		"{\"cmd\":\"unterminated\n" + good + "\n",
		"\xff\xfe\x00\n" + good + "\r\n",
		`{"v":1,"end":"2025-05-05T05:05:05Z","machine":"m","lane":"l","prio":1,"wait_s":0,"run_s":0,"exit":2,"cmd":"x"}`,
		"",
	} {
		f.Add([]byte(s), 3)
	}
	f.Fuzz(func(t *testing.T, data []byte, n int) {
		if len(data) > maxFuzzInput {
			t.Skip("input over the fuzz bound")
		}
		n = n % 8 // keep the window small; negative and zero mean "all"
		dir := t.TempDir()
		path := filepath.Join(dir, "history.jsonl")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		recs, err := ReadLast(path, n)
		if err != nil {
			t.Fatalf("ReadLast(n=%d) failed on a readable file: %v\ninput %q", n, err, data)
		}
		if n > 0 && len(recs) > n {
			t.Fatalf("ReadLast(n=%d) returned %d records", n, len(recs))
		}

		// A valid line after arbitrary junk must still load, as the last record.
		want := Record{End: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), Machine: "fuzz", Lane: "x", Prio: 3, Cmd: "ok"}
		junk := append(bytes.Clone(data), '\n')
		if err := os.WriteFile(path, junk, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := Append(path, want); err != nil {
			t.Fatal(err)
		}
		recs, err = ReadLast(path, 1)
		if err != nil || len(recs) != 1 || recs[0].Machine != "fuzz" || recs[0].Cmd != "ok" {
			t.Fatalf("a good line after junk was lost: recs=%+v err=%v\ninput %q", recs, err, data)
		}
	})
}
