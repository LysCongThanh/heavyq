// Package procinfo reads process identity and CPU usage from /proc.
//
// The /proc root is injectable (ProcFS.Root) so parsing edge cases can be
// tested against a fake tree; production always uses "/proc". On systems
// without /proc (macOS) identity falls back to `ps -o lstart=` and CPU
// attribution reports itself unavailable.
package procinfo

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// ProcFS is a handle on a /proc-like directory.
type ProcFS struct{ Root string }

// Default is the real /proc.
var Default = ProcFS{Root: "/proc"}

// Available reports whether the /proc tree exists.
func (p ProcFS) Available() bool {
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
	b, err := os.ReadFile(filepath.Join(p.Root, strconv.Itoa(pid), "stat"))
	if err != nil {
		return nil, err
	}
	s := string(b)
	i := strings.LastIndex(s, ")")
	if i < 0 || i+2 > len(s) {
		return nil, errors.New("malformed stat")
	}
	f := strings.Fields(s[i+1:])
	if len(f) < 20 {
		return nil, errors.New("short stat")
	}
	return f, nil
}

// StartTime returns an opaque, stable token for when pid started. Comparing
// it guards against pid reuse: same pid + same token = same process.
func (p ProcFS) StartTime(pid int) (string, error) {
	if pid <= 0 {
		return "", errors.New("invalid pid")
	}
	f, err := p.statFields(pid)
	if err == nil {
		return f[19], nil // field 22: starttime in clock ticks since boot
	}
	if p.Available() {
		return "", err // /proc exists, process does not
	}
	out, perr := exec.Command("ps", "-o", "lstart=", "-p", strconv.Itoa(pid)).Output()
	token := strings.Join(strings.Fields(string(out)), "_")
	if perr != nil || token == "" {
		return "", errors.New("process not found")
	}
	return token, nil
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
