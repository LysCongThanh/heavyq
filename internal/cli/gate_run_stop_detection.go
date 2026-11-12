package cli

import (
	"io"
	"os"
	"slices"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/history"
)

// A gate run passes a stop from outside through (128+n for a signal, 78 for
// an operator cancel) instead of calling it a failure. The exit code alone
// cannot say which it was: a command killed by the OOM killer exits 137 and one
// that segfaults 139, and 78 is also sysexits' EX_CONFIG. So the stop is what
// heavyq itself saw: a signal it received, or a cancel of its ticket.

// runCommandStopped is runCommand plus whether heavyq was stopped from outside
// (a signal it received, an operator cancel), as opposed to the command ending
// with that code by itself.
func runCommandStopped(cfg *config.Config, args []string, stderr io.Writer) (rc int, stopped bool) {
	var r *run
	rc = runCommandWith(cfg, args, stderr, &r)
	return rc, r != nil && (r.interrupted || r.cancelled)
}

// childStopped reports whether a child heavyq of `gate run --parallel` that
// exited rc was stopped from outside, by its ledger line (the child records a
// signal as "interrupted" and a cancel as "cancelled"): a child can be
// signalled or cancelled on its own, without the gate seeing anything.
func childStopped(gate string, runArgs []string, rc int, since time.Time) bool {
	if rc != ExitCancelled && (rc <= 128 || rc >= 160) {
		return false
	}
	sep := slices.Index(runArgs, "--")
	if sep < 0 {
		return false
	}
	hash := history.CmdHash(runArgs[sep+1:])
	recs, _ := history.ReadLast(history.DefaultPath(), 256)
	for i := len(recs) - 1; i >= 0; i-- {
		if r := recs[i]; r.Gate == gate && r.CmdHash == hash && !r.End.Before(since) {
			return r.Result == history.OutcomeCancelled || r.Result == history.OutcomeInterrupted
		}
	}
	return false
}

// signalled is the signal the gate itself received while its children ran
// (nil: none).
func (s *childSet) signalled() os.Signal {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pending
}

// gateStopCode reports whether the exit code of gateRunCommand is a stop it
// passed through: gate run returns 128+n and 78 only for those (a failing
// command makes it 1).
func gateStopCode(rc int) bool { return rc == ExitCancelled || (rc > 128 && rc < 160) }
