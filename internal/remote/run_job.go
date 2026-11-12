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
	"regexp"
	"strconv"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/config"
)

//go:embed remote_job.sh
var jobScript string

// Job exit codes reserved by remote_job.sh (the command's own 97, 98 and 255
// are remapped to 96 so these stay unambiguous).
const (
	ExitSlotBusy    = 97
	ExitSetupFailed = 98
	// ExitConnectionLost is what ssh returns when the connection drops. The job
	// never returns it for the command, so it always means the transport.
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
	// Env (NAME=value) is set and Unset is removed in the command's
	// environment, whatever the remote's own environment holds: the variables
	// that are part of the run's identity (cache.env). The job's own steps do
	// not see them.
	Env   []string
	Unset []string
	// Quiet leaves out the job's banner line (machine, commit, slot): an
	// agent's run prints only heavyq's running line and digest.
	Quiet  bool
	Stdout io.Writer
	Stderr io.Writer
}

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// commandEnv is the shell code that gives the command Env and Unset ("" for
// none). Names are checked (and the job's own HEAVYQ_ names refused), values
// quoted.
func commandEnv(set, unset []string) (string, error) {
	var b strings.Builder
	ok := func(n string) bool { return envName.MatchString(n) && !strings.HasPrefix(n, "HEAVYQ_") }
	for _, kv := range set {
		n, v, _ := strings.Cut(kv, "=")
		if !ok(n) {
			return "", fmt.Errorf("cannot pass the variable %q to a remote job", n)
		}
		b.WriteString("export " + n + "=" + ShellQuote(v) + "\n")
	}
	for _, n := range unset {
		if !ok(n) {
			return "", fmt.Errorf("cannot pass the variable %q to a remote job", n)
		}
		b.WriteString("unset " + n + "\n")
	}
	return b.String(), nil
}

// Started is a running remote job.
type Started struct {
	cmd      *exec.Cmd
	lifeline *os.File
}

// StartJob launches the job. Its stdin is a pipe that carries the command
// line and nothing after it: if heavyq dies (even by SIGKILL) the pipe closes
// and the remote side cleans up, which also covers transports that do not
// forward signals. The command line goes there rather than into an argument:
// the transport's command is one exec argument, limited to 128 KiB, and a
// command line as long as a local argv does not fit in it.
func StartJob(s JobSpec) (*Started, error) {
	if !config.SafeName.MatchString(s.Repo) || !fullSHA.MatchString(s.SHA) ||
		!config.SafeName.MatchString(s.Lane) || !config.SafeName.MatchString(s.Slot) {
		return nil, errors.New("unsafe remote job parameters")
	}
	cmdEnv, err := commandEnv(s.Env, s.Unset)
	if err != nil {
		return nil, err
	}
	// The identity variables travel with the command line on stdin, not in an
	// environment variable: everything in env goes into the transport's one
	// exec argument, and their values could fill it as a long argv would.
	line := b64(cmdEnv + jobCommandLine(s.Argv))
	env := []string{
		"HEAVYQ_CMD_LEN=" + strconv.Itoa(len(line)),
		"HEAVYQ_SUBDIR=" + s.Subdir,
		"HEAVYQ_WITH=" + strings.Join(s.Siblings, ","),
		"HEAVYQ_WATCH_STDIN=1",
	}
	// Every input is set, empty when unused, so a value inherited from the caller's environment (a heavyq
	// run inside another heavyq job, or a local transport) never stands in for this job's own.
	env = append(env, "HEAVYQ_BASE_REF="+s.BaseRef, "HEAVYQ_BASE_SHA="+s.BaseSHA, "HEAVYQ_ENV_PATH="+s.Machine.EnvPath)
	hook := ""
	if s.Machine.CacheHook != "" {
		hook = b64(s.Machine.CacheHook)
	}
	env = append(env, "HEAVYQ_HOOK_B64="+hook)
	quiet := ""
	if s.Quiet {
		quiet = "1"
	}
	env = append(env, "HEAVYQ_JOB_QUIET="+quiet)
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
	// The write ends once the job has read the line, or with an error once it is gone; Wait closes w.
	go func() { _, _ = io.WriteString(w, line) }()
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

// jobCommandLine is argv as one command line for the remote bash. The first
// word is always quoted: to bash a quoted word is a plain command name, as
// argv[0] is to a local exec, never a reserved word (time, if, coproc) or an
// assignment (FOO=1).
func jobCommandLine(argv []string) string {
	if len(argv) == 0 {
		return ""
	}
	first := "'" + strings.ReplaceAll(argv[0], "'", `'\''`) + "'"
	if len(argv) == 1 {
		return first
	}
	return first + " " + ShellJoin(argv[1:])
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
