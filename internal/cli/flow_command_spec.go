package cli

// The command table entry of `heavyq flow` and its dispatch; the handler is in flow_command.go.
func init() {
	registerCommand(commandSpec{
		Name:    "flow",
		Order:   55,
		Config:  cfgLoaded,
		Run:     func(inv *invocation) int { return flowCommand(inv.Cfg, inv.args(), inv.Stdout, inv.Stderr) },
		Summary: "where work waits: needs-you, the constraint and throughput, from the materialised flow state",
		Synopsis: []string{
			"heavyq [--config FILE] flow show [--json]",
			"heavyq [--config FILE] flow refresh",
		},
		Subs: []subSpec{
			{Name: "show", Summary: "print the flow state of this repository (reads <git common dir>/heavyq/flow/state.json)", Flags: []flagSpec{
				jsonFlag("the state document itself (schema in docs/flow.md)")}},
			{Name: "refresh", Summary: "recompute the flow state now, ignoring the 60 s debounce"},
		},
		Notes: "The state is derived from the ledger, the leases and the hook events; nothing is declared. It is rewritten at the end of any\n" +
			"run and at the SessionStart, Stop, Notification, PermissionRequest and StopFailure hooks, at most once a minute (a needs-you\n" +
			"event writes at once). `show` only reads, in a few milliseconds; with no state yet it computes it once.\n" +
			"HEAVYQ_FLOW=off stops the automatic writes.",
		Docs: "flow.md",
	})
}
