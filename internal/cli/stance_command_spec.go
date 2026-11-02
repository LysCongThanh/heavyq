package cli

// The command table entry of `heavyq stance` and its dispatch; the handler is in stance_command.go.
func init() {
	registerCommand(commandSpec{
		Name:    "stance",
		Order:   200,
		Run:     func(inv *invocation) int { return stanceCommand(inv.args(), inv.Stdout, inv.Stderr) },
		Summary: "stances: ways of thinking, entered on evidence or a keyword, with trust and a decision record",
		Synopsis: []string{
			"heavyq stance ls [--json]",
			"heavyq stance show <name> [--json]",
			"heavyq stance enter <name> [--item ID] [--why TEXT]",
			"heavyq stance exit [--item ID] [--force]",
			"heavyq stance off",
			"heavyq stance status [--json]",
			"heavyq stance stats [--json]",
		},
		Subs: []subSpec{
			{Name: "ls", Summary: "list stances", Flags: []flagSpec{jsonFlag("machine-readable output")}},
			{Name: "show", Args: "<name>", Summary: "print one stance", Flags: []flagSpec{jsonFlag("machine-readable output")}},
			{Name: "enter", Args: "<name>", Summary: "put this lane into a stance and print its compact form", Flags: []flagSpec{
				{Long: "item", Value: "ID", Help: "item whose decision record applies (default: the lane's leased item)"},
				{Long: "why", Value: "TEXT", Help: "the reason, shown as \"why now\""}}},
			{Name: "exit", Summary: "leave the stance (a decision record may be required)", Flags: []flagSpec{
				{Long: "force", Help: "leave even though the exit condition is not met (logged)"}}},
			{Name: "off", Summary: "leave the stance and mute stances for this lane until the next enter"},
			{Name: "status", Summary: "the lane's stance, its decision record and the evidence now", Flags: []flagSpec{jsonFlag("machine-readable output")}},
			{Name: "stats", Summary: "time and runs to proof after each stance, against the lane's baseline", Flags: []flagSpec{jsonFlag("machine-readable output")}},
		},
		Notes: "Sources, highest precedence first: <repo>/.heavyq/stances/*.md (only once trusted with heavyq trust), the user's\n" +
			"stances directory, built-in. enter, exit, off, status and stats act on the lane of the current directory.\n" +
			"HEAVYQ_STANCES=off turns every stance off for a process.",
		Docs: "stances.md",
	})
}
