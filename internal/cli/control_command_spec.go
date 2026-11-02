package cli

// The command table entries of cancel, reprio, pause and resume, and their dispatch; the handler is control_command.go.
func init() {
	registerCommand(commandSpec{
		Name:   "cancel",
		Order:  90,
		Config: cfgLoaded,
		Run: func(inv *invocation) int {
			return controlCommand(inv.Cfg, "cancel", inv.args(), inv.Stdout, inv.Stderr)
		},
		Summary:  "cancel a ticket or a running heavyq process",
		Synopsis: []string{"heavyq cancel <ticket|pid>"},
		Notes:    "The cancelled run exits 78.",
		Docs:     "queue.md",
	})
}

func init() {
	registerCommand(commandSpec{
		Name:   "reprio",
		Order:  100,
		Config: cfgLoaded,
		Run: func(inv *invocation) int {
			return controlCommand(inv.Cfg, "reprio", inv.args(), inv.Stdout, inv.Stderr)
		},
		Summary:  "change the priority of a waiting ticket",
		Synopsis: []string{"heavyq reprio <ticket|pid> <1-9>"},
		Docs:     "queue.md",
	})
}

func init() {
	registerCommand(commandSpec{
		Name:     "pause",
		Order:    110,
		Config:   cfgLoaded,
		Run:      func(inv *invocation) int { return controlCommand(inv.Cfg, "pause", inv.args(), inv.Stdout, inv.Stderr) },
		Summary:  "stop admitting new jobs on a machine",
		Synopsis: []string{"heavyq [--config FILE] pause <machine>"},
		Notes:    "Running jobs are not affected. `heavyq resume <machine>` undoes it.",
		Docs:     "queue.md",
	})
}

func init() {
	registerCommand(commandSpec{
		Name:   "resume",
		Order:  120,
		Config: cfgLoaded,
		Run: func(inv *invocation) int {
			return controlCommand(inv.Cfg, "resume", inv.args(), inv.Stdout, inv.Stderr)
		},
		Summary:  "admit jobs on a paused machine again",
		Synopsis: []string{"heavyq [--config FILE] resume <machine>"},
		Docs:     "queue.md",
	})
}
