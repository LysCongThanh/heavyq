package cli

import (
	"fmt"

	"github.com/LysCongThanh/heavyq/internal/redact"
)

// judgeSuspect decides what to make of a run that exited 0 whose output has a
// shell not-found line (r.scan): the exit code is untrustworthy, typically a
// pipeline whose last stage succeeded. It sets r.suspect, so the run is not
// stored in the cache, recorded as a suspect line (never as proof) and worded
// PASS?. Under --strict, and for gates and item proofs (which are proofs), the
// run fails with 127 instead. It returns the exit code to use.
func (r *run) judgeSuspect(rc int) int {
	if rc != 0 || r.interrupted || r.cancelled {
		return rc
	}
	line := r.scan.Line()
	if line == "" {
		return rc
	}
	r.suspect = redact.Command(line)
	strict := r.opts.Strict || r.opts.Gate != ""
	if strict {
		rc = 127
	}
	if !r.out.digest { // a digest says it too
		fmt.Fprintf(r.stderr, "heavyq: %s %s\n", suspectHead(strict), r.suspectWhy(strict))
	}
	return rc
}

func suspectHead(strict bool) string {
	if strict {
		return "FAIL exit 127 (strict):"
	}
	return "PASS?"
}

// suspectWhy is the one-line warning quoting the offending output line.
func (r *run) suspectWhy(strict bool) string {
	s := fmt.Sprintf("the command exited 0, but its output has %q: a command was not found, so this proves nothing; not recorded as proof, not cached", r.suspect)
	if !strict {
		s += " (--strict makes it fail with 127)"
	}
	return s
}
