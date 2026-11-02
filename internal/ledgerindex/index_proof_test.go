package ledgerindex

import (
	"math/rand"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/proof"
)

// The proof computed from the index is the proof computed from a full scan of
// the ledger, for every (repo, tree) of random ledgers: passes, failures,
// flaky trees, runs under load, cache replays, give-ups and older lines
// without a run_id included. (Trees here have fewer runs than the index keeps;
// above that the index hands the question to the tail, tested separately.)
func TestProofViaIndexEqualsFullScan(t *testing.T) {
	for seed := int64(1); seed <= 40; seed++ {
		rng := rand.New(rand.NewSource(seed))
		path := filepath.Join(t.TempDir(), "history-2026-11.jsonl")
		at := time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC)
		for i, n := 0, 10+rng.Intn(60); i < n; i++ {
			at = at.Add(time.Minute)
			if err := Append(path, randomRecord(rng, at)); err != nil {
				t.Fatal(err)
			}
		}
		all, err := history.ReadLast(path, 0)
		if err != nil {
			t.Fatal(err)
		}
		for repo := 0; repo < 2; repo++ {
			for tree := 0; tree < 5; tree++ { // tree4 never runs: proves "missing" agrees too
				subj := subjectFor("repo"+string(rune('0'+repo)), "tree"+string(rune('0'+tree)))
				viaIndex, src, err := TreeRuns(path, subj.Repo, subj.Tree)
				if err != nil || src != FromIndex {
					t.Fatalf("seed %d: %v from %s", seed, err, src)
				}
				want := proof.Evaluate("ready", gateCmds, subj, all)
				got := proof.Evaluate("ready", gateCmds, subj, viaIndex)
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("seed %d %s/%s:\n via index %+v\n full scan %+v", seed, subj.Repo, subj.Tree, got, want)
				}
			}
		}
	}
}

// A tree with more runs than the index keeps is not answered from a cut list:
// the read comes from the ledger tail and the proof still agrees with the scan.
func TestProofOnBusyTreeFallsBackToTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history-2026-11.jsonl")
	at := time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC)
	cmd := gateCmds[0]
	for i := 0; i < TreeRunsKept+10; i++ {
		rec := history.Record{End: at.Add(time.Duration(i) * time.Second), Lane: "a", Machine: "local", Cmd: cmd,
			CmdHash: history.CmdHash(proof.Argv(cmd)), RunID: ridOf("tree0", cmd), Repo: "repo0", Tree: "tree0", Exit: i % 3 % 2}
		if err := Append(path, rec); err != nil {
			t.Fatal(err)
		}
	}
	subj := subjectFor("repo0", "tree0")
	recs, src, err := TreeRuns(path, subj.Repo, subj.Tree)
	if err != nil || src != FromTail || len(recs) != TreeRunsKept+10 {
		t.Fatalf("%d runs from %s, %v", len(recs), src, err)
	}
	all, _ := history.ReadLast(path, 0)
	if got, want := proof.Evaluate("g", gateCmds[:1], subj, recs), proof.Evaluate("g", gateCmds[:1], subj, all); !reflect.DeepEqual(got, want) {
		t.Fatalf("via tail %+v\n full scan %+v", got, want)
	}
}
