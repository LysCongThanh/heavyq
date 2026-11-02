package cli

// The command table entry of `heavyq recap` and its dispatch; the handler is in recap_command.go.
func init() {
	registerCommand(commandSpec{
		Name:    "recap",
		Order:   25,
		Config:  cfgLoaded,
		Run:     func(inv *invocation) int { return recapCommand(inv.Cfg, inv.args(), inv.Stdout, inv.Stderr) },
		Summary: "how it is going and what landed: progress, forecast, the one best action, and every line's command",
		Synopsis: []string{
			"heavyq [--config FILE] recap [--since DUR] [--session S] [--run ID] [--json] [--verbose] [--ascii] [--no-notify]",
		},
		Flags: []flagSpec{
			{Long: "since", Value: "DUR", Help: "how far back to look, e.g. 90m or 2h (default: the run's start, else 4h)"},
			{Long: "session", Value: "S", Help: "only this session's items and runs"},
			{Long: "run", Value: "ID", Help: "the run declared by preflight: look back to its start"},
			jsonFlag("machine-readable output (schema in docs/recap.md)"),
			{Long: "verbose", Help: "also lanes, and the time of each line"},
			{Long: "ascii", Help: "ASCII glyphs and text only"},
			{Long: "no-notify", Help: "do not push a notification on a state change"},
		},
		Notes: "A header with progress, the forecast (on track or at risk, and when) and the constraint; the one best next\n" +
			"action; then merged-proven, proven, claimed-unproven, blocked, wasted (the same failure repeated) and in-flight.\n" +
			"Merged means reachable from the default branch: a squash merge reads \"proven\".",
		Docs: "recap.md",
	})
}
