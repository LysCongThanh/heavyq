package cli

// The command table entry of `heavyq run` and its dispatch; the handler is in run_command.go.
func init() {
	registerCommand(commandSpec{
		Name:    "run",
		Order:   10,
		Config:  cfgLoaded,
		Run:     func(inv *invocation) int { return runCommand(inv.Cfg, inv.Argv, inv.Stderr) },
		Summary: "run a command through the queue at a priority (1 first, 9 last)",
		Synopsis: []string{
			"heavyq [--config FILE] <prio 1-9> [flags] -- <command...>",
		},
		Flags: []flagSpec{
			{Long: "lane", Value: "NAME", Help: "label shown in status (default: $HEAVYQ_LANE or the directory name)"},
			{Long: "light", Help: "light job: may use light slots and jump ahead into them"},
			{Long: "max-wait", Value: "DUR", Help: "give up after DUR in the queue (exit 75); \"90\", \"90s\", \"5m\""},
			{Long: "remote", Value: "WHERE", Choices: []string{"local", "auto"}, Help: "local | auto | <remote name> | a,b (first that fits) (default: auto when safe)"},
			{Long: "needs-ram", Help: "place by free RAM only (ignore load)"},
			{Long: "repo", Value: "DIR", Path: "dir", Help: "repository to ship for remote runs (default: git root of cwd)"},
			{Long: "ref", Value: "REF", Help: "commit to run remotely (default: HEAD)"},
			{Long: "base-ref", Value: "R/B", Help: "recreate refs/remotes/R/B remotely (default: origin's default branch; \"none\" = no base ref)"},
			{Long: "with", Value: "REPO[@REF]", Path: "dir", Help: "sibling repo next to the main one, pinned to REF (default HEAD); remote runs get ../REPO (repeatable)"},
			{Long: "need-gh", Help: "the command uses the GitHub CLI login of this machine: run here only"},
			{Long: "needs", Value: "POOL[:N],..", Help: "hold N values of named pools while it runs (local only)"},
			{Long: "cache", Help: "reuse the result of an identical run on the same clean commit"},
			{Long: "session", Value: "ID", Help: "who ran it, recorded in history (default: $HEAVYQ_SESSION)"},
			{Long: "item", Value: "ID", Help: "work item it belongs to (default: $HEAVYQ_ITEM)"},
			{Long: "gate", Value: "NAME", Help: "gate it is part of (ids: 1-80 characters from A-Za-z0-9._:-)"},
			{Long: "strict", Help: "exit 127 (FAIL) when an exit 0 run printed a shell \"command not found\" line (always so for gates)"},
			{Long: "digest", Help: "end with a result digest (always on for agents)"},
			{Long: "verbose", Short: "v", Help: "explain cache decisions"},
		},
		Notes: "Lower priority numbers run first; equal priorities are FIFO. The command's exit code is heavyq's,\n" +
			"except 2 usage, 75 max-wait, 76 remote unreachable too long, 77 remote setup failed, 78 cancelled,\n" +
			"255 connection to the remote lost. In agent mode (CLAUDECODE, ... set) output is a queued line, a\n" +
			"running line and a digest; see `heavyq help agents`.",
		Docs: "queue.md",
	})
}
