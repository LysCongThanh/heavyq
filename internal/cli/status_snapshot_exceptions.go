package cli

import (
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/ledgerindex"
	"github.com/LysCongThanh/heavyq/internal/proof"
	"github.com/LysCongThanh/heavyq/internal/redact"
	"github.com/LysCongThanh/heavyq/internal/repoconfig"
	"github.com/LysCongThanh/heavyq/internal/snapshot"
)

const (
	// gateActiveWindow: only work trees a lane ran in within this long are
	// checked for a red gate.
	gateActiveWindow = time.Hour
	// exceptionWindow is how far back the Tower looks at runs (loops, give-ups,
	// active work trees): the longest of the windows those use. The proof of a
	// work tree's code comes from the tree's own index, not from this window.
	exceptionWindow = max(snapshot.LoopWindow, snapshot.GiveUpWindow, gateActiveWindow)
	// maxGateWorktrees and gateProofBudget keep the snapshot build fast: at
	// most this many work trees are identified (git), within this time. The
	// rest are reported as skipped.
	maxGateWorktrees = 8
	gateProofBudget  = 1500 * time.Millisecond
	// gateProofTTL: the dashboard rebuilds the snapshot every few seconds;
	// a work tree's proof state is reused for this long.
	gateProofTTL = 10 * time.Second
)

// proofSource returns the ledger records that can decide a proof of a subject
// (see proofRecords). An error means the ledger could not be read.
type proofSource func(proof.Subject) ([]history.Record, error)

// fillExceptions sets s.Gates and s.Exceptions from the ledger and the
// work trees lanes ran in lately. Recent runs come from the lane index and
// proofs from the tree index (see internal/ledgerindex), never from a scan. A
// ledger that cannot be read proves nothing (fail closed), so the exceptions
// then come from live state only.
func fillExceptions(cfg *config.Config, s *snapshot.Snapshot, now time.Time) {
	recs, _, err := ledgerindex.Recent(history.DefaultPath(), now.Add(-exceptionWindow))
	if err != nil {
		recs = nil
		s.Gates = []snapshot.LaneGate{{State: snapshot.GateSkipped, Reason: "ledger unreadable: " + err.Error()}}
	} else {
		s.Gates = laneGates(cfg, recs, now, proofRecords)
	}
	s.Exceptions = snapshot.ComputeExceptions(*s, recs, now)
}

// activeTree is a work tree a lane ran in lately: the lane that ran there last and when.
type activeTree struct {
	lane, worktree string
	at             time.Time
}

// activeTrees lists the work trees lanes ran in within gateActiveWindow,
// most recent first. recs is the ledger, oldest first.
func activeTrees(recs []history.Record, now time.Time) []activeTree {
	var out []activeTree
	seen := map[string]bool{}
	for i := len(recs) - 1; i >= 0; i-- {
		r := recs[i]
		if r.Worktree == "" || r.Lane == "" || seen[r.Worktree] || now.Sub(r.End) > gateActiveWindow {
			continue
		}
		seen[r.Worktree] = true
		out = append(out, activeTree{r.Lane, r.Worktree, r.End})
	}
	return out
}

// laneGates is the proof state of the main gate in each recently active work
// tree. At most maxGateWorktrees are looked at, within gateProofBudget.
func laneGates(cfg *config.Config, recs []history.Record, now time.Time, src proofSource) []snapshot.LaneGate {
	out := []snapshot.LaneGate{}
	start := time.Now()
	for i, t := range activeTrees(recs, now) {
		if _, err := os.Stat(t.worktree); err != nil {
			continue // removed since: nothing to prove
		}
		var g snapshot.LaneGate
		switch {
		case i >= maxGateWorktrees:
			g = snapshot.LaneGate{State: snapshot.GateSkipped, Reason: "not checked: more active work trees than the Tower looks at"}
		case time.Since(start) > gateProofBudget:
			g = snapshot.LaneGate{State: snapshot.GateSkipped, Reason: "not checked: the proof budget of this snapshot ran out"}
		default:
			g = cachedWorktreeGate(cfg, t.worktree, src)
		}
		g.Lane, g.Worktree = t.lane, t.worktree
		out = append(out, g)
	}
	return out
}

var gateCache = struct {
	sync.Mutex
	m map[string]cachedGate
}{m: map[string]cachedGate{}}

type cachedGate struct {
	g  snapshot.LaneGate
	at time.Time
}

func cachedWorktreeGate(cfg *config.Config, worktree string, src proofSource) snapshot.LaneGate {
	gateCache.Lock()
	defer gateCache.Unlock()
	if c, ok := gateCache.m[worktree]; ok && time.Since(c.at) < gateProofTTL {
		return c.g
	}
	g := worktreeGate(cfg, worktree, src)
	gateCache.m[worktree] = cachedGate{g, time.Now()}
	return g
}

// worktreeGate evaluates the main gate of the repository around worktree on
// the code checked out there now. Without a heavyq.toml, or without an
// unambiguous gate, it says so instead of guessing.
func worktreeGate(cfg *config.Config, worktree string, src proofSource) snapshot.LaneGate {
	skip := func(why string) snapshot.LaneGate {
		return snapshot.LaneGate{State: snapshot.GateSkipped, Reason: why}
	}
	repo, err := repoconfig.Load(worktree)
	switch {
	case err != nil:
		return skip("heavyq.toml unusable: " + err.Error())
	case repo == nil:
		return skip("no heavyq.toml in " + filepath.Base(worktree))
	}
	gate, ok := pickGate(repo, "", io.Discard)
	if !ok {
		return skip("no gate to check: name one \"" + defaultGate + "\" in heavyq.toml")
	}
	subj := subjectOf(cfg, repo.Root)
	recs, err := src(subj)
	if err != nil {
		return skip("ledger unreadable: " + err.Error())
	}
	st := proof.Evaluate(gate.Name, gate.Run, subj, recs)
	out := snapshot.LaneGate{Gate: gate.Name, Tree: st.Tree, State: snapshot.GateUnproven}
	for _, c := range st.Commands {
		if c.State == proof.Failed {
			out.State = snapshot.GateFailed
			out.Failed = append(out.Failed, snapshot.GateFailure{
				Cmd: redact.Command(c.Cmd), At: c.At, Count: failureCount(recs, subj, c.Cmd), Class: failureClass(c),
			})
		}
	}
	switch {
	case out.State == snapshot.GateFailed:
	case st.Proven:
		out.State = snapshot.GateProven
	case st.Tree == "":
		out.State, out.Reason = snapshot.GateSkipped, st.Reason
	}
	return out
}

// failureClass says whether a failed gate command is noise (snapshot.ClassFlaky
// or snapshot.ClassUnderLoad) rather than a real failure, from the class the
// proof assigned it. A plain failure, or one the proof cannot classify, is
// "" and counts as real.
func failureClass(c proof.Command) string {
	switch c.Class {
	case proof.ClassFlaky:
		return snapshot.ClassFlaky
	case proof.ClassUnderLoad:
		return snapshot.ClassUnderLoad
	}
	return ""
}

// failureCount is how many conclusive runs of the gate command cmd failed on
// the code subj identifies. recs are the records of that code (proofRecords).
func failureCount(recs []history.Record, subj proof.Subject, cmd string) int {
	matches, n := proof.MatcherFor(cmd, subj), 0
	for _, r := range recs {
		if r.Tree == subj.Tree && r.Repo == subj.Repo && matches(r) && r.Subdir == "" && r.Result == "" && r.Exit != 0 {
			n++
		}
	}
	return n
}
