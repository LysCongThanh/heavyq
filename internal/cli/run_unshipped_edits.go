package cli

import (
	"fmt"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/gitutil"
)

// A remote run executes the committed tree: edits in the work tree stay behind.
// An operator who runs a test file that exists only as an uncommitted edit gets
// a pass on the other files and no hint that the file never ran, so a run
// admitted to a remote says how many files it leaves behind. It is said once, by
// announceAdmitted, and costs one `git status` only on the way to a remote.

// unshippedEdits is how many uncommitted files the run leaves behind on
// machine: 0 for a local run (it sees them), for a run that asked for another
// ref than HEAD (it chose not to ship the work tree), for one placed by default
// (placement and the re-check at admission both need a clean tree), and when
// git cannot say.
func (r *run) unshippedEdits(machine string) int {
	if machine == config.LocalName || !r.pl.refIsHead || r.pl.defaultMode || r.pl.repo == "" {
		return 0
	}
	n, err := gitutil.UncommittedFiles(r.pl.repo)
	if err != nil {
		r.verbosef("uncommitted files not counted: %v", err)
		return 0
	}
	return n
}

// uncommittedFiles words n files: "1 uncommitted file", "3 uncommitted files".
func uncommittedFiles(n int) string {
	if n == 1 {
		return "1 uncommitted file"
	}
	return fmt.Sprintf("%d uncommitted files", n)
}

// unshippedWarning is the line a person reads: a warning, what to do about it.
func unshippedWarning(n int, machine string) string {
	verb := "are"
	if n == 1 {
		verb = "is"
	}
	return fmt.Sprintf("heavyq: warning: %s %s not shipped: %s runs the committed tree only (commit them, or use --remote local)\n",
		uncommittedFiles(n), verb, machine)
}
