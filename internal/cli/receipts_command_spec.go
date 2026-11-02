package cli

// The command table entry of `heavyq receipts` and its dispatch; the handler is in receipts_command.go.
func init() {
	registerCommand(commandSpec{
		Name:     "receipts",
		Order:    160,
		Run:      func(inv *invocation) int { return receiptsCommand(inv.args(), inv.Stdout, inv.Stderr) },
		Summary:  "summarise what heavyq did over a window of the ledger",
		Synopsis: []string{"heavyq receipts [--day|--week|--month] [--share] [--json]"},
		Flags: []flagSpec{
			{Long: "day", Help: "the last 24 hours"},
			{Long: "week", Help: "the last 7 days (default)"},
			{Long: "month", Help: "the last 30 days"},
			{Long: "share", Help: "round into ranges and drop every name: safe to post"},
			jsonFlag("machine-readable output"),
		},
		Docs: "receipts.md",
	})
}
