package cli

// The command table entry of `heavyq brief` and its dispatch; the handler is in brief_command.go.
func init() {
	registerCommand(commandSpec{
		Name:     "brief",
		Order:    140,
		Config:   cfgLoaded,
		Run:      func(inv *invocation) int { return briefCommand(inv.Cfg, inv.args(), inv.Stdout, inv.Stderr) },
		Summary:  "the thread of this repository for this lane, in at most 300 tokens",
		Synopsis: []string{"heavyq [--config FILE] brief [--json]"},
		Flags:    []flagSpec{jsonFlag("machine-readable output (schema in docs/steer.md)")},
		Notes: "The goal, the lane's items and their status, the next command, a red gate, items that need a human and one\n" +
			"machine-pressure line. The SessionStart hook injects it at startup, resume and after compaction.",
		Docs: "steer.md",
	})
}
