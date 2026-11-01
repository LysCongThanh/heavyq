package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/LysCongThanh/heavyq/internal/agentmode"
	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/queue"
)

// outputMode resolves how heavyq words its output for this process
// ($HEAVYQ_OUTPUT, $HEAVYQ_AGENT, then agent detection; see agentmode.Resolve).
// An ignored bad value is said once on stderr.
func outputMode(stderr io.Writer) agentmode.Mode {
	m, warn := agentmode.Resolve(os.LookupEnv)
	if warn != "" {
		fmt.Fprintln(stderr, "heavyq: warning:", warn)
	}
	return m
}

// runOutput is how one run words its own output, and what it keeps of the
// command's. Everything about agent mode that a run needs hangs off it.
type runOutput struct {
	mode agentmode.Mode
	// digest: a result digest ends the run (terse modes, or --digest).
	digest bool
	// capture sees the command's output when digest is on; nil otherwise, and
	// then the command gets the real streams exactly as before.
	capture *agentmode.Capture
	// stdout and stderr are what the command (or a replayed result) writes to.
	stdout, stderr io.Writer
	nar            waitNarrator
	// gaveUp is why the ticket left the queue without running, for the digest.
	gaveUp string
}

// newRunOutput sets up the output of a run that started at t0 in lane.
func newRunOutput(mode agentmode.Mode, wantDigest bool, errw io.Writer, t0 time.Time, lane string) runOutput {
	o := runOutput{mode: mode, digest: mode.Terse() || wantDigest, stdout: os.Stdout, stderr: os.Stderr,
		nar: waitNarrator{w: errw, terse: mode.Terse(), t0: t0}}
	if o.digest {
		o.capture = agentmode.NewCapture(logDir(), agentmode.LogName(t0, os.Getpid(), lane))
		o.stdout, o.stderr = o.capture.Wrap(os.Stdout), o.capture.Wrap(os.Stderr)
	}
	return o
}

// logDir is where the full output of failed runs is kept: next to the ledger
// ($XDG_STATE_HOME/heavyq/logs, or beside $HEAVYQ_HISTORY). "" when there is
// no state directory, and then no log is written.
func logDir() string {
	p := history.DefaultPath()
	if p == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(p), "logs")
}

// note says something to a person. In terse modes it is dropped: the queued
// line, the running line and the digest already carry it.
func (r *run) note(format string, a ...any) {
	if !r.out.mode.Terse() {
		fmt.Fprintf(r.stderr, "heavyq: "+format+"\n", a...)
	}
}

// giveUp records why the ticket leaves the queue without running. The
// message is printed as usual for a person; the digest repeats it for an agent.
func (r *run) giveUp(format string, a ...any) {
	r.out.gaveUp = fmt.Sprintf(format, a...)
	r.note("%s", r.out.gaveUp)
}

// discardLog removes a log that no digest claimed (deferred by runCommand, so
// no path through a run leaves a file behind).
func (o *runOutput) discardLog() {
	if o.capture != nil {
		o.capture.Finish(false)
	}
}

// usageFail reports a bad invocation. For an agent it also says what to do.
func usageFail(stderr io.Writer, mode agentmode.Mode, err error) int {
	fmt.Fprintln(stderr, "heavyq:", err)
	if mode.Terse() {
		fmt.Fprintln(stderr, "next: fix the arguments; `heavyq help agents` shows the usage")
	}
	return ExitUsage
}

// cmdText is the command as shown to people: redacted and bounded.
func (r *run) cmdText() string { return queue.TruncateCmd(r.opts.Argv) }
