package cli

// The command table entry of `heavyq gate` and its dispatch; the handler is in gate_command.go.
func init() {
	registerCommand(commandSpec{
		Name:    "gate",
		Order:   40,
		Config:  cfgLoaded,
		Run:     func(inv *invocation) int { return gateCommand(inv.Cfg, inv.args(), inv.Stdout, inv.Stderr) },
		Summary: "prove that this exact code passed the commands of a gate",
		Synopsis: []string{
			"heavyq [--config FILE] gate status [name] [--json]",
			"heavyq [--config FILE] gate run [name] [--parallel] [--keep-going] [--lane NAME]",
		},
		Subs: []subSpec{
			{Name: "status", Args: "[name]", Summary: "is every command proven on this exact code? (exit 0 yes, 1 no, 2 usage)",
				Flags: []flagSpec{jsonFlag("machine-readable output (schema in docs/gates.md)")}},
			{Name: "run", Args: "[name]", Summary: "run the unproven commands through the queue with --cache", Flags: []flagSpec{
				{Long: "parallel", Help: "start every unproven command at once (the queue still limits how many run)"},
				{Long: "keep-going", Help: "run the remaining commands after one fails"},
				{Long: "lane", Value: "NAME", Help: "lane name (default: $HEAVYQ_LANE or the repository directory name)"},
			}},
		},
		Notes: "name defaults to \"ready\", else to the only gate in heavyq.toml. Without a heavyq.toml, the gate \"ready\"\n" +
			"is detected from go.mod, package.json, pytest config or Cargo.toml; `heavyq init` writes it down.",
		Docs: "gates.md",
	})
}
