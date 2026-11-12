package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"
)

func (r *run) runLocal(c *claim) int {
	cmd, line := localCommand(r.opts.Argv)
	defer line.close()
	cmd.Dir = r.localDir()
	cmd.Stdin = os.Stdin
	cmd.Stdout, cmd.Stderr = r.cmdStreams() // the real streams, or tees of them for the digest
	// With --cache the streams are pipes: don't wait forever on a daemon that
	// inherited them after the command itself exited.
	cmd.WaitDelay = 10 * time.Second
	subreaper := becomeSubreaper()
	cmd.Env = append(os.Environ(), "HEAVYQ_MACHINE=local", "HEAVYQ_SLOT="+c.slot, "HEAVYQ_LANE="+r.me.Lane, "HEAVYQ_ACTIVE=1")
	cmd.Env = append(cmd.Env, c.lease.Env()...) // the pool values this run holds
	if line != nil {
		cmd.Env = append(cmd.Env, lifelineEnv+"=1")
	}
	// Slot and mutex descriptors are close-on-exec: the command (and any
	// daemon it leaves behind) never holds a slot. The slot is released when
	// this process exits; the lifeline then stops the command (lifeline_process.go).
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(r.stderr, "heavyq:", err)
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
			return 127
		}
		return 126
	}
	line.started()
	r.tree = newProcessTree(cmd.Process.Pid, subreaper)
	return r.supervise(func(s os.Signal) { _ = cmd.Process.Signal(s) }, func() int {
		err := cmd.Wait()
		if errors.Is(err, exec.ErrWaitDelay) {
			r.note("a background process kept the command's output open; stopped reading it %s after the command exited", cmd.WaitDelay)
		}
		r.cpuSecs, r.maxRSSMB = childUsage(cmd.ProcessState)
		return commandExit(cmd.ProcessState, err)
	})
}

// lifelinePipe is the pipe between heavyq (the write end, held until the
// command ended) and the lifeline process (the read end, fd 3 there).
type lifelinePipe struct{ rd, wr *os.File }

// localCommand is the exec.Cmd that runs argv locally: this binary as the
// command's lifeline (see lifeline_process.go), or argv itself when there is no
// such binary to start (a test binary of this package).
func localCommand(argv []string) (*exec.Cmd, *lifelinePipe) {
	exe := lifelineExecutable()
	if exe != "" {
		if rd, wr, err := os.Pipe(); err == nil {
			cmd := exec.Command(exe, argv...)
			cmd.ExtraFiles = []*os.File{rd} // fd 3 of the lifeline
			return cmd, &lifelinePipe{rd: rd, wr: wr}
		}
	}
	return exec.Command(argv[0], argv[1:]...), nil
}

// lifelineExecutable is the binary to start as a lifeline: this very binary
// (on Linux /proc/self/exe, which survives an upgrade that replaced the file).
func lifelineExecutable() string {
	exe, err := os.Executable()
	if err != nil || strings.HasSuffix(exe, ".test") {
		return ""
	}
	if runtime.GOOS == "linux" {
		if _, err := os.Stat("/proc/self/exe"); err == nil {
			return "/proc/self/exe"
		}
	}
	return exe
}

// started drops heavyq's copy of the read end: the lifeline holds it now.
func (l *lifelinePipe) started() {
	if l != nil {
		l.rd.Close()
	}
}

// close ends the lifeline (the command is over, or never started).
func (l *lifelinePipe) close() {
	if l != nil {
		l.rd.Close()
		l.wr.Close()
	}
}

// supervise waits for the child while forwarding termination signals. A
// signal to heavyq or an operator cancel stops the whole tree the command
// started, not just its direct child: SIGTERM, then SIGKILL after cancelGrace
// (at once on a further signal). The exit code is then 128+signal (or
// ExitCancelled) whatever the child's, and heavyq keeps the slot until nothing
// of the tree runs any more.
func (r *run) supervise(forward func(os.Signal), wait func() int) int {
	done := make(chan int, 1)
	go func() { done <- wait() }()
	sigCode := 0
	tick := time.NewTicker(controlPoll)
	defer tick.Stop()
	var (
		killAt   <-chan time.Time
		deadline time.Time
		killed   bool
	)
	stop := func(code int) {
		sigCode = code
		r.tree.signal(syscall.SIGTERM)
		forward(syscall.SIGTERM)
		deadline = time.Now().Add(cancelGrace)
		killAt = time.After(cancelGrace)
	}
	kill := func() {
		killed = true
		r.tree.signal(syscall.SIGKILL)
		forward(syscall.SIGKILL)
	}
	for {
		select {
		case rc := <-done:
			if sigCode != 0 {
				r.reapTree(deadline, killed)
				return sigCode
			}
			return rc
		case <-tick.C:
			if sigCode == 0 && controlBox().CancelRequested(r.me.Name) {
				r.note("cancelled by an operator; stopping the command")
				r.cancelled = true
				stop(ExitCancelled)
			}
		case <-killAt:
			kill()
		case s := <-r.sigs:
			if sigCode == 0 {
				r.interrupted = true
				stop(signalExit(s))
			} else {
				kill()
			}
		}
	}
}

// reapTree keeps the slot until everything the stopped command started is
// gone: survivors get the rest of the grace period, then SIGKILL, again on every
// pass (a process can fork between a scan and its own kill), at once after a
// further signal.
func (r *run) reapTree(deadline time.Time, killed bool) {
	for r.tree.alive() {
		if killed || !time.Now().Before(deadline) {
			killed = true
			r.tree.signal(syscall.SIGKILL)
		}
		select {
		case <-r.sigs:
			killed = true
		case <-time.After(100 * time.Millisecond):
		}
	}
}

const (
	controlPoll = time.Second      // how often a running ticket checks for a cancel request
	cancelGrace = 10 * time.Second // SIGTERM -> SIGKILL delay for a stopped command
)
