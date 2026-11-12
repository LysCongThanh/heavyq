package procinfo

import (
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// Owner is what a survey found about one live process.
type Owner struct {
	Start   string // the start token, as StartTime returns it
	Stopped bool   // stopped by job control (Ctrl-Z, SIGSTOP) or a tracer
}

// Survey reads the start token and stop state of every pid in one pass:
// one /proc read per pid, or ONE ps run for all of them where ps is the
// source (macOS). A pid that is gone, or has exited and waits to be reaped
// (a zombie, as Alive judges it), is absent from the result. ok is false
// when the process table could not be read at all; a caller must then not
// take an absent pid for a dead one.
func (p ProcFS) Survey(pids []int) (map[int]Owner, bool) {
	var live []int
	for _, pid := range pids {
		if pid <= 0 {
			continue
		}
		// kill(pid, 0) is a cheap first cut: EPERM means the pid exists (another user's).
		if err := syscall.Kill(pid, 0); err == nil || errors.Is(err, syscall.EPERM) {
			live = append(live, pid)
		}
	}
	out := map[int]Owner{}
	if len(live) == 0 {
		return out, true
	}
	if p.UsePS || !p.Available() {
		return psSurvey(live)
	}
	for _, pid := range live {
		if f, err := p.statFields(pid); err == nil && !exited(f[0]) {
			out[pid] = Owner{Start: f[19], Stopped: f[0] == "T" || f[0] == "t"}
		}
	}
	return out, true
}

// psSurvey asks ps about all pids at once. lstart is the last column: it is
// several words, folded to one token exactly as psStartTime folds it.
func psSurvey(pids []int) (map[int]Owner, bool) {
	list := make([]string, len(pids))
	for i, pid := range pids {
		list[i] = strconv.Itoa(pid)
	}
	b, err := runPS("-o", "pid=,stat=,lstart=", "-p", strings.Join(list, ","))
	var exit *exec.ExitError
	// ps exits 1 when none of the pids exists any more: a valid answer. Anything else
	// (no ps, a timeout, a crash) is not.
	if err != nil && !(errors.As(err, &exit) && exit.ExitCode() == 1) {
		return nil, false
	}
	out := map[int]Owner{}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		pid, err := strconv.Atoi(f[0])
		if err != nil || exited(f[1]) {
			continue
		}
		out[pid] = Owner{Start: strings.Join(f[2:], "_"), Stopped: f[1][0] == 'T' || f[1][0] == 't'}
	}
	return out, true
}
