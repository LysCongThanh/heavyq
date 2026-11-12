package remote

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
)

var fullSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

// ensureBareScript creates <work>/git/<name>.git if missing and switches git hooks off in it. Args: work name.
//
// Shipping is plumbing: a hook that runs in the bare repo (a template that carries hooks, a global
// core.hooksPath on this machine, hooks left by an earlier version) could decline the push, print into the
// job's output or run slow checks. core.hooksPath=/dev/null in the repo's own config overrides every one of
// them for every command that runs in or through the repo: the receiving side of the push, "git worktree
// add" and "git update-ref" of the job script. Init runs under the same setting because a
// reference-transaction hook already sees the repo's first ref. Two runs may set it at once; the loser only
// needs to see the value the winner wrote.
//
// Runs shipping one repo to a new remote at once all try to create it, and
// every git init but one may fail on the lock of its config ("could not lock
// config file"). A loser waits for the winner and goes on once the directory
// is a repository; any other failure of git init is reported as it is.
const ensureBareScript = `W="$1"; N="$2"
case "$W" in /*) ;; *) W="$HOME/$W" ;; esac
D="$W/git/$N.git"
mkdir -p "$W/git" || exit 1
if ! git --git-dir="$D" rev-parse --git-dir >/dev/null 2>&1; then
  E=$(git -c core.hooksPath=/dev/null init -q --bare "$D" 2>&1) || { sleep 1; git --git-dir="$D" rev-parse --git-dir >/dev/null 2>&1; } ||
    { printf '%s\n' "$E" >&2; exit 1; }
fi
hp() { git -C "$D" config --get core.hooksPath 2>/dev/null; }
[ "$(hp)" = /dev/null ] || git -C "$D" config core.hooksPath /dev/null || [ "$(hp)" = /dev/null ] || exit 1
touch "$D/heavyq-used"
`

// Push makes commit sha of the local repo at repoPath available in the bare
// repo <name>.git on machine m, under refs/heavyq/<ref>. Only that commit and
// its history are sent; uncommitted and ignored files never leave the machine.
//
// No hook of the local repo runs: --no-verify skips pre-push, the only hook a push to a URL calls on this
// side. The receiving side is covered by ensureBareScript.
func Push(m config.MachineConfig, repoPath, name, sha, ref string, timeout time.Duration) error {
	return PushContext(context.Background(), m, repoPath, name, sha, ref, timeout)
}

// PushContext is Push that stops when ctx ends: the ensure step and git push
// (with its ssh) run in a session of their own and are killed with it.
//
// Runs that ship the same new commit to one remote at once race on its ref:
// receive-pack creates refs/heavyq/<ref> against the value the push saw
// advertised (none), so a slower push is refused with "reference already
// exists" although the commit is there; the next attempt finds the ref in
// place and has nothing to send. On a remote with no bare repo yet a push may
// also reach the repo while another run is still creating it, which costs
// one more attempt, so a ship makes up to shipAttempts.
func PushContext(ctx context.Context, m config.MachineConfig, repoPath, name, sha, ref string, timeout time.Duration) error {
	if !config.SafeName.MatchString(name) || !fullSHA.MatchString(sha) {
		return fmt.Errorf("unsafe repo name %q or sha %q", name, sha)
	}
	var err error
	for attempt := 1; ; attempt++ {
		err = shipOnce(ctx, m, repoPath, name, sha, ref, timeout)
		if err == nil || ctx.Err() != nil || attempt == shipAttempts {
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(time.Duration(attempt) * shipRetryPause):
		}
	}
}
