package cli

// The command table entry of `heavyq doctor` and its dispatch; the handler is in doctor_command.go.
func init() {
	registerCommand(commandSpec{
		Name:  "doctor",
		Order: 220,
		Run: func(inv *invocation) int {
			return doctorCommand(inv.CfgPath, inv.Explicit, inv.args(), inv.Stdout, inv.Stderr)
		},
		Summary:  "check the config, the ledger index and each remote's tools; rebuild the index",
		Synopsis: []string{"heavyq doctor [--reindex]"},
		Flags:    []flagSpec{{Long: "reindex", Help: "rebuild the ledger index from the ledger"}},
		Docs:     "ledger.md",
	})
}
