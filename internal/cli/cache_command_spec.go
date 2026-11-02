package cli

// The command table entry of `heavyq cache` and its dispatch; the handler is in cache_command.go.
func init() {
	registerCommand(commandSpec{
		Name:     "cache",
		Order:    170,
		Config:   cfgLoaded,
		Run:      func(inv *invocation) int { return cacheCommand(inv.Cfg, inv.args(), inv.Stdout, inv.Stderr) },
		Summary:  "inspect or empty the result cache of --cache runs",
		Synopsis: []string{"heavyq [--config FILE] cache ls [--json] | stats [--json] | clear"},
		Subs: []subSpec{
			{Name: "ls", Summary: "list cached results", Flags: []flagSpec{jsonFlag("machine-readable output")}},
			{Name: "stats", Summary: "size, hit counts and limits", Flags: []flagSpec{jsonFlag("machine-readable output")}},
			{Name: "clear", Summary: "remove every cached result"},
		},
		Docs: "cache-and-coalescing.md",
	})
}
