package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	xterm "golang.org/x/term"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/snapshot"
	"github.com/LysCongThanh/heavyq/internal/tui/app"
	"github.com/LysCongThanh/heavyq/internal/tui/render"
	"github.com/LysCongThanh/heavyq/internal/tui/term"
)

const (
	topMinInterval = time.Second
	lineWidth      = 200 // --line prints the whole strip; the status bar cuts it
	onceFallbackW  = 80
)

// topCommand is `heavyq top`: the queue in a terminal pane. It reads the
// snapshot `status --json` prints and the dashboard streams, and with
// --control sends the dashboard's requests through the same mailbox.
// --once and --line print one frame and exit, for scripts and tmux.
func topCommand(cfg *config.Config, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("top", flag.ContinueOnError)
	fs.SetOutput(stderr)
	interval := fs.Duration("interval", 2*time.Second, "snapshot period")
	ctl := fs.Bool("control", false, "allow cancel (y/n), priority and pause/resume from the keyboard")
	modeName := fs.String("mode", "", "display mode: "+strings.Join(render.ModeNames(), ", ")+" (default: the last one you used, else full)")
	line := fs.Bool("line", false, "print the one-line strip once and exit (tmux status-right); same as --mode line --once")
	once := fs.Bool("once", false, "print one frame of the mode to stdout and exit")
	ascii := fs.Bool("ascii", false, "ASCII glyphs only")
	bell := fs.Bool("bell", false, "ring the terminal bell when something new needs you")
	cols := fs.Int("width", 0, "with --once/--line: the width to lay out for (default: the terminal's, else $COLUMNS)")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if fs.NArg() > 0 || *interval < topMinInterval || *cols < 0 {
		fmt.Fprintln(stderr, "usage: heavyq [--config FILE] top [--control] [--mode "+strings.Join(render.ModeNames(), "|")+"] [--interval 2s] [--ascii] [--bell] [--line | --once [--width N]]")
		return ExitUsage
	}
	statePath := app.StatePath()
	mode, ok := topMode(*modeName, *line, statePath)
	if !ok {
		fmt.Fprintf(stderr, "heavyq: --mode must be one of %s\n", strings.Join(render.ModeNames(), ", "))
		return ExitUsage
	}
	theme := render.ThemeFromEnv(os.Getenv, *ascii)
	probes := newProbeCache(cfg.Queue.ProbeTimeout.Duration)
	build := func() (snapshot.Snapshot, error) { return buildSnapshot(cfg, probes, cpuSampleInterval) }

	if *line || *once {
		return topOnce(build, mode, theme, *line, *cols, stdout, stderr)
	}
	out, isFile := stdout.(*os.File)
	if !isFile || !xterm.IsTerminal(int(out.Fd())) || !xterm.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprintln(stderr, "heavyq: top needs a terminal on stdin and stdout; use `heavyq status`, `heavyq top --once` or `heavyq top --line` in a pipe")
		return ExitUsage
	}
	var actions app.Actions
	if *ctl {
		a, err := newTopActions(cfg)
		if err != nil {
			fmt.Fprintln(stderr, "heavyq:", err)
			return ExitUsage
		}
		actions = a
	}
	sess, err := term.Start(os.Stdin, out)
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return 1
	}
	defer sess.Restore() // also on a panic: the shell must get its terminal back
	err = app.Run(context.Background(), app.Config{
		Source: build, Why: topWhy, Control: actions, Interval: *interval, Mode: mode, Theme: theme, Bell: *bell,
		SaveMode: func(m render.Mode) { _ = app.SaveMode(statePath, m) }, // failing to remember is not an error
	}, os.Stdin, sess)
	sess.Restore()
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return 1
	}
	return 0
}

// topMode is --mode, else (for a live screen) the remembered one, else full.
func topMode(name string, line bool, statePath string) (render.Mode, bool) {
	switch {
	case line:
		return render.ModeLine, true
	case name != "":
		return render.ParseMode(name)
	}
	if m, ok := app.LoadMode(statePath); ok {
		return m, true
	}
	return render.ModeFull, true
}

// topOnce prints one frame: a snapshot, laid out for a width, without
// touching the terminal state. Colour only when stdout is a terminal.
func topOnce(build func() (snapshot.Snapshot, error), mode render.Mode, theme render.Theme, line bool,
	cols int, stdout, stderr io.Writer) int {
	s, err := build()
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return 1
	}
	f, isFile := stdout.(*os.File)
	tty := isFile && xterm.IsTerminal(int(f.Fd()))
	if !tty {
		theme.Level = render.LevelNone
	}
	if cols == 0 {
		cols = onceWidth(f, tty, line || mode == render.ModeLine)
	}
	now := time.Now()
	v := render.View{Now: now, Interval: 2 * time.Second, Fetched: now}
	for _, l := range render.Frame(&s, mode, v, render.Options{Cols: cols, Theme: theme}) {
		fmt.Fprintln(stdout, l)
	}
	return 0
}

// onceWidth is the width for a one-shot frame: the strip is not cut by us,
// a terminal is as wide as it says, else $COLUMNS, else 80.
func onceWidth(f *os.File, tty, line bool) int {
	if line {
		return lineWidth
	}
	if tty {
		if c, _, err := xterm.GetSize(int(f.Fd())); err == nil && term.SaneSize(c, 1) {
			return c
		}
	}
	if c, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && term.SaneSize(c, 1) {
		return c
	}
	return onceFallbackW
}
