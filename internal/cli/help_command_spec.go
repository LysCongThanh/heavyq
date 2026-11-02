package cli

// The command table entry of `heavyq help` and its dispatch; the handler is in help_command.go.
func init() {
	registerCommand(commandSpec{
		Name:         "help",
		Order:        240,
		Aliases:      []string{"-h", "--help"},
		Run:          func(inv *invocation) int { return helpCommand(inv.args(), inv.Stdout, inv.Stderr) },
		Summary:      "show the usage, the help of one command, or the agent card",
		Synopsis:     []string{"heavyq help [agents|<command>]"},
		NextWord:     []string{"agents"},
		NextCommands: true,
	})
}
