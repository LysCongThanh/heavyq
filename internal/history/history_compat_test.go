package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// v1Line is a line as heavyq 0.3 wrote it: no v, no id, no identity fields.
const v1Line = `{"end":"2026-10-30T10:00:00Z","machine":"local","lane":"api","prio":4,"wait_s":1,"run_s":2,"exit":0,"cmd":"go test ./..."}`

func TestMixedV1AndV2LinesLoad(t *testing.T) {
	p := filepath.Join(t.TempDir(), "history.jsonl")
	if err := os.WriteFile(p, []byte(v1Line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	end := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	want := Record{End: end, Machine: "local", Lane: "api", Prio: 4, Exit: 1, Cmd: "go test ./...",
		Session: "sess-1", Item: "A1", Gate: "ready", Repo: "r", Worktree: "/w", Subdir: "pkg",
		Commit: "c0ffee", Tree: "abc+def", Dirty: true}
	if err := Append(p, want); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, append(mustRead(t, p), []byte("garbage\n"+`{"v":3,"id":"X","future_field":1,"end":"2026-11-02T00:00:00Z","cmd":"later"}`+"\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	recs, err := ReadLast(p, 0)
	if err != nil || len(recs) != 3 {
		t.Fatalf("got %d records, err %v", len(recs), err)
	}
	if recs[0].Version() != 1 || recs[0].ID != "" || recs[0].Tree != "" || recs[0].Cmd != "go test ./..." {
		t.Fatalf("v1 line: %+v", recs[0])
	}
	got := recs[1]
	if got.Version() != SchemaVersion || len(got.ID) != 26 {
		t.Fatalf("v2 line lacks schema or id: %+v", got)
	}
	got.V, got.ID = 0, ""
	if got != want {
		t.Fatalf("v2 round trip:\n got  %+v\n want %+v", got, want)
	}
	if recs[2].Version() != 3 || recs[2].Cmd != "later" {
		t.Fatalf("unknown future fields must be ignored: %+v", recs[2])
	}
}

func TestAppendStampsSchemaAndKeepsGivenID(t *testing.T) {
	p := filepath.Join(t.TempDir(), "h.jsonl")
	if err := Append(p, Record{V: 1, Cmd: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := Append(p, Record{ID: "FIXED", Cmd: "b"}); err != nil {
		t.Fatal(err)
	}
	recs, _ := ReadLast(p, 0)
	if recs[0].V != SchemaVersion || recs[0].ID == "" || recs[1].ID != "FIXED" {
		t.Fatalf("%+v", recs)
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(strings.Split(string(mustRead(t, p)), "\n")[0]), &raw); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"session", "item", "gate", "repo", "commit", "tree", "dirty"} {
		if _, ok := raw[k]; ok {
			t.Errorf("empty field %q must not be written", k)
		}
	}
}

// Appends from many goroutines (and processes) must never interleave bytes:
// each record is one write.
func TestConcurrentAppendsStayWholeLines(t *testing.T) {
	p := filepath.Join(t.TempDir(), "h.jsonl")
	const writers, each = 16, 40
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < each; i++ {
				if err := Append(p, Record{End: time.Now(), Cmd: strings.Repeat("x", 300), Tree: strings.Repeat("t", 80)}); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	recs, err := ReadLast(p, 0)
	if err != nil || len(recs) != writers*each {
		t.Fatalf("got %d records (err %v), want %d: lines were torn", len(recs), err, writers*each)
	}
	seen := map[string]bool{}
	for _, r := range recs {
		if seen[r.ID] {
			t.Fatalf("duplicate id %s", r.ID)
		}
		seen[r.ID] = true
	}
}

func TestValidTag(t *testing.T) {
	for _, ok := range []string{"a", "s1", "3f2a9c1e-77aa-4c1d-b3a0-0123456789ab", "A.b_c:d-e", strings.Repeat("x", 80)} {
		if !ValidTag(ok) {
			t.Errorf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"", "a b", "a/b", "a;b", "$(x)", "é", "a\nb", "'q'", strings.Repeat("x", 81)} {
		if ValidTag(bad) {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
