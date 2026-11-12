// Package procinfo reads process identity and CPU usage.
//
// On Linux the source is /proc. The /proc root is injectable (ProcFS.Root)
// so parsing edge cases can be tested against a fake tree; production always
// uses "/proc". On macOS there is no /proc and heavyq avoids cgo, so the
// source is `ps` (ProcFS.UsePS, see ps_process_source.go): the same
// snapshot shape feeds Breakdown, Attribute and Descendants unchanged.
package procinfo

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

// ProcFS is a handle on a process source: a /proc-like directory, or `ps`
// when UsePS is set.
type ProcFS struct {
	Root  string
	UsePS bool // read processes from ps(1) instead of Root (macOS)
}

// Default is the real process source of this platform: /proc, or ps on macOS.
var Default = ProcFS{Root: "/proc", UsePS: runtime.GOOS == "darwin"}

// Available reports whether the process source works: the /proc tree
// exists, or (UsePS) ps answers.
func (p ProcFS) Available() bool {
	if p.UsePS {
		return psWorks()
	}
	_, err := os.Stat(filepath.Join(p.Root, "self", "stat"))
	if err == nil {
		return true
	}
	_, err = os.Stat(filepath.Join(p.Root, "1", "stat"))
	return err == nil
}

// statFields returns the fields of /proc/<pid>/stat that follow the
// "(comm)" field, which may itself contain spaces and parentheses.
// Index 0 is the state (field 3), index 1 is ppid (field 4).
func (p ProcFS) statFields(pid int) ([]string, error) {
	_, f, err := p.stat(pid)
	return f, err
}

// stat returns the command name (comm) of pid and the fields that follow it.
func (p ProcFS) stat(pid int) (string, []string, error) {
	b, err := os.ReadFile(filepath.Join(p.Root, strconv.Itoa(pid), "stat"))
	if err != nil {
		return "", nil, err
	}
	s := string(b)
	i := strings.LastIndex(s, ")")
	j := strings.IndexByte(s, '(')
	if i < 0 || j < 0 || j > i || i+2 > len(s) {
		return "", nil, errors.New("malformed stat")
	}
	f := strings.Fields(s[i+1:])
	if len(f) < 22 {
		return "", nil, errors.New("short stat")
	}
	return s[j+1 : i], f, nil
}

// errGone means the process source answered and pid is not there. Any
// other identity error means the source could not answer (a fork that
// failed, a ps killed by its timeout): nothing is known about pid then.
var errGone = errors.New("process not found")

// identity returns pid's state letter ("R", "S", "Z", ...) and its start
// token, read in one go from /proc/<pid>/stat or from one ps run.
func (p ProcFS) identity(pid int) (state, start string, err error) {
	if p.UsePS {
		return psIdentity(pid)
	}
	f, err := p.statFields(pid)
	switch {
	case err == nil:
		return f[0], f[19], nil // field 22: starttime in clock ticks since boot
	case !p.Available():
		return psIdentity(pid)
	case errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ESRCH):
		return "", "", errGone // /proc exists, process does not
	}
	return "", "", err
}

// StartTime returns an opaque, stable token for when pid started. Comparing
// it guards against pid reuse: same pid + same token = same process.
func (p ProcFS) StartTime(pid int) (string, error) {
	if pid <= 0 {
		return "", errors.New("invalid pid")
	}
	_, start, err := p.identity(pid)
	if err == nil && start == "" {
		err = errGone
	}
	return start, err
}

// Alive reports whether pid is running and is still the process that had
// start token start (not a recycled pid, not an exited zombie).
//
// Callers delete state when it says false (queue tickets, leases), so it
// fails closed: once kill(2) has seen the pid, only a source that answered
// "no such process", another start token or an exited state make it dead.
// A source that could not answer (ps not runnable or killed, which is most
// likely on an overloaded machine) leaves the process alive.
func (p ProcFS) Alive(pid int, start string) bool {
	if pid <= 0 || start == "" {
		return false
	}
	if err := syscall.Kill(pid, 0); err != nil && !errors.Is(err, syscall.EPERM) {
		return false
	}
	state, got, err := p.identity(pid)
	if err != nil {
		return !errors.Is(err, errGone)
	}
	return got == start && !exited(state)
}

// Running reports whether pid exists and is not a zombie.
func (p ProcFS) Running(pid int) bool {
	if p.UsePS {
		state, _, err := psIdentity(pid)
		return err == nil && !exited(state)
	}
	f, err := p.statFields(pid)
	if err != nil {
		return syscall.Kill(pid, 0) == nil && !p.Available()
	}
	return !exited(f[0])
}

// exited reports whether a process state is zombie (Z) or dead (X): the
// process has exited and only waits for its parent to reap it.
func exited(state string) bool {
	return state != "" && (state[0] == 'Z' || state[0] == 'X')
}
