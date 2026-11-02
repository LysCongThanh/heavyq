package cli

// The command table entry of `heavyq preflight` and its dispatch; the handler is in preflight_command.go.
func init() {
	registerCommand(commandSpec{
		Name:     "preflight",
		Order:    150,
		Config:   cfgLoaded,
		Run:      func(inv *invocation) int { return preflightCommand(inv.Cfg, inv.args(), inv.Stdout, inv.Stderr) },
		Summary:  "GO or NO-GO before an unattended run, with the questions to answer now",
		Synopsis: []string{"heavyq [--config FILE] preflight [--hours N] [--parallel N] [--start] [--json]"},
		Flags: []flagSpec{
			{Long: "hours", Value: "N", Help: "planned length of the run in hours (also the run span's expected end)"},
			{Long: "parallel", Value: "N", Help: "lanes planned (default: lanes holding leases, else 3)"},
			{Long: "start", Help: "open the run span even on NO-GO (a span is opened on GO anyway)"},
			jsonFlag("machine-readable output (schema in docs/preflight.md)"),
		},
		Notes: "Checks the anchor, dependencies, environment, Claude Code permissions and quota; every finding ends with the\n" +
			"command that fixes it. Exit: 0 GO, 1 NO-GO, 2 usage.",
		Docs: "preflight.md",
	})
}
