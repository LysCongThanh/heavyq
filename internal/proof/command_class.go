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
}

// replayed reports whether r only replays the result of another run (a cache
// hit, or a run that waited for an identical in-flight one). The run it copies
// has its own line, with its own machine reading.
func replayed(r history.Record) bool { return r.Cache == "hit" || r.Cache == "coalesced" }

// tallyRuns counts runs (oldest first). A replay is counted only when no
// real run of the same outcome exists, so repeating `gate run` on a tree does
// not inflate the numbers, while a result that only ever arrived by replay
// (another clone ran it) still counts once.
func tallyRuns(runs []history.Record) tally {
	var t tally
	seen := map[bool]bool{} // by "passed"
	count := func(r history.Record) {
		seen[r.Exit == 0] = true
		if r.Exit == 0 {
			t.pass++
			return
		}
		t.fail++
		t.lastFail = r
		if !r.Loaded() {
			t.unexplained++
			t.lastUnexplained = r
		}
	}
	for _, r := range runs {
		if !replayed(r) {
			count(r)
		}
	}
	for _, r := range runs {
		if replayed(r) && !seen[r.Exit == 0] {
			count(r)
		}
	}
	return t
}

// decide turns the conclusive runs of a command on the tree (oldest first, at
// least one) into its status. out carries the command and is filled in.
func decide(out Command, runs []history.Record) Command {
	t := tallyRuns(runs)
	out.Passes, out.Fails = t.pass, t.fail
	deciding := runs[len(runs)-1]
	switch {
	case t.pass > 0 && t.unexplained > 0:
		out.State, out.Class = Failed, ClassFlaky
		deciding = t.lastUnexplained
		out.Reason = fmt.Sprintf("flaky: %d pass / %d fail on this tree", t.pass, t.fail)
	case deciding.Exit == 0:
		out.State = Proven
	case t.lastFail.Loaded():
		out.State, out.Class = Failed, ClassUnderLoad
		out.Reason = "under load: " + t.lastFail.LoadWhy()
	default:
		out.State, out.Class = Failed, ClassFailed
	}
	out.At, out.Lane, out.RunSecs, out.Cache, out.Machine = deciding.End, deciding.Lane, deciding.RunSecs, deciding.Cache, deciding.Machine
	exit := deciding.Exit
	out.Exit = &exit
	return out
}
