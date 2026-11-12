package cli

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"syscall"
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

// commandExit is the exit code of a command that was waited for: its own
// status whenever the process is known to have ended. An error on top of it (a
// background process still held the output when WaitDelay ran out, the output
// could not be copied) says nothing about how the command itself ended.
func commandExit(ps *os.ProcessState, err error) int {
	if ps != nil {
		if ws, ok := ps.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return 128 + int(ws.Signal())
		}
		if ps.Exited() {
			return ps.ExitCode()
		}
	}
	return processExit(err)
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
