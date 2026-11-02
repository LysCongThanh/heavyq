package stance

import (
	"fmt"

	"github.com/LysCongThanh/heavyq/internal/history"
)

// FailureClass is the most widespread failure of a lane: the same command
// failing the same way on Trees different trees, with no pass of that command
// in between. It is the evidence behind enter.same_failure_class_across_trees:
// the code changed and the failure did not, so the approach is not working.
type FailureClass struct {
	Label string // "<command> exit <code>", for the "why now" line; "" when there is no failure
	Trees int
}

// SameFailureClassAcrossTrees reads the records of one lane, oldest first
// (ledgerindex.LaneRuns), and returns its widest failure class.
//
// A class is (repository, command, exit code), the command being identified by
// cmd_hash, else the recorded text. Only runs that went to their end on a known
// tree count: no give-ups or cancels, no cache replays (they repeat another
// run's result, not new evidence) and no failures the machine explains (a run
// admitted over its limits is under_load, not a failing approach). A pass of
// the command forgets its classes: the approach worked after all. Ties go to
// the label that sorts first, so the answer is deterministic.
func SameFailureClassAcrossTrees(recs []history.Record) FailureClass {
	type class struct {
		label string
		trees map[string]bool
	}
	byCmd := map[string]map[int]*class{} // command -> exit code -> class
	for _, r := range recs {
		if r.Result != "" || r.Tree == "" || r.Cache == "hit" || r.Cache == "coalesced" {
			continue
		}
		cmd := r.CmdHash
		if cmd == "" {
			cmd = r.Cmd
		}
		key := r.Repo + "\x00" + cmd
		if r.Exit == 0 {
			delete(byCmd, key)
			continue
		}
		if r.Loaded() {
			continue
		}
		if byCmd[key] == nil {
			byCmd[key] = map[int]*class{}
		}
		c := byCmd[key][r.Exit]
		if c == nil {
			c = &class{label: fmt.Sprintf("%s exit %d", clipOneLine(r.Cmd, 60), r.Exit), trees: map[string]bool{}}
			byCmd[key][r.Exit] = c
		}
		c.trees[r.Tree] = true
	}
	var best FailureClass
	for _, codes := range byCmd {
		for _, c := range codes {
			if n := len(c.trees); n > best.Trees || (n == best.Trees && c.label < best.Label) {
				best = FailureClass{Label: c.label, Trees: n}
			}
		}
	}
	return best
}

// clipOneLine flattens s to one line and cuts it to at most n bytes.
func clipOneLine(s string, n int) string { return cutRunes(oneLine(s), n) }
