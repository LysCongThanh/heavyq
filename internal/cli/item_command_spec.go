package cli

// The command table entry of `heavyq item` and its dispatch; the handler is in item_command.go.
func init() {
	registerCommand(commandSpec{
		Name:    "item",
		Order:   50,
		Config:  cfgLoaded,
		Run:     func(inv *invocation) int { return itemCommand(inv.Cfg, inv.args(), inv.Stdout, inv.Stderr) },
		Summary: "claim, prove and finish work items of the anchor checklist",
		Synopsis: []string{
			"heavyq [--config FILE] item ls [--all] [--json]",
			"heavyq [--config FILE] item claim <id> | --next [--json]",
			"heavyq [--config FILE] item release <id> [--force]",
			"heavyq [--config FILE] item done <id> [--run] [--json]",
			"heavyq [--config FILE] item block <id> \"needs: <what a human must provide>\"",
		},
		Subs: []subSpec{
			{Name: "ls", Summary: "list the items nobody has finished", Flags: []flagSpec{
				{Long: "all", Help: "include items that are proven and ticked"},
				jsonFlag("machine-readable output (schema in docs/items.md)")}},
			{Name: "claim", Args: "<id>|--next", Summary: "lease an item to this lane (TTL 30 min, renewed by every run)", Flags: []flagSpec{
				{Long: "next", Help: "claim the first item nobody holds"}, jsonFlag("machine-readable output")}},
			{Name: "release", Args: "<id>", Summary: "give an item back", Flags: []flagSpec{
				{Long: "force", Help: "release it even if another lane holds it"}}},
			{Name: "done", Args: "<id>", Summary: "tick an item once its proof passes on this exact code", Flags: []flagSpec{
				{Long: "run", Help: "run the proof through the queue first"}, jsonFlag("machine-readable output")}},
			{Name: "block", Args: "<id> \"needs: ...\"", Summary: "record what is missing and free the item"},
		},
		Notes: "Items are the checklist lines of .heavyq/anchor.md. Exit: 0 ok, 1 refused (held, unproven, nothing to\nclaim), 2 usage.",
		Docs:  "items.md",
	})
}
