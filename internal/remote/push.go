package remote

import (
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
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
const ensureBareScript = `W="$1"; N="$2"
case "$W" in /*) ;; *) W="$HOME/$W" ;; esac
mkdir -p "$W/git" || exit 1
B="$W/git/$N.git"
[ -d "$B" ] || git -c core.hooksPath=/dev/null init -q --bare "$B" || exit 1
hp() { git -C "$B" config --get core.hooksPath 2>/dev/null; }
[ "$(hp)" = /dev/null ] || git -C "$B" config core.hooksPath /dev/null || [ "$(hp)" = /dev/null ] || exit 1
touch "$B/heavyq-used"
`

// Push makes commit sha of the local repo at repoPath available in the bare
// repo <name>.git on machine m, under refs/heavyq/<ref>. Only that commit and
// its history are sent; uncommitted and ignored files never leave the machine.
//
// No hook of the local repo runs: --no-verify skips pre-push, the only hook a push to a URL calls on this
// side. The receiving side is covered by ensureBareScript.
func Push(m config.MachineConfig, repoPath, name, sha, ref string, timeout time.Duration) error {
	if !config.SafeName.MatchString(name) || !fullSHA.MatchString(sha) {
		return fmt.Errorf("unsafe repo name %q or sha %q", name, sha)
	}
	if _, err := Transport(m.Transport).Output(timeout, ensureBareScript, m.WorkDir, name); err != nil {
		return fmt.Errorf("prepare bare repo on %s: %w", m.Name, err)
	}
	url := strings.TrimSuffix(m.GitURL, "/") + "/" + name + ".git"
	cmd := exec.Command("git", "-C", repoPath, "push", "-q", "--force", "--no-verify", url, sha+":refs/heavyq/"+ref)
	var errb bytes.Buffer
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git push %s@%.12s to %s: %s", name, sha, m.Name, strings.TrimSpace(errb.String()))
	}
	return nil
}
