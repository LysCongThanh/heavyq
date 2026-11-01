package cli

import (
	"fmt"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/proof"
)

// gateCounts tallies the commands of a gate that are not proven.
type gateCounts struct {
	failed, flaky, underLoad, missing int
}

func countGate(g proof.Gate) gateCounts {
	var n gateCounts
	for _, c := range g.Commands {
		switch {
		case c.State == proof.Failed:
			n.failed++
			switch c.Class {
			case proof.ClassFlaky:
				n.flaky++
			case proof.ClassUnderLoad:
				n.underLoad++
			}
		case c.State != proof.Proven:
			n.missing++
		}
	}
	return n
}

// String is "1 failed (1 flaky, 1 under load), 2 missing": the classes are
// named only when there are some.
func (n gateCounts) String() string {
	var classes []string
	if n.flaky > 0 {
		classes = append(classes, fmt.Sprintf("%d flaky", n.flaky))
	}
	if n.underLoad > 0 {
		classes = append(classes, fmt.Sprintf("%d under load", n.underLoad))
	}
	failed := fmt.Sprintf("%d failed", n.failed)
	if len(classes) > 0 {
		failed += " (" + strings.Join(classes, ", ") + ")"
	}
	return fmt.Sprintf("%s, %d missing", failed, n.missing)
}

// hints are the lines `gate status` adds under the summary for the failures
// that call for something other than debugging right away.
func (n gateCounts) hints(gate string) []string {
	var out []string
	if n.underLoad > 0 {
		out = append(out, "under load: the machine was over its limits when the run started; rerun once before debugging: heavyq gate run "+gate)
	}
	if n.flaky > 0 {
		out = append(out, "flaky \u2014 re-run once to confirm: heavyq gate run "+gate)
	}
	return out
}
