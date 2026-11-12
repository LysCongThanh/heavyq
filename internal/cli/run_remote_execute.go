package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/redact"
	"github.com/LysCongThanh/heavyq/internal/remote"
)

// runRemote ships the commit (and its base and siblings) to the claimed
// remote and runs the command there. While it ships, a signal or an operator
// cancel stops the push at once (setupWatch), so the slot is never held by a
// push nobody can stop.
func (r *run) runRemote(c *claim) int {
	m, _ := r.cfg.Machine(c.machine)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watch := r.watchSetup(cancel)
	baseRef, err := r.ship(ctx, m)
	if rc, stopped := r.setupStopped(watch); stopped {
		return rc
	}
	if err != nil {
		r.setupError(err)
		return r.setupFailed()
	}
	if rc := r.checkExitTrust(m, filepath.Base(r.pl.repo), c.slot, r.cfg.Queue.ProbeTimeout.Duration*6); rc != 0 {
		return rc
	}
	select { // a signal during the exit canary: do not start the command at all
	case s := <-r.sigs:
		r.interrupted = true
		return signalExit(s)
	default:
	}
	if controlBox().CancelRequested(r.me.Name) { // the exit canary takes time: a cancel may have come meanwhile
		r.note("cancelled by an operator before the job started")
		r.cancelled = true
		return ExitCancelled
	}
	jobOut, jobErr := r.cmdStreams()
	set, unset := identityEnv(r.cfg.Cache.Env)
	job, err := remote.StartJob(remote.JobSpec{Machine: m, Repo: filepath.Base(r.pl.repo), SHA: r.pl.sha, Lane: r.me.Lane,
		Slot: c.slot, Argv: r.opts.Argv, Subdir: r.pl.subdir, Siblings: siblingIDs(r.pl.siblings), BaseRef: baseRef,
		BaseSHA: r.pl.baseSHA, Env: set, Unset: unset, Quiet: r.out.mode.Terse(), Stdout: jobOut, Stderr: jobErr})
	if err != nil {
		r.setupError(err)
		return r.setupFailed()
	}
	rc := r.supervise(job.Signal, job.Wait)
	switch rc { // an agent reads these in the digest
	case remote.ExitSlotBusy, remote.ExitSetupFailed:
		r.note("remote setup on %s failed (job code %d); the command did not run", c.machine, rc)
		return r.setupFailed()
	case remote.ExitConnectionLost:
		r.note("connection to %s lost; the remote side stops the command", c.machine)
	case 127:
		r.remoteNotFound(c.machine)
	}
	return rc
}

// ship pushes the commit, the base commit (a failure there only drops the
// base ref) and each sibling to m. It returns the base ref the job creates,
// and fixes r.base to the commit the job gets ("" when it gets none).
func (r *run) ship(ctx context.Context, m config.MachineConfig) (string, error) {
	name := filepath.Base(r.pl.repo)
	timeout := r.cfg.Queue.ProbeTimeout.Duration * 6
	r.base, r.baseFixed = "", true // the job gets no base unless it is pushed: then the run has none
	if err := remote.PushContext(ctx, m, r.pl.repo, name, r.pl.sha, r.pl.sha[:12], timeout); err != nil {
		return "", err
	}
	baseRef := ""
	if r.opts.BaseRef != "" && r.pl.baseSHA != "" {
		if err := remote.PushContext(ctx, m, r.pl.repo, name, r.pl.baseSHA, "base-"+r.pl.baseSHA[:12], timeout); err != nil {
			if ctx.Err() != nil {
				return "", err
			}
			fmt.Fprintln(r.stderr, "heavyq: warning: base ref not pushed:", err)
		} else {
			baseRef, r.base = r.opts.BaseRef, r.pl.baseSHA
		}
	}
	for _, s := range r.pl.siblings {
		if err := remote.PushContext(ctx, m, s.Path, s.Name, s.SHA, s.SHA[:12], timeout); err != nil {
			return "", err
		}
	}
	return baseRef, nil
}

// setupFailed marks the run as one whose command never started because
// heavyq could not prepare the remote, and returns ExitRemoteSetup. Only this
// makes a 77 a setup failure: a command's own 77 is its own result.
func (r *run) setupFailed() int {
	r.remoteSetupFailed = true
	return ExitRemoteSetup
}

// setupError says why a remote run could not be set up: git's whole message
// for a person, one bounded line for an agent (the digest says the rest).
func (r *run) setupError(err error) {
	msg := err.Error()
	if r.out.mode.Terse() {
		if msg = redact.OneLine(msg); len(msg) > 300 {
			msg = strings.ToValidUTF8(msg[:297], "") + "..."
		}
	}
	fmt.Fprintln(r.stderr, "heavyq:", msg)
}
