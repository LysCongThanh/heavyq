package history

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCmdHashIsSha256OfTheArgvJoinedWithNUL(t *testing.T) {
	sum := sha256.Sum256([]byte("sh\x00-c\x00go test ./..."))
	want := hex.EncodeToString(sum[:])
	if got := CmdHash([]string{"sh", "-c", "go test ./..."}); got != want || len(got) != 64 {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestCmdHashTellsApartWhatTheDisplayFormCannot(t *testing.T) {
	prefix := strings.Repeat("x", 300)
	a, b := CmdHash([]string{"sh", "-c", prefix + "1"}), CmdHash([]string{"sh", "-c", prefix + "2"})
	if a == b {
		t.Fatal("commands that differ after the cut must hash differently")
	}
	// Splitting differently is a different command, though the words are the same.
	if CmdHash([]string{"a b"}) == CmdHash([]string{"a", "b"}) || CmdHash([]string{"ab", "c"}) == CmdHash([]string{"a", "bc"}) {
		t.Fatal("argument boundaries must be part of the hash")
	}
}

func TestLoadedExplainsAMachineOverItsLimits(t *testing.T) {
	read := func(load float64, free int) Record {
		return Record{Exit: 1, Load1: load, FreeMB: free, Cores: 8, MaxLoad: 6, JobMB: 3072}
	}
	cases := []struct {
		name string
		r    Record
		why  string // "" = not loaded
	}{
		{"forced", Record{Forced: true}, "anti-starvation"},
		{"forced beats a calm reading", Record{Forced: true, Load1: 0.1, FreeMB: 9000, Cores: 8, MaxLoad: 6, JobMB: 3072}, "anti-starvation"},
		{"load over the limit", read(7.5, 9000), "load 7.50 >= 6.00"},
		{"load at the limit", read(6, 9000), "load 6.00 >= 6.00"},
		{"load under the limit", read(5.99, 9000), ""},
		{"RAM under the budget", read(0.5, 3071), "3071 MB free < 3072 MB"},
		{"RAM at the budget", read(0.5, 3072), ""},
		{"no free RAM at all", read(0.5, 0), "0 MB free"},
		{"older line: nothing is known", Record{Exit: 1}, ""},
		{"limits without a reading", Record{Exit: 1, MaxLoad: 6, JobMB: 3072}, ""},
		{"a reading without limits", Record{Exit: 1, Load1: 99, FreeMB: 1, Cores: 4}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := c.r.LoadWhy()
			if (c.why == "") != (got == "") || !strings.Contains(got, c.why) || c.r.Loaded() != (c.why != "") {
				t.Fatalf("LoadWhy = %q, want it to contain %q", got, c.why)
			}
		})
	}
}

// The new fields round-trip, are left out when there is nothing to say, and
// an old line without them loads with zero values.
func TestCmdHashAndMachineReadingRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "h.jsonl")
	want := Record{Cmd: "sh -c true", CmdHash: CmdHash([]string{"sh", "-c", "true"}), Load1: 2.25, FreeMB: 0, Cores: 8,
		MaxLoad: 6, JobMB: 3072, Forced: true}
	if err := Append(p, want); err != nil {
		t.Fatal(err)
	}
	if err := Append(p, Record{Cmd: "old"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	var first, second map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"cmd_hash", "load1", "cores", "max_load", "job_mb"} {
		if _, ok := first[k]; !ok {
			t.Errorf("field %q missing from %s", k, lines[0])
		}
		if _, ok := second[k]; ok {
			t.Errorf("empty field %q must not be written: %s", k, lines[1])
		}
	}
	recs, err := ReadLast(p, 0)
	if err != nil || len(recs) != 2 {
		t.Fatalf("%d records, err %v", len(recs), err)
	}
	got := recs[0]
	got.V, got.ID = 0, ""
	if got != want {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
	if recs[1].CmdHash != "" || recs[1].Cores != 0 || recs[1].Loaded() {
		t.Fatalf("a record without the fields: %+v", recs[1])
	}
}
