package remote

import (
	"context"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/config"
)

//go:embed remote_job.sh
var jobScript string

// Job exit codes reserved by remote_job.sh (the command's own 97/98 are
// remapped to 96 so these stay unambiguous).
const (
	ExitSlotBusy    = 97
	ExitSetupFailed = 98
	// ExitConnectionLost is what ssh returns when the connection drops.
	ExitConnectionLost = 255
)

// JobSpec describes one remote run.
type JobSpec struct {
	Machine  config.MachineConfig
	Repo     string // bare repo name (directory name of the local repo)
	SHA      string
	Lane     string
	Slot     string
	Argv     []string // command; joined with shell quoting
	Subdir   string
	Siblings []string // "name@sha"
	BaseRef  string
	BaseSHA  string
	Stdout   io.Writer
	Stderr   io.Writer
}

// Started is a running remote job.
type Started struct {
	cmd      *exec.Cmd
	lifeline *os.File
}

// StartJob launches the job. Its stdin is a pipe heavyq never writes to: if
// heavyq dies (even by SIGKILL) the pipe closes and the remote side cleans
// up, which also covers transports that do not forward signals.
func StartJob(s JobSpec) (*Started, error) {
	if !config.SafeName.MatchString(s.Repo) || !fullSHA.MatchString(s.SHA) ||
		!config.SafeName.MatchString(s.Lane) || !config.SafeName.MatchString(s.Slot) {
		return nil, errors.New("unsafe remote job parameters")
	}
	env := []string{
		"HEAVYQ_CMD_B64=" + b64(ShellJoin(s.Argv)),
		"HEAVYQ_SUBDIR=" + s.Subdir,
		"HEAVYQ_WITH=" + strings.Join(s.Siblings, ","),
		"HEAVYQ_WATCH_STDIN=1",
	}
	if s.BaseRef != "" {
		env = append(env, "HEAVYQ_BASE_REF="+s.BaseRef, "HEAVYQ_BASE_SHA="+s.BaseSHA)
	}
	if s.Machine.EnvPath != "" {
		env = append(env, "HEAVYQ_ENV_PATH="+s.Machine.EnvPath)
	}
	if s.Machine.CacheHook != "" {
		env = append(env, "HEAVYQ_HOOK_B64="+b64(s.Machine.CacheHook))
	}
	cmd, err := Transport(s.Machine.Transport).Command(context.Background(), env, jobScript, s.Machine.WorkDir, s.Repo, s.SHA, s.Lane, s.Slot)
	if err != nil {
		return nil, err
	}
	r, w, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("lifeline pipe: %w", err)
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = r, s.Stdout, s.Stderr
	if err := cmd.Start(); err != nil {
		r.Close()
		w.Close()
		return nil, fmt.Errorf("start transport %q: %w", s.Machine.Transport[0], err)
	}
	r.Close() // the child owns the read end now
	return &Started{cmd: cmd, lifeline: w}, nil
}

// Pid of the transport process.
func (j *Started) Pid() int { return j.cmd.Process.Pid }

// Signal forwards sig to the transport process (ssh closes the session).
func (j *Started) Signal(sig os.Signal) { _ = j.cmd.Process.Signal(sig) }

// Wait waits for the job and returns its exit code.
func (j *Started) Wait() int {
	err := j.cmd.Wait()
	j.lifeline.Close()
	return exitCode(err)
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if c := ee.ExitCode(); c >= 0 {
			return c
		}
		return ExitConnectionLost // killed by a signal
	}
	return ExitConnectionLost
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
