package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/LysCongThanh/heavyq/internal/agentmode"
	"github.com/LysCongThanh/heavyq/internal/cache"
	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/gitutil"
)

// nextRetryLater is the instruction after a max-wait: the one thing an agent
// must not do is run the command some other way.
const nextRetryLater = "retry later; don't bypass heavyq"

// announceAdmitted says that the ticket was admitted. For a person: where the
// run was placed and the turn it got. For an agent: the one line that opens
// the run, with the command, lane and tree, so it sits at the head of the
// output (agents that truncate a failing command's output keep head and tail).
// A run that goes to a remote with uncommitted edits says they stay behind (see
// run_unshipped_edits.go): a line of its own for a person, a part of the running
// line for an agent, whose three-line budget has no room for another.
func (r *run) announceAdmitted(c *claim, waited time.Duration, co gitutil.Checkout) {
	left := r.unshippedEdits(c.machine)
	if !r.out.mode.Terse() {
		if r.pl.defaultMode && c.machine != config.LocalName {
			fmt.Fprintf(r.stderr, "heavyq: placed on %s by score (clean repo, commit %.12s; force local with --remote local)\n", c.machine, r.pl.sha)
		}
		fmt.Fprintf(r.stderr, "heavyq: our turn after %s (prio %d, lane %s, %s/%s): %s\n",
			fmtDur(waited), r.me.Prio, r.me.Lane, c.machine, c.slot, r.me.Cmd)
		if left > 0 {
			fmt.Fprint(r.stderr, unshippedWarning(left, c.machine))
		}
		return
	}
	extra := ""
	if t := shortTreeID(co.Tree, co.Dirty); t != "" {
		extra += ", tree " + t
	}
	if r.out.nar.queued {
		extra += ", waited " + fmtDur(waited)
	}
	if left > 0 {
		extra += ", " + uncommittedFiles(left) + " not shipped"
	}
	fmt.Fprintf(r.stderr, "heavyq: running %s (p%d, lane %s, %s/%s%s)\n",
		r.me.Cmd, r.me.Prio, r.me.Lane, c.machine, c.slot, extra)
}

// shortTreeID is the tree id as a digest shows it: 12 characters, and for a
// work tree with uncommitted edits "+" and 8 characters of the edits' digest.
func shortTreeID(tree string, dirty bool) string {
	if tree == "" {
		return ""
	}
	head, edits, edited := strings.Cut(tree, "+")
	s := head[:min(12, len(head))]
	if edited {
		s += "+" + edits[:min(8, len(edits))]
	} else if dirty {
		s += "+edits"
	}
	return s
}

// digestRan ends a run whose command was started: PASS, FAIL, or one of the
// ways it was stopped.
func (r *run) digestRan(rc int, machine string, co gitutil.Checkout, ran, waited time.Duration) {
	if !r.out.digest {
		return
	}
	res := agentmode.Result{Verdict: agentmode.Pass, Cmd: r.me.Cmd, Exit: rc, Dur: ran, Waited: waited,
		Tree: shortTreeID(co.Tree, co.Dirty)}
	switch {
	case r.cancelled:
		res.Verdict, res.Why, res.Next = agentmode.Cancelled, "an operator cancelled it while it ran", nextCancelled
	case r.interrupted:
		res.Verdict, res.Why = agentmode.Interrupted, fmt.Sprintf("stopped by signal %d", rc-128)
		res.Next = "run it again if you still need the result"
	case rc == ExitRemoteSetup:
		res.Verdict, res.Why = agentmode.NotRun, "remote setup on "+machine+" failed; the command did not run"
		res.Next = nextRemoteDown
	case r.suspect != "":
		res.Why, res.Next = r.suspectWhy(rc != 0), nextSuspect
		if rc != 0 { // strict: it is a proof, so a failure
			res.Verdict = agentmode.Fail
		} else {
			res.Verdict = agentmode.Suspect
		}
	case rc != 0:
		res.Verdict, res.Next = agentmode.Fail, r.failNext(rc, machine)
	}
	r.printDigest(res)
}

