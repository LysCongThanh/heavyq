package cli

// The command table entry of `heavyq statusline` and its dispatch; the handler is in statusline_command.go.
func init() {
	registerCommand(commandSpec{
		Name:  "statusline",
		Order: 85,
		Run: func(inv *invocation) int {
			return statuslineCommand(inv.args(), inv.Stdin, inv.Stdout, inv.Stderr)
		},
		Summary:  "heavyq's segment of Claude Code's status line: progress, what needs you, on track and when",
		Synopsis: []string{"heavyq statusline [--segment] [--wrap CMD] [--ascii] [--no-notify]"},
		Flags: []flagSpec{
			{Long: "segment", Help: "print only heavyq's segment (the default)"},
			{Long: "wrap", Value: "CMD", Help: "run your existing status-line command and append the segment to its last line"},
			{Long: "ascii", Help: "ASCII glyphs only"},
			{Long: "no-notify", Help: "do not push a notification on a state change"},
		},
		Notes: "Reads the status-line JSON on stdin and the flow state under the repository's common dir; never runs git and\n" +
			"never blocks. Prints nothing when there is nothing to say. NO_COLOR is honoured.",
		Docs: "statusline.md",
	})
}
