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

// StartTime returns an opaque, stable token for when pid started. Comparing
// it guards against pid reuse: same pid + same token = same process.
func (p ProcFS) StartTime(pid int) (string, error) {
	if pid <= 0 {
		return "", errors.New("invalid pid")
	}
	if p.UsePS {
		return psStartTime(pid)
	}
	f, err := p.statFields(pid)
	if err == nil {
		return f[19], nil // field 22: starttime in clock ticks since boot
	}
	if p.Available() {
		return "", err // /proc exists, process does not
	}
	return psStartTime(pid)
}

// Alive reports whether pid is running and is still the process that had
// start token start (not a recycled pid).
func (p ProcFS) Alive(pid int, start string) bool {
	if pid <= 0 || start == "" {
		return false
	}
	if err := syscall.Kill(pid, 0); err != nil && !errors.Is(err, syscall.EPERM) {
		return false
	}
	got, err := p.StartTime(pid)
	return err == nil && got == start
}

// Running reports whether pid exists and is not a zombie.
func (p ProcFS) Running(pid int) bool {
	if p.UsePS {
		return psRunning(pid)
	}
	f, err := p.statFields(pid)
	if err != nil {
		return syscall.Kill(pid, 0) == nil && !p.Available()
	}
	return f[0] != "Z" && f[0] != "X"
}
