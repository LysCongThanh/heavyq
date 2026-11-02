package cli

// The command table entry of `heavyq steer` and its dispatch; the handler is in steer_command.go.
func init() {
	registerCommand(commandSpec{
		Name:    "steer",
		Order:   150,
		Run:     func(inv *invocation) int { return steerCommand(inv.args(), inv.Stdout, inv.Stderr) },
		Summary: "show or set the stop gate mode of this repository (off, guard, destroy)",
		Synopsis: []string{
			"heavyq steer",
			"heavyq steer off|guard|destroy [--apply] [--scope local|project|user]",
		},
		NextWord: []string{"off", "guard", "destroy"},
		Flags: []flagSpec{
			{Long: "apply", Help: "destroy: set the Claude Code Stop-hook block cap to 0 in the Claude Code settings"},
			{Long: "scope", Value: "SCOPE", Choices: []string{"local", "project", "user"}, Help: "with --apply: local, project or user settings"},
		},
		Notes: "off: no stop gate (default). guard: Stop blocks while this lane holds claimed items not proven on this tree.\n" +
			"destroy: no cap, and SubagentStop, TaskCompleted and TeammateIdle are gated too. HEAVYQ_STEER=off is the kill switch.",
		Docs: "steer.md",
	})
}
