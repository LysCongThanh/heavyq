package cli

// The command table entry of `heavyq fleet` and its dispatch; the handler is in fleet_command.go.
func init() {
	registerCommand(commandSpec{
		Name:     "fleet",
		Order:    82,
		Config:   cfgLoaded,
		Run:      func(inv *invocation) int { return fleetCommand(inv.Cfg, inv.args(), inv.Stdout, inv.Stderr) },
		Summary:  "observe every machine and every queue, foreign ones included: overload, waits, red runs, worker caps",
		Synopsis: []string{"heavyq [--config FILE] fleet [--since DUR] [--timeout DUR] [--json] [--md FILE] [--agent]"},
		Flags: []flagSpec{
			{Long: "since", Value: "DUR", Help: "history window, e.g. 6h or 168h (default 24h)"},
			{Long: "timeout", Value: "DUR", Help: "per-machine probe timeout; a machine slower than this is unreachable (default 10s)"},
			jsonFlag("machine-readable output (schema/fleet.schema.json)"),
			{Long: "md", Value: "FILE", Path: "file", Help: "write a markdown report to FILE atomically (tmp + rename); stdout stays quiet"},
			{Long: "agent", Help: "a compact digest under 4 KB (automatic in agent mode)"},
		},
		Notes: "Read-only and daemonless: it probes local, every [[remote]] and every observe_only remote in parallel over\n" +
			"their transport, and reads each [[foreign]] queue's history tail, tickets and machine files. Run it from cron or\n" +
			"a systemd user timer every 10 minutes with --md to keep a report other agents can read.",
		Docs: "fleet.md",
	})
}
