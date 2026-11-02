package ledgerindex

import (
	"fmt"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/proof"
)

// gateCmds are the commands of the gate the random ledgers run.
var gateCmds = []string{"gofmt -l .", "go vet ./...", "go test -race ./..."}

// ridOf is the run_id of cmd on tree in these tests (what Subject.RunID returns).
func ridOf(tree, cmd string) string { return fmt.Sprintf("rid:%s:%s", tree, cmd) }

// randomRecord makes one ledger record over a small domain, so trees and
// run_ids repeat: 3 lanes, 2 repos, 4 trees, the 3 gate commands, passes and
// failures, cache replays, runs under load, give-ups, subdir runs and lines
// without a run_id (older heavyq).
func randomRecord(rng *rand.Rand, at time.Time) history.Record {
	cmd := gateCmds[rng.Intn(len(gateCmds))]
	tree := fmt.Sprintf("tree%d", rng.Intn(4))
	r := history.Record{
		End: at, Machine: "local", Lane: fmt.Sprintf("lane%d", rng.Intn(3)), Prio: 5, RunSecs: float64(rng.Intn(30)),
		Cmd: cmd, CmdHash: history.CmdHash(proof.Argv(cmd)), RunID: ridOf(tree, cmd),
		Repo: fmt.Sprintf("repo%d", rng.Intn(2)), Worktree: "/w", Tree: tree, Commit: "c" + tree,
	}
	if rng.Intn(3) == 0 {
		r.Exit = 1 + rng.Intn(2)
	}
	switch rng.Intn(12) {
	case 0:
		r.Cache = "hit"
	case 1:
		r.Cache = "coalesced"
	case 2:
		r.Forced = true
	case 3:
		r.Result = history.OutcomeGaveUp
	case 4:
		r.Subdir = "sub"
	case 5:
		r.RunID = ""
	case 6:
		r.Tree, r.RunID = "", ""
	}
	return r
}

// subjectFor is the proof subject of (repo, tree): it computes run_ids like
// ridOf and says the environment cannot differ, so older lines match by hash.
func subjectFor(repo, tree string) proof.Subject {
	return proof.Subject{Repo: repo, Tree: tree, Commit: "c" + tree, EnvFree: true,
		RunID: func(argv []string) string { return ridOf(tree, argv[len(argv)-1]) }}
}

// snapshotDir returns every file under dir (except the lock) as path -> content.
func snapshotDir(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() == "lock" {
			return err
		}
		b, err := os.ReadFile(p)
		rel, _ := filepath.Rel(dir, p)
		out[rel] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func sameSnapshots(t *testing.T, label string, got, want map[string]string) {
	t.Helper()
	for name, w := range want {
		g, ok := got[name]
		switch {
		case !ok:
			t.Errorf("%s: %s missing from the incremental index", label, name)
		case g != w:
			t.Errorf("%s: %s differs\n incremental: %.300s\n rebuilt:     %.300s", label, name, g, w)
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("%s: %s only in the incremental index", label, name)
		}
	}
}

// smallBounds makes eviction and truncation happen with a few dozen records.
func smallBounds(t *testing.T) {
	t.Helper()
	l, r, o, ml, mt := LaneRecords, TreeRunsKept, maxOutcomes, maxLanes, maxTrees
	LaneRecords, TreeRunsKept, maxOutcomes, maxLanes, maxTrees = 5, 4, 3, 2, 5
	t.Cleanup(func() { LaneRecords, TreeRunsKept, maxOutcomes, maxLanes, maxTrees = l, r, o, ml, mt })
}
