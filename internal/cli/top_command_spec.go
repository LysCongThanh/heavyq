package cli

// The command table entry of `heavyq top` and its dispatch; the handler is in top_command.go.
func init() {
	registerCommand(commandSpec{
		Name:     "top",
		Order:    80,
		Config:   cfgLoaded,
		Run:      func(inv *invocation) int { return topCommand(inv.Cfg, inv.args(), inv.Stdout, inv.Stderr) },
		Summary:  "monitor the queue in a terminal pane (a live screen, or one frame for tmux)",
		Synopsis: []string{"heavyq [--config FILE] top [--control] [--mode full|focus|lanes|machines|line] [--interval 2s] [--line] [--once]"},
		Flags: []flagSpec{
			{Long: "ascii", Help: "ASCII glyphs only (automatic for a non-UTF-8 locale and TERM=dumb)"},
			{Long: "bell", Help: "ring the terminal bell when something new needs you"},
			{Long: "control", Help: "allow cancel (inline y/n), priority and pause/resume from the keyboard"},
			{Long: "interval", Value: "DUR", Help: "snapshot period (at least 1s)"},
			{Long: "line", Help: "print the one-line strip once and exit (tmux status-right); same as --mode line --once"},
			{Long: "mode", Value: "MODE", Choices: []string{"full", "focus", "lanes", "machines", "line"},
				Help: "display mode (default: the last one you used, else full); m cycles at runtime"},
			{Long: "once", Help: "print one frame of the mode to stdout and exit"},
			{Long: "width", Value: "N", Help: "with --once or --line: the width to lay out for (default: the terminal's, else $COLUMNS)"},
		},
		Notes: "Keys: j k g G Tab move, Enter explains the selected lane, / filters, m changes the mode, r refreshes, y copies the\n" +
			"selected command, ? helps, q quits; with --control also c (cancel, asks y/n), + and - (priority), p (pause).\n" +
			"The last mode is remembered in $XDG_STATE_HOME/heavyq/top-mode.",
		Docs: "top.md",
	})
}
