package statusline

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// wrapTimeout bounds the user's own command: a status line that never returns
// must not hold Claude Code's updates, and it is theirs, not ours, to fix.
// A variable only so a test can shorten it.
var wrapTimeout = 5 * time.Second

// maxWrapped bounds what is kept of the wrapped command's output.
const maxWrapped = 64 << 10

// runWrapped runs the user's existing status-line command with the same stdin
// Claude Code gave heavyq, and returns what it printed without the trailing
// newline, plus its exit code. The command comes from the user's own settings
// (it is what they had in `statusLine.command`), so it runs through the shell
// exactly as Claude Code would have run it; heavyq never edits it.
func runWrapped(command string, stdin []byte, dir string, stderr io.Writer) (string, int) {
	ctx, cancel := context.WithTimeout(context.Background(), wrapTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = dir
	cmd.Stdin = bytes.NewReader(stdin)
	var out bytes.Buffer
	cmd.Stdout = &limitedBuffer{buf: &out, max: maxWrapped}
	cmd.Stderr = stderr
	// Its own process group, killed as one on timeout: a script or pipeline
	// forks children that the shell's death alone would leave running, and a
	// blocked, silent one never dies of the closed pipe either. Only the
	// timeout kills; a command that ends on its own keeps what it started.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return killGroup(cmd.Process.Pid) }
	cmd.WaitDelay = 200 * time.Millisecond
	err := cmd.Run()
	code := 0
	var ee *exec.ExitError
	switch {
	case errors.As(err, &ee):
		code = ee.ExitCode()
	case err != nil:
		code = 1
	}
	return strings.TrimRight(out.String(), "\r\n"), code
}

// killGroup SIGKILLs the process group led by pid. A group that is already
// gone is reported as done, so os/exec treats it like a process that exited.
func killGroup(pid int) error {
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	return nil
}

// limitedBuffer keeps at most max bytes and swallows the rest.
type limitedBuffer struct {
	buf *bytes.Buffer
	max int
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if room := l.max - l.buf.Len(); room > 0 {
		l.buf.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}
