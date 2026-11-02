package cli

// The command table entry of `heavyq trust` and its dispatch; the handler is in trust_command.go.
func init() {
	registerCommand(commandSpec{
		Name:     "trust",
		Order:    210,
		Run:      func(inv *invocation) int { return trustCommand(inv.args(), inv.Stdout, inv.Stderr) },
		Summary:  "trust this repository's own stances (a content hash; an edit asks again)",
		Synopsis: []string{"heavyq trust [--list|--revoke]"},
		Flags: []flagSpec{
			{Long: "list", Help: "list trusted repositories"},
			{Long: "revoke", Help: "forget this repository"},
		},
		Notes: "Stances in .heavyq/stances are listed but never injected until you run this from your own terminal\n" +
			"(it is refused under a coding agent). Your own and the built-in stances need no trust.",
		Docs: "stances.md",
	})
}
