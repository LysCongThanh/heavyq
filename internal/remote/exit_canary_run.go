package remote

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
)

// The exit canary: before a remote may prove anything, heavyq runs `exit 3` through the very job path a run
// takes there (remote_job.sh, its PATH and env_path, its choice of setsid or Perl) and checks that the job
// returns exactly 3, and only once the command finished: a file the canary writes just before it exits must
// be there when the job has returned. A remote whose PATH had a setsid wrapper that forked and exited 0 once
// reported every run as passed; the canary catches that and anything like it, whatever the script trusts.

// canaryExit is the code the canary command exits with: not 0, not one the job script reserves.
const canaryExit = 3

// canaryCheckScript says whether the canary's file is in the remote's cache dir, and removes it.
// Args: <work_dir> <nonce>.
const canaryCheckScript = `W="$1"; case "$W" in /*) ;; *) W="$HOME/$W" ;; esac
f="$W/cache/heavyq-canary-$2"
if [ -f "$f" ]; then rm -f "$f"; echo heavyq-canary-present; else echo heavyq-canary-absent; fi
`

// CanaryJob is where the canary runs: a commit already pushed to the machine and the slot the run holds.
type CanaryJob struct {
	Repo, SHA, Lane, Slot string
}

// RunCanary runs the exit canary on m. trusted reports the verdict and reason says why it is false. err
// means the canary could not run (unreachable, slot busy, setup failed): no verdict either way.
func RunCanary(m config.MachineConfig, j CanaryJob, timeout time.Duration) (trusted bool, reason string, err error) {
	nonce, err := canaryNonce()
	if err != nil {
		return false, "", err
	}
	mc := m
	mc.CacheHook = "" // the dependency hook is not part of the exit path, and may be slow
	script := fmt.Sprintf(`sleep 0.3; printf ok > "$HEAVYQ_CACHE_DIR/heavyq-canary-%s"; exit %d`, nonce, canaryExit)
	var out bytes.Buffer
	job, err := StartJob(JobSpec{Machine: mc, Repo: j.Repo, SHA: j.SHA, Lane: j.Lane, Slot: j.Slot,
		Argv: []string{"sh", "-c", script}, Stdout: &out, Stderr: &out})
	if err != nil {
		return false, "", err
	}
	done := make(chan int, 1)
	go func() { done <- job.Wait() }()
	var rc int
	select {
	case rc = <-done:
	case <-time.After(timeout):
		job.Signal(os.Kill)
		<-done
		return false, "", fmt.Errorf("no answer within %s", timeout)
	}
	switch rc {
	case ExitSlotBusy, ExitSetupFailed, ExitConnectionLost:
		return false, "", fmt.Errorf("job code %d: %s", rc, lastLine(out.String()))
	case canaryExit:
	default:
		return false, fmt.Sprintf("the canary `exit %d` came back as %d", canaryExit, rc), nil
	}
	got, err := Transport(mc.Transport).Output(timeout, canaryCheckScript, mc.WorkDir, nonce)
	switch {
	case err != nil:
		return false, "", fmt.Errorf("canary file check: %w", err)
	case strings.Contains(got, "heavyq-canary-present"):
		return true, "", nil
	case strings.Contains(got, "heavyq-canary-absent"):
		return false, "the job returned before its command finished (the canary's file was not there)", nil
	}
	return false, "", fmt.Errorf("canary file check: unexpected output %q", strings.TrimSpace(got))
}

func canaryNonce() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// lastLine is the last non-empty line of s, for an error message.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
