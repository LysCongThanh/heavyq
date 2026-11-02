package cli

// The command table entry of `heavyq init` and its dispatch; the handler is in init_command.go.
func init() {
	registerCommand(commandSpec{
		Name:     "init",
		Order:    60,
		Run:      func(inv *invocation) int { return initCommand(inv.args(), inv.Stdout, inv.Stderr) },
		Summary:  "write heavyq.toml (the gate) for this repository",
		Synopsis: []string{"heavyq init [--agents-md] [--force] [--print]"},
		Flags: []flagSpec{
			{Long: "agents-md", Help: "add the heavyq section to AGENTS.md (and CLAUDE.md if present)"},
			{Long: "force", Help: "overwrite an existing heavyq.toml"},
			{Long: "print", Help: "print heavyq.toml to stdout and write nothing"},
		},
		Notes: "Never overwrites an existing heavyq.toml without --force.",
		Docs:  "gates.md",
	})
}
