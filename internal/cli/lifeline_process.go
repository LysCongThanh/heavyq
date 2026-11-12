package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

// A local command does not run as heavyq's direct child but under a lifeline:
// this same binary, started with lifelineEnv set and the read end of a pipe on
// fd 3 whose write end only heavyq holds (close-on-exec, so the command never
// gets it). The lifeline runs the command and waits for it. When heavyq dies,
// even by SIGKILL, the pipe reads EOF and the lifeline stops the command's whole
// tree: heavyq's slot and pool locks are freed by the kernel the moment it dies,
// so a command left running would share them with the next job. Remote jobs
// have the same lifeline on their stdin (remote_job.sh).
//
// The lifeline is also the command's subreaper on Linux, so orphans of the
// command that exit are reaped as they go instead of piling up as zombies.

const (
	// lifelineEnv marks a process started as a lifeline (see runLocal).
	lifelineEnv = "HEAVYQ_LIFELINE"
	// lifelineFD is the fd the pipe is handed over on (the first ExtraFiles entry).
	lifelineFD = 3
	// lifelineGrace is how long the command gets between SIGTERM and SIGKILL
	// once heavyq is gone: short, because its slot is already free.
	lifelineGrace = 2 * time.Second
)

// LifelineRequested reports whether this process was started as a lifeline:
// the variable is set and fd 3 is the pipe. A variable alone (exported by
// hand) is not enough.
func LifelineRequested() bool {
	if os.Getenv(lifelineEnv) != "1" {
		return false
	}
	var st syscall.Stat_t
	return syscall.Fstat(lifelineFD, &st) == nil && st.Mode&syscall.S_IFMT == syscall.S_IFIFO
}

// RunLifeline is the whole life of a lifeline process: it runs argv, returns
// its exit code (128+n for a signal), and stops the command's tree if heavyq
// goes away first.
func RunLifeline(argv []string) int {
	_ = os.Unsetenv(lifelineEnv)
	syscall.CloseOnExec(lifelineFD)
	line := os.NewFile(lifelineFD, "heavyq-lifeline")
	subreaper := becomeSubreaper()
	// heavyq forwards SIGTERM, which goes on to the command. A terminal's
	// SIGINT, SIGQUIT and SIGHUP reach the command by themselves (same process
	// group), and heavyq turns them into SIGTERM: they must not stop us first.
	sigs := make(chan os.Signal, 8)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP, syscall.SIGQUIT, syscall.SIGPIPE)
	if len(argv) == 0 {
		fmt.Fprintln(os.Stderr, "heavyq: lifeline: no command")
		return ExitUsage
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "heavyq:", err)
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
			return 127
		}
		return 126
	}
	go func() {
		for s := range sigs {
			if s == syscall.SIGTERM {
				_ = cmd.Process.Signal(s)
			}
		}
	}()
	broke, stopped := make(chan struct{}), make(chan struct{})
	if line != nil {
		go func() {
			_, _ = io.Copy(io.Discard, line) // returns once heavyq, the only writer, is gone
			close(broke)
			stopOwnTree()
			close(stopped)
		}()
	}
	code := reapUntil(cmd.Process.Pid, subreaper)
	select {
	case <-broke:
		// heavyq is gone and the command ended (most likely of our SIGTERM):
		// what it started must still get its SIGKILL pass, which only runs
		// while this process lives.
		<-stopped
	default:
	}
	return code
}

// reapUntil waits for every child of this process (the command and, as a
// subreaper, the orphans it adopted) until the command itself has ended, and
// returns the command's exit code. Without a subreaper (macOS) it waits for the
// command alone: there are no adopted orphans, and a wait for any child would
// also reap the ps that procinfo runs while stopping the tree, which then
// fails and reports live processes as gone.
func reapUntil(child int, subreaper bool) int {
	target := child
	if subreaper {
		target = -1
	}
	for {
		var ws syscall.WaitStatus
		pid, err := syscall.Wait4(target, &ws, 0, nil)
		switch {
		case errors.Is(err, syscall.EINTR):
			continue
		case err != nil:
			return 1
		case pid != child:
			continue // an orphan of the command, reaped so it is no zombie
		case ws.Signaled():
			return 128 + int(ws.Signal())
		default:
			return ws.ExitStatus()
		}
	}
}