const (
	nextCancelled  = "cancelled by operator; ask before re-running"
	nextSuspect    = "install the missing tool where it ran (or make the pipe fail: bash -o pipefail -c), then re-run"
	nextRemoteDown = "the remote is unreachable; retry later, or add --remote local; don't bypass heavyq"
)

// failNext is the one thing to do after a command failed.
func (r *run) failNext(rc int, machine string) string {
	switch {
	case rc == 127 && machine != config.LocalName:
		return "command not found on " + machine + "; install it there or add its directory to that remote's env_path (heavyq doctor lists its tools), then re-run"
	case rc == 127:
		return "command not found; check its name and PATH, then re-run"
	case rc == 126:
		return "command is not executable; check its permissions, then re-run"
	case rc == ExitConnLost && machine != config.LocalName:
		return "the connection to " + machine + " was lost; retry, or add --remote local"
	}
	if machine != config.LocalName {
		if next := r.baseRefNext(); next != "" {
			return next
		}
	}
	if r.opts.Gate != "" {
		return "fix the failure above, then run: heavyq gate run " + r.opts.Gate
	}
	return "fix the failure above, then re-run this command"
}

// digestNotRun ends a ticket that left the queue (or the wait for an identical
// run) without running its command.
func (r *run) digestNotRun(rc int) {
	if !r.out.digest {
		return
	}
	res := agentmode.Result{Verdict: agentmode.NotRun, Cmd: r.cmdText(), Exit: rc, Waited: time.Since(r.t0),
		Why: r.out.gaveUp}
	switch {
	case r.cancelled:
		res.Verdict, res.Next = agentmode.Cancelled, nextCancelled
		res.Why = orDefault(res.Why, "an operator cancelled it while it waited")
	case rc > 128 && rc != ExitConnLost:
		res.Verdict, res.Why = agentmode.Interrupted, fmt.Sprintf("stopped by signal %d while it waited", rc-128)
		res.Next = "run it again if you still need the result"
	case rc == ExitMaxWait:
		res.Next = nextRetryLater
	case rc == ExitRemoteDown:
		res.Next = nextRemoteDown
	default: // exit 2: the code changed while it waited, or the queue itself failed (see Why)
		res.Next = "run it again; if that repeats, `heavyq why` explains the queue"
	}
	r.printDigest(res)
}

// digestReused ends a run that was answered from a stored or coalesced result.
func (r *run) digestReused(m *cache.Meta) {
	if !r.out.digest {
		return
	}
	when := m.Created.Local().Format("2006-01-02 15:04")
	res := agentmode.Result{Verdict: agentmode.Reused, Cmd: r.cmdText(), Exit: m.Exit, Dur: time.Since(r.t0),
		Tree: shortTreeID(r.cache.tree, false)}
	if m.Exit == 0 {
		res.Detail = fmt.Sprintf("already passed on this exact tree at %s (lane %s); do not re-run", when, m.Lane)
	} else {
		res.Detail = fmt.Sprintf("already failed (exit %d) on this exact tree at %s (lane %s); re-running cannot change that",
			m.Exit, when, m.Lane)
		res.Next = "change the code, then re-run this command"
	}
	r.printDigest(res)
}

// printDigest completes res from what the command printed (failure lines, the
// saved log) and writes it to stderr, as text or, under HEAVYQ_OUTPUT=json,
// as one JSON line. A log is kept only for a result that is not a success.
func (r *run) printDigest(res agentmode.Result) {
	failed := res.Verdict == agentmode.Fail || (res.Verdict == agentmode.Reused && res.Exit != 0)
	if failed && r.out.capture != nil {
		tail, cut := r.out.capture.Tail()
		res.Failures, res.More = agentmode.FailureLines(tail, cut, agentmode.MaxFailureLines)
	}
	if r.out.capture != nil {
		keep := res.Verdict != agentmode.Pass && (res.Verdict != agentmode.Reused || res.Exit != 0)
		if l, ok := r.out.capture.Finish(keep); ok {
			res.Log = &l
		}
	}
	if r.out.mode == agentmode.JSON {
		fmt.Fprint(r.stderr, res.JSON())
		return
	}
	fmt.Fprint(r.stderr, res.Text())
}
