package remote

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
)

const (
	// shipAttempts bounds the attempts of a ship (see PushContext), and
	// shipRetryPause times the attempt number separates them.
	shipAttempts   = 3
	shipRetryPause = 200 * time.Millisecond
	// pushDeadline is the most one git push may take. It is a backstop: the
	// keepalives of gitSSHCommand end a push over a dead link within a minute,
	// and a signal or a cancel ends it at once; a first push of a large repo
	// over a slow link takes long but makes progress.
	pushDeadline = 30 * time.Minute
	// gitSSHCommand is the ssh git push runs when the user configured none:
	// the options of heavyq's own transport (no prompt, bounded connect) plus
	// keepalives, so a link that goes silent ends the push instead of holding
	// the slot until TCP gives up.
	gitSSHCommand = "ssh -o BatchMode=yes -o ConnectTimeout=10 -o ServerAliveInterval=15 -o ServerAliveCountMax=4"
)

// shipOnce makes sure the bare repo exists and pushes sha to refs/heavyq/<ref>.
func shipOnce(ctx context.Context, m config.MachineConfig, repoPath, name, sha, ref string, timeout time.Duration) error {
	if _, err := Transport(m.Transport).OutputContext(ctx, timeout, ensureBareScript, m.WorkDir, name); err != nil {
		return fmt.Errorf("prepare bare repo on %s: %w", m.Name, err)
	}
	pctx, cancel := context.WithTimeout(ctx, pushDeadline)
	defer cancel()
	url := strings.TrimSuffix(m.GitURL, "/") + "/" + name + ".git"
	cmd := exec.CommandContext(pctx, "git", pushArgs(repoPath, url, sha+":refs/heavyq/"+ref)...)
	cmd.Env = pushEnv(repoPath)
	isolate(cmd)
	var errb bytes.Buffer
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		switch {
		case ctx.Err() != nil:
			msg = "stopped"
		case pctx.Err() != nil:
			msg = fmt.Sprintf("not done after %s", pushDeadline)
		case msg == "":
			msg = err.Error()
		}
		return fmt.Errorf("git push %s@%.12s to %s: %s", name, sha, m.Name, msg)
	}
	return nil
}

// pushArgs is the git push of a ship. Shipping is plumbing: the user's push
// settings are for their own remotes. --recurse-submodules=no keeps
// push.recurseSubmodules (and submodule.recurse) from pushing a submodule's
// unpushed branch to the submodule's real origin, --no-signed and an empty
// push.pushOption keep push.gpgSign and push.pushOption from failing every
// ship (the bare repo supports neither), and --no-verify skips pre-push.
func pushArgs(repoPath, url, refspec string) []string {
	return []string{"-C", repoPath, "-c", "push.pushOption=", "push", "-q", "--force", "--no-verify",
		"--no-signed", "--recurse-submodules=no", "--no-follow-tags", url, refspec}
}

// pushEnv gives git push gitSSHCommand unless the user chose an ssh for git
// (GIT_SSH_COMMAND, GIT_SSH or core.sshCommand), which is kept as it is.
func pushEnv(repoPath string) []string {
	env := os.Environ()
	if os.Getenv("GIT_SSH_COMMAND") != "" || os.Getenv("GIT_SSH") != "" {
		return env
	}
	if out, err := exec.Command("git", "-C", repoPath, "config", "--get", "core.sshCommand").Output(); err == nil && strings.TrimSpace(string(out)) != "" {
		return env
	}
	return append(env, "GIT_SSH_COMMAND="+gitSSHCommand)
}

// isolate runs cmd in a session of its own and makes the end of its context
// kill its whole process group. A terminal's ^C then reaches heavyq only,
// which ends the context and reports the signal (not a failed ship), the ssh
// that git starts dies with git, and without a controlling terminal no ssh
// can stop to ask for a password.
func isolate(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second
}
