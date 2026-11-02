package cli

// The command table entry of `heavyq dash` and its dispatch; the handler is in dash_command.go.
func init() {
	registerCommand(commandSpec{
		Name:     "dash",
		Order:    70,
		Config:   cfgLoaded,
		Run:      func(inv *invocation) int { return dashCommand(inv.Cfg, inv.args(), inv.Stdout, inv.Stderr) },
		Summary:  "serve the loopback web dashboard",
		Synopsis: []string{"heavyq [--config FILE] dash [--control] [--addr 127.0.0.1:0] [--open]"},
		Flags: []flagSpec{
			{Long: "addr", Value: "ADDR", Help: "listen address (loopback only; port 0 picks a free one)"},
			{Long: "control", Help: "allow cancel, priority changes and pause/resume from the page"},
			{Long: "interval", Value: "DUR", Help: "snapshot period of the live stream (at least 1s)"},
			{Long: "open", Help: "open the dashboard in the default browser"},
		},
		Docs: "dashboard.md",
	})
}
