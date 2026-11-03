package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/ledgerindex"
	"github.com/LysCongThanh/heavyq/internal/remote"
)

// execute runs the command on the claimed machine and records history.
func (r *run) execute(c *claim) int {
	waited := time.Since(r.t0)
	co := r.checkoutFor(c.machine) // the code the command is about to see
	r.announceAdmitted(c, waited, co)
	warnShellPipe(r.stderr, r.opts.Argv)
	runStart := time.Now()
	var rc int
	if c.machine == config.LocalName {
		rc = r.runLocal(c)
		co = r.settled(co)
	} else {
		rc = r.runRemote(c)
	}
	runSecs := time.Since(runStart).Seconds()
	rc = r.judgeSuspect(rc)
	ran := !r.interrupted && !r.cancelled && rc != 126 && rc != 127 &&
		(c.machine == config.LocalName || (rc != ExitRemoteSetup && rc != ExitConnLost)) && r.suspect == ""
	r.cacheFinish(c.machine, rc, ran, runSecs)
	rec := history.Record{End: time.Now(), Machine: c.machine, Lane: r.me.Lane, Prio: r.me.Prio, Light: r.me.Light,
		WaitSecs: waited.Seconds(), RunSecs: runSecs, Exit: rc, Cmd: r.me.Cmd, Forced: c.forced,
		CPUSecs: r.cpuSecs, MaxRSSMB: r.maxRSSMB}
	r.tag(&rec, co)
	c.stampAdmission(&rec)
	switch {
	case r.cancelled:
		rec.Result = history.OutcomeCancelled
	case r.interrupted:
		rec.Result = history.OutcomeInterrupted
	case r.suspect != "":
		rec.Result = history.OutcomeSuspect // exit 0 (127 under --strict), but the shell lost a command: no proof
	case rc == ExitRemoteSetup || rc == 126 || rc == 127:
		rec.Result = history.OutcomeGaveUp // the command never started
	}
	if r.cache != nil {
		rec.Cache, rec.CacheNote = r.cache.status, r.cache.note
	}
	if err := ledgerindex.Append(history.DefaultPath(), rec); err != nil {
		fmt.Fprintln(r.stderr, "heavyq: warning: history not written:", err)
	}
	r.digestRan(rc, c.machine, co, time.Since(runStart), waited)
	return rc
}

// localDir is where a local command runs: the caller's cwd, unless --repo
// points elsewhere and the cwd is outside it.
func (r *run) localDir() string {
	if r.opts.Repo == "" {
		return r.pl.wd
	}
	repo, err := filepath.Abs(r.opts.Repo)
	if err != nil {
		return r.pl.wd
	}
	if rel, err := filepath.Rel(repo, r.pl.wd); err == nil && !strings.HasPrefix(rel, "..") {
		return r.pl.wd
	}
	return repo
}

func (r *run) runLocal(c *claim) int {
	cmd := exec.Command(r.opts.Argv[0], r.opts.Argv[1:]...)
	cmd.Dir = r.localDir()
	cmd.Stdin = os.Stdin
	cmd.Stdout, cmd.Stderr = r.cmdStreams() // the real streams, or tees of them for the digest
	// With --cache the streams are pipes: don't wait forever on a daemon that
	// inherited them after the command itself exited.
	cmd.WaitDelay = 10 * time.Second
	subreaper := becomeSubreaper()
	cmd.Env = append(os.Environ(), "HEAVYQ_MACHINE=local", "HEAVYQ_SLOT="+c.slot, "HEAVYQ_LANE="+r.me.Lane, "HEAVYQ_ACTIVE=1")
	cmd.Env = append(cmd.Env, c.lease.Env()...) // the pool values this run holds
	// Slot and mutex descriptors are close-on-exec: the command (and any
	// daemon it leaves behind) never holds a slot. The slot is released when
	// this process exits.
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(r.stderr, "heavyq:", err)
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
			return 127
		}
		return 126
	}
	r.tree = newProcessTree(cmd.Process.Pid, subreaper)
	return r.supervise(func(s os.Signal) { _ = cmd.Process.Signal(s) }, func() int {
		rc := processExit(cmd.Wait())
		r.cpuSecs, r.maxRSSMB = childUsage(cmd.ProcessState)
		return rc
	})
}

