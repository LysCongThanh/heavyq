package procinfo

import (
	"context"
	"errors"
	"fmt"
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

// runPS runs ps with a fixed C locale (stable column and date formats) and
// a fixed zone. lstart is printed in the local time of ps, so without TZ two
// heavyq processes in different zones (or one before and one after the
// system zone changes) read different start tokens for the same process and
// prune each other's live tickets. Later entries win over the caller's.
func runPS(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), psTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ps", args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "TZ=UTC0")
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

// psIdentity reads pid's state and start token from
// `ps -o stat= -o lstart=`. The token is the start date in UTC with
// whitespace folded to underscores; one-second resolution is enough to tell
// a recycled pid from the original in practice.
//
// It returns errGone only when ps ran to the end and listed nothing (exit 0,
// or 1 for "no such pid", with empty output). A ps that could not start,
// was killed by psTimeout or a signal, or failed otherwise gives another
// error: that says nothing about pid, and Alive must not read it as dead.
func psIdentity(pid int) (state, start string, err error) {
	if pid <= 0 {
		return "", "", errGone
	}
	out, err := runPS("-o", "stat=", "-o", "lstart=", "-p", strconv.Itoa(pid))
	f := strings.Fields(string(out))
	var exit *exec.ExitError
	answered := err == nil || (errors.As(err, &exit) && exit.ExitCode() == 1)
	switch {
	case answered && len(f) == 0:
		return "", "", errGone
	case err != nil:
		return "", "", fmt.Errorf("ps cannot tell whether pid %d runs: %w", pid, err)
	}
	return f[0], strings.Join(f[1:], "_"), nil
}

// isKernelTask reports whether a process is the kernel itself rather than
// something a user could be running: on macOS pid 0 is kernel_task, whose
// accumulated CPU time and wired memory would drown every "outside" group.
func isKernelTask(pid int, _ procSample) bool { return pid == 0 }
