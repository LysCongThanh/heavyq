package cli

// The command table entry of `heavyq run end|ls|current` and its dispatch; the handler is in runspan_command.go.
// "run" is the priority form of a command (`heavyq 3 -- ...`) and is never spelled by name, so the run span
// commands are the entry "runspan" and answer to the word "run" as an alias.
func init() {
	registerCommand(commandSpec{
		Name:    "runspan",
		Order:   160,
		Aliases: []string{"run"},
		Config:  cfgNone,
		Run:     func(inv *invocation) int { return runSpanCommand(inv.args(), inv.Stdout, inv.Stderr) },
		Summary: "the run span `heavyq preflight` opens: end it, list them, show the current one",
		Synopsis: []string{
			"heavyq run end [<id>]",
			"heavyq run ls [--json]",
			"heavyq run current [--json]",
		},
		Subs: []subSpec{
			{Name: "end", Args: "[<id>]", Summary: "close the current run span (or the one given)"},
			{Name: "ls", Summary: "list the run spans, newest first", Flags: []flagSpec{jsonFlag("machine-readable output")}},
			{Name: "current", Summary: "the open run span, if any (exit 1 when none)", Flags: []flagSpec{jsonFlag("machine-readable output")}},
		},
		Notes: "A run span is the plan of one long run: id, goal, items, expected end. `heavyq preflight` opens it, readers such as\n" +
			"recap compare it with what happened. Exit: 0 ok, 1 none open, 2 usage.",
		Docs: "preflight.md",
	})
}
