package procinfo

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// psTimeout bounds one ps run. A healthy ps takes tens of milliseconds even
// with thousands of processes; a wedged one must not hang `heavyq status`.
const psTimeout = 5 * time.Second

// runPS runs ps with a fixed C locale (stable column and date formats, and
// a start token that does not change with the caller's LANG).
func runPS(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), psTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ps", args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	return cmd.Output()
}

// psSnapshot lists every process. Fail-soft like the /proc reader: when ps
// fails the table is empty, and callers see "no processes" rather than an
// error.
func psSnapshot() map[int]procSample {
	out, err := runPS("-axo", psColumns)
	if err != nil && len(out) == 0 {
		return map[int]procSample{}
	}
	return parsePS(out)
}

var (
	psProbeOnce sync.Once
	psProbeOK   bool
)

// psWorks reports whether ps can list processes here (probed once: ps must
// exist and see this very process).
func psWorks() bool {
	psProbeOnce.Do(func() {
		out, err := runPS("-o", "pid=", "-p", strconv.Itoa(os.Getpid()))
		psProbeOK = err == nil && strings.TrimSpace(string(out)) == strconv.Itoa(os.Getpid())
	})
	return psProbeOK
}

// psStartTime is the start token from `ps -o lstart=`: the start date with
// whitespace folded to underscores. It has one-second resolution, enough to
// tell a recycled pid from the original in practice.
func psStartTime(pid int) (string, error) {
	out, err := runPS("-o", "lstart=", "-p", strconv.Itoa(pid))
	token := strings.Join(strings.Fields(string(out)), "_")
	if err != nil || token == "" {
		return "", errors.New("process not found")
	}
	return token, nil
}

// psRunning reports whether pid exists and is not a zombie (state "Z").
func psRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	out, err := runPS("-o", "stat=", "-p", strconv.Itoa(pid))
	state := strings.TrimSpace(string(out))
	return err == nil && state != "" && state[0] != 'Z' && state[0] != 'X'
}

// isKernelTask reports whether a process is the kernel itself rather than
// something a user could be running: on macOS pid 0 is kernel_task, whose
// accumulated CPU time and wired memory would drown every "outside" group.
func isKernelTask(pid int, _ procSample) bool { return pid == 0 }
