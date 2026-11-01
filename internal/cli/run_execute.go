package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/remote"
)

// execute runs the command on the claimed machine and records history.
func (r *run) execute(c *claim) int {
	waited := time.Since(r.t0)
	if r.pl.defaultMode && c.machine != config.LocalName {
		fmt.Fprintf(r.stderr, "heavyq: placed on %s by score (clean repo, commit %.12s; force local with --remote local)\n", c.machine, r.pl.sha)
	}
	fmt.Fprintf(r.stderr, "heavyq: our turn after %s (prio %d, lane %s, %s/%s): %s\n",
		fmtDur(waited), r.me.Prio, r.me.Lane, c.machine, c.slot, r.me.Cmd)
	runStart := time.Now()
	var rc int
	if c.machine == config.LocalName {
		rc = r.runLocal(c)
	} else {
		rc = r.runRemote(c)
	}
	runSecs := time.Since(runStart).Seconds()
	ran := !r.interrupted && rc != 126 && rc != 127 &&
		(c.machine == config.LocalName || (rc != ExitRemoteSetup && rc != ExitConnLost))
	r.cacheFinish(c.machine, rc, ran, runSecs)
	rec := history.Record{End: time.Now(), Machine: c.machine, Lane: r.me.Lane, Prio: r.me.Prio, Light: r.me.Light,
		WaitSecs: waited.Seconds(), RunSecs: runSecs, Exit: rc, Cmd: r.me.Cmd}
	if r.cache != nil {
		rec.Cache, rec.CacheNote = r.cache.status, r.cache.note
	}
	if err := history.Append(history.DefaultPath(), rec); err != nil {
		fmt.Fprintln(r.stderr, "heavyq: warning: history not written:", err)
	}
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
	cmd.Stdout, cmd.Stderr = r.cmdStreams()
	// With --cache the streams are pipes: don't wait forever on a daemon that
	// inherited them after the command itself exited.
	cmd.WaitDelay = 10 * time.Second
	cmd.Env = append(os.Environ(), "HEAVYQ_MACHINE=local", "HEAVYQ_SLOT="+c.slot, "HEAVYQ_LANE="+r.me.Lane, "HEAVYQ_ACTIVE=1")
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
	return r.supervise(func(s os.Signal) { _ = cmd.Process.Signal(s) }, func() int {
		return processExit(cmd.Wait())
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
	var sibs []string
	for _, s := range r.pl.siblings {
		if err := remote.Push(m, s.Path, s.Name, s.SHA, s.SHA[:12], timeout); err != nil {
			fmt.Fprintln(r.stderr, "heavyq:", err)
			return ExitRemoteSetup
		}
		sibs = append(sibs, s.Name+"@"+s.SHA)
	}
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
// exit code is 128+signal regardless of the child's code.
func (r *run) supervise(forward func(os.Signal), wait func() int) int {
	done := make(chan int, 1)
	go func() { done <- wait() }()
	sigCode := 0
	for {
		select {
		case rc := <-done:
			if sigCode != 0 {
				return sigCode
			}
			return rc
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
