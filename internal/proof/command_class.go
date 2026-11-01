package proof

import (
	"fmt"

	"github.com/LysCongThanh/heavyq/internal/history"
)

// tally counts the runs of one command on one tree.
type tally struct {
	pass, fail                int // runs that exited 0 / did not
	unexplained               int // failures the machine does not explain (not Loaded)
	lastFail, lastUnexplained history.Record
	// confirmed: the last two runs that load does not explain both passed.
	confirmed bool
}

// replayed reports whether r only replays the result of another run (a cache
// hit, or a run that waited for an identical in-flight one). The run it copies
// has its own line, with its own machine reading.
func replayed(r history.Record) bool { return r.Cache == "hit" || r.Cache == "coalesced" }

// tallyRuns counts runs (oldest first). A replay is counted only when no
// real run of the same outcome exists, so repeating `gate run` on a tree does
// not inflate the numbers or fake a repetition, while a result that only ever
// arrived by replay (another clone ran it) still counts once.
func tallyRuns(runs []history.Record) tally {
	counted := make([]bool, len(runs))
	seen := map[bool]bool{} // by "passed"
	for i, r := range runs {
		if !replayed(r) {
			counted[i], seen[r.Exit == 0] = true, true
		}
	}
	for i, r := range runs {
		if replayed(r) && !seen[r.Exit == 0] {
			counted[i], seen[r.Exit == 0] = true, true
		}
	}
	var t tally
	var recent []history.Record // counted runs without the failures load explains
	for i, r := range runs {
		if !counted[i] {
			continue
		}
		if r.Exit == 0 {
			t.pass++
		} else {
			t.fail++
			t.lastFail = r
			if r.Loaded() {
				continue
			}
			t.unexplained++
			t.lastUnexplained = r
		}
		recent = append(recent, r)
	}
	n := len(recent)
	t.confirmed = n >= 2 && recent[n-1].Exit == 0 && recent[n-2].Exit == 0
	return t
}

// decide turns the conclusive runs of a command on the tree (oldest first, at
// least one) into its status. out carries the command and is filled in.
//
// Flaky is a label, not a verdict: a command that failed unexplained and
// passed on this tree is proven again by repetition, once its last two runs
// passed. Until then it is not proven.
func decide(out Command, runs []history.Record) Command {
	t := tallyRuns(runs)
	out.Passes, out.Fails = t.pass, t.fail
	deciding := runs[len(runs)-1]
	flaky := fmt.Sprintf("flaky: %d fail / %d pass on this tree", t.fail, t.pass)
	switch {
	case deciding.Exit != 0 && t.lastFail.Loaded(): // the latest failure is the latest counted one
		out.State, out.Class = Failed, ClassUnderLoad
		out.Reason = "under load: " + t.lastFail.LoadWhy()
	case deciding.Exit != 0 && t.pass > 0:
		out.State, out.Class, out.Reason = Failed, ClassFlaky, flaky
	case deciding.Exit != 0:
		out.State, out.Class = Failed, ClassFailed
	case t.unexplained == 0:
		out.State = Proven
	case t.confirmed:
		out.State, out.Class, out.Reason = Proven, ClassFlaky, flaky
	default: // passed after a failure, once: not yet confirmed
		out.State, out.Class, out.Reason = Failed, ClassFlaky, flaky
		deciding = t.lastUnexplained
	}
	out.At, out.Lane, out.RunSecs, out.Cache, out.Machine = deciding.End, deciding.Lane, deciding.RunSecs, deciding.Cache, deciding.Machine
	exit := deciding.Exit
	out.Exit = &exit
	return out
}
