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

// ensureBareScript creates <work>/git/<name>.git if missing. Args: work name.
const ensureBareScript = `W="$1"; N="$2"
case "$W" in /*) ;; *) W="$HOME/$W" ;; esac
mkdir -p "$W/git" || exit 1
[ -d "$W/git/$N.git" ] || git init -q --bare "$W/git/$N.git" || exit 1
touch "$W/git/$N.git/heavyq-used"
`

// Push makes commit sha of the local repo at repoPath available in the bare
// repo <name>.git on machine m, under refs/heavyq/<ref>. Only that commit and
// its history are sent; uncommitted and ignored files never leave the machine.
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
