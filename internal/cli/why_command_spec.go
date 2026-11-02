package cli

// The command table entry of `heavyq why` and its dispatch; the handler is in why_command.go.
func init() {
	registerCommand(commandSpec{
		Name:     "why",
		Order:    30,
		Config:   cfgLoaded,
		Run:      func(inv *invocation) int { return whyCommand(inv.Cfg, inv.args(), inv.Stdout, inv.Stderr) },
		Summary:  "say in words why the queue is as it is, or why a lane waits",
		Synopsis: []string{"heavyq [--config FILE] why [lane]"},
		Notes:    "Without a lane: the story the dashboard shows. With a lane: its running and waiting jobs, the reason\nper machine and its last recorded run. A lane that is not queued is idle, not an error.",
		Docs:     "queue.md",
	})
}
