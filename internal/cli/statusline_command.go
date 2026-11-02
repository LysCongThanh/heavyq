package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"

	"github.com/LysCongThanh/heavyq/internal/statusline"
)

const statuslineUsage = `usage: heavyq statusline [--segment] [--wrap '<your status line command>'] [--ascii] [--no-notify]

Prints heavyq's segment for Claude Code's status line, from the materialised flow state and the status-line JSON on
stdin: "◆ 5/8 proven  ⚑ 1 needs you  ▲ CI wait  ✓ on track · ~16:40". It never runs git and never blocks (under 5 ms);
with nothing to say it prints nothing. --wrap runs your existing status-line command with the same stdin and appends
the segment to its last line. See docs/statusline.md.
`

// statuslineCommand implements `heavyq statusline`. It never fails the status
// line: a panic or a bad flag value leaves the line as it was.
func statuslineCommand(args []string, stdin io.Reader, stdout, stderr io.Writer) (rc int) {
	fs := flag.NewFlagSet("statusline", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, statuslineUsage) }
	fs.Bool("segment", false, "print only heavyq's segment (the default)")
	wrap := fs.String("wrap", "", "run this status-line command and append the segment to its last line")
	ascii := fs.Bool("ascii", false, "ASCII glyphs only")
	noNotify := fs.Bool("no-notify", false, "do not push a notification on a state change")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return ExitUsage
	}
	defer func() {
		if p := recover(); p != nil {
			rc = 0
		}
	}()
	return statusline.Run(statusline.Options{
		Stdin: stdin, Stdout: stdout, Stderr: stderr,
		Wrap: *wrap, ASCII: *ascii, NoNotify: *noNotify,
		Cwd: func() string { wd, _ := os.Getwd(); return wd },
		// `heavyq flow refresh` ships with the flow engine: where this binary has no
		// such command there is nothing to spawn.
		SpawnRefresh: spawnFlowRefresh(),
	})
}

// spawnFlowRefresh is the refresh starter, nil when this binary has no `flow`
// command. The process is detached (own session, no stdio): the status line
// must not wait for it, and its failure is nobody's problem here.
func spawnFlowRefresh() func(dir string) {
	if _, ok := commandByName("flow"); !ok {
		return nil
	}
	return func(dir string) {
		exe, err := os.Executable()
		if err != nil {
			return
		}
		cmd := exec.Command(exe, "flow", "refresh")
		cmd.Dir = dir
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if cmd.Start() != nil {
			return
		}
		go func() { _ = cmd.Wait() }()
	}
}
