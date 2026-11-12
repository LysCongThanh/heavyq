package cli

// The command table entry of `heavyq retarget` and its dispatch; the handler is retarget_command.go.
func init() {
	registerCommand(commandSpec{
		Name:   "retarget",
		Order:  105,
		Config: cfgLoaded,
		Run: func(inv *invocation) int {
			return retargetCommand(inv.Cfg, inv.args(), inv.Stdout, inv.Stderr)
		},
		Summary:  "move a waiting ticket to other machines (keeps its priority and place)",
		Synopsis: []string{"heavyq [--config FILE] retarget <ticket|pid> --remote WHERE"},
		Flags: []flagSpec{
			{Long: "remote", Value: "WHERE", Choices: []string{"local", "auto"}, Help: "local | auto | <remote name> | a,b (first that fits), as for a run"},
		},
		Notes: "The ticket waits for the new machines from its next poll, as if queued with --remote WHERE: a remote runs\n" +
			"the commit it recorded when queued, this machine runs the work tree in place. Exit 0 once it moved, 2 when the\n" +
			"move is refused (running ticket, unknown or paused machine, a ticket queued for this machine only).",
		Docs: "remote.md",
	})
}
