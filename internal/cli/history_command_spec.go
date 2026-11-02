package cli

// The command table entry of `heavyq history` and its dispatch; the handler is in history_command.go.
func init() {
	registerCommand(commandSpec{
		Name:     "history",
		Order:    130,
		Run:      func(inv *invocation) int { return historyCommand(inv.args(), inv.Stdout, inv.Stderr) },
		Summary:  "show the most recent runs of the ledger",
		Synopsis: []string{"heavyq history [-n N] [-v]"},
		Flags: []flagSpec{
			{Short: "n", Value: "N", Help: "number of runs to show (default 20)"},
			{Short: "v", Help: "also show the tree, session, item and gate of each run"},
		},
		Docs: "history.md",
	})
}
