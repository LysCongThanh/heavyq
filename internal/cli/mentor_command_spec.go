package cli

// The command table entry of `heavyq mentor` and its dispatch; the handler is in mentor_command.go.
func init() {
	registerCommand(commandSpec{
		Name:   "mentor",
		Order:  190,
		Config: cfgPathOnly,
		Run: func(inv *invocation) int {
			return mentorCommand(inv.CfgPath, inv.Explicit, inv.args(), inv.Stdout, inv.Stderr)
		},
		Summary: "stats and switches of the mentor (a note after a repeated identical failure)",
		Synopsis: []string{
			"heavyq [--config FILE] mentor stats [--json]",
			"heavyq [--config FILE] mentor mute <rule>",
			"heavyq [--config FILE] mentor unmute <rule>",
		},
		Subs: []subSpec{
			{Name: "stats", Summary: "each rule's two arms and its lift", Flags: []flagSpec{jsonFlag("machine-readable output")}},
			{Name: "mute", Args: "<rule>", Summary: "silence a rule"},
			{Name: "unmute", Args: "<rule>", Summary: "turn a muted rule back on"},
		},
		Notes: "The note goes to 80% of eligible events; the other 20% are logged and left silent, so the effect can be\n" +
			"measured. HEAVYQ_MENTOR=off silences the mentor for one process tree.",
		Docs: "mentor.md",
	})
}