func (r *run) runRemote(c *claim) int {
	m, _ := r.cfg.Machine(c.machine)
	name := filepath.Base(r.pl.repo)
	timeout := r.cfg.Queue.ProbeTimeout.Duration * 6
	if err := remote.Push(m, r.pl.repo, name, r.pl.sha, r.pl.sha[:12], timeout); err != nil {
		fmt.Fprintln(r.stderr, "heavyq:", err)
		return ExitRemoteSetup
	}
	baseRef := ""
	if r.opts.BaseRef != "" && r.pl.baseSHA != "" {
		if err := remote.Push(m, r.pl.repo, name, r.pl.baseSHA, "base-"+r.pl.baseSHA[:12], timeout); err != nil {
			fmt.Fprintln(r.stderr, "heavyq: warning: base ref not pushed:", err)
		} else {
			baseRef = r.opts.BaseRef
		}
	}
	for _, s := range r.pl.siblings {
		if err := remote.Push(m, s.Path, s.Name, s.SHA, s.SHA[:12], timeout); err != nil {
			fmt.Fprintln(r.stderr, "heavyq:", err)
			return ExitRemoteSetup
		}
	}
	sibs := siblingIDs(r.pl.siblings)
	select {
	case s := <-r.sigs:
		return signalExit(s)
	default:
	}
	jobOut, jobErr := r.cmdStreams()
	job, err := remote.StartJob(remote.JobSpec{Machine: m, Repo: name, SHA: r.pl.sha, Lane: r.me.Lane, Slot: c.slot,
		Argv: r.opts.Argv, Subdir: r.pl.subdir, Siblings: sibs, BaseRef: baseRef, BaseSHA: r.pl.baseSHA,
		Stdout: jobOut, Stderr: jobErr})
	if err != nil {
		fmt.Fprintln(r.stderr, "heavyq:", err)
		return ExitRemoteSetup
	}
	rc := r.supervise(job.Signal, job.Wait)
	switch rc {
	case remote.ExitSlotBusy, remote.ExitSetupFailed:
		fmt.Fprintf(r.stderr, "heavyq: remote setup on %s failed (job code %d); the command did not run\n", c.machine, rc)
		return ExitRemoteSetup
	case remote.ExitConnectionLost:
		fmt.Fprintf(r.stderr, "heavyq: connection to %s lost; the remote side stops the command\n", c.machine)
	}
	return rc
}

// supervise waits for the child while forwarding termination signals: the
// first signal becomes SIGTERM, any further one SIGKILL. After a signal the
// exit code is 128+signal regardless of the child's code. An operator cancel
// request is handled like a first signal (SIGTERM, then SIGKILL after
// cancelGrace) and exits ExitCancelled.
func (r *run) supervise(forward func(os.Signal), wait func() int) int {
	done := make(chan int, 1)
	go func() { done <- wait() }()
	sigCode := 0
	tick := time.NewTicker(controlPoll)
	defer tick.Stop()
	var killAt <-chan time.Time
	for {
		select {
		case rc := <-done:
			if r.cancelled {
				r.reapCancelled(killAt)
			}
			if sigCode != 0 {
				return sigCode
			}
			return rc
		case <-tick.C:
			if sigCode == 0 && controlBox().CancelRequested(r.me.Name) {
				r.note("cancelled by an operator; stopping the command")
				sigCode, r.cancelled = ExitCancelled, true
				r.tree.signal(syscall.SIGTERM)
				forward(syscall.SIGTERM)
				killAt = time.After(cancelGrace)
			}
		case <-killAt:
			r.tree.signal(syscall.SIGKILL)
			forward(syscall.SIGKILL)
		case s := <-r.sigs:
			if sigCode == 0 {
				sigCode = signalExit(s)
				r.interrupted = true
				forward(syscall.SIGTERM)
			} else {
				forward(syscall.SIGKILL)
			}
		}
	}
}

// reapCancelled keeps the slot until everything the cancelled command
// started is gone: survivors get the rest of the grace period, then SIGKILL.
func (r *run) reapCancelled(killAt <-chan time.Time) {
	for r.tree.alive() {
		select {
		case <-killAt:
			r.tree.signal(syscall.SIGKILL)
			time.Sleep(100 * time.Millisecond)
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
}

const (
	controlPoll = time.Second      // how often a running ticket checks for a cancel request
	cancelGrace = 10 * time.Second // SIGTERM -> SIGKILL delay for a cancelled command
)

// childUsage returns the CPU seconds (user+system) and peak resident set of
// an exited command and the children it waited for.
func childUsage(ps *os.ProcessState) (float64, int) {
	if ps == nil {
		return 0, 0
	}
	cpu := (ps.UserTime() + ps.SystemTime()).Seconds()
	ru, ok := ps.SysUsage().(*syscall.Rusage)
	if !ok {
		return cpu, 0
	}
	rss := int64(ru.Maxrss) / 1024 // KiB on Linux
	if runtime.GOOS == "darwin" {
		rss /= 1024 // bytes on macOS
	}
	return cpu, int(rss)
}

// processExit converts a Wait error into a shell-style exit code.
func processExit(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return 128 + int(ws.Signal())
		}
		return ee.ExitCode()
	}
	return 1
}
