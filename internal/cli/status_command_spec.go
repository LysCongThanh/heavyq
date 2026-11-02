package cli

// The command table entry of `heavyq status` and its dispatch; the handler is in status_command.go.
func init() {
	registerCommand(commandSpec{
		Name:     "status",
		Order:    20,
		Aliases:  []string{"--status"},
		Config:   cfgLoaded,
		Run:      func(inv *invocation) int { return statusCommand(inv.Cfg, inv.args(), inv.Stdout, inv.Stderr) },
		Summary:  "show what runs, what waits and why",
		Synopsis: []string{"heavyq [--config FILE] status [--json]"},
		Flags:    []flagSpec{jsonFlag("print the machine-readable snapshot (schema 1)")},
		Notes:    "Prints the story, the running and waiting tickets, machines with their slots, and CPU by lane.",
		Docs:     "queue.md",
	})
}
