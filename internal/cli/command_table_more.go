package cli

// moreCommands: history, cache, hooks, stances and the commands about heavyq itself.
var moreCommands = []commandSpec{
	{
		Name:     "history",
		Summary:  "show the most recent runs of the ledger",
		Synopsis: []string{"heavyq history [-n N] [-v]"},
		Flags: []flagSpec{
			{Short: "n", Value: "N", Help: "number of runs to show (default 20)"},
			{Short: "v", Help: "also show the tree, session, item and gate of each run"},
		},
		Docs: "history.md",
	},
	{
		Name:     "brief",
		Summary:  "the thread of this repository for this lane, in at most 300 tokens",
		Synopsis: []string{"heavyq [--config FILE] brief [--json]"},
		Flags:    []flagSpec{jsonFlag("machine-readable output (schema in docs/steer.md)")},
		Notes: "The goal, the lane's items and their status, the next command, a red gate, items that need a human and one\n" +
			"machine-pressure line. The SessionStart hook injects it at startup, resume and after compaction.",
		Docs: "steer.md",
	},
	{
		Name:    "steer",
		Summary: "show or set the stop gate mode of this repository (off, guard, destroy)",
		Synopsis: []string{
			"heavyq steer",
			"heavyq steer off|guard|destroy [--apply] [--scope local|project|user]",
		},
		NextWord: []string{"off", "guard", "destroy"},
		Flags: []flagSpec{
			{Long: "apply", Help: "destroy: set the Claude Code Stop-hook block cap to 0 in the Claude Code settings"},
			{Long: "scope", Value: "SCOPE", Choices: []string{"local", "project", "user"}, Help: "with --apply: local, project or user settings"},
		},
		Notes: "off: no stop gate (default). guard: Stop blocks while this lane holds claimed items not proven on this tree.\n" +
			"destroy: no cap, and SubagentStop, TaskCompleted and TeammateIdle are gated too. HEAVYQ_STEER=off is the kill switch.",
		Docs: "steer.md",
	},
	{
		Name:     "receipts",
		Summary:  "summarise what heavyq did over a window of the ledger",
		Synopsis: []string{"heavyq receipts [--day|--week|--month] [--share] [--json]"},
		Flags: []flagSpec{
			{Long: "day", Help: "the last 24 hours"},
			{Long: "week", Help: "the last 7 days (default)"},
			{Long: "month", Help: "the last 30 days"},
			{Long: "share", Help: "round into ranges and drop every name: safe to post"},
			jsonFlag("machine-readable output"),
		},
		Docs: "receipts.md",
	},
	{
		Name:     "cache",
		Summary:  "inspect or empty the result cache of --cache runs",
		Synopsis: []string{"heavyq [--config FILE] cache ls [--json] | stats [--json] | clear"},
		Subs: []subSpec{
			{Name: "ls", Summary: "list cached results", Flags: []flagSpec{jsonFlag("machine-readable output")}},
			{Name: "stats", Summary: "size, hit counts and limits", Flags: []flagSpec{jsonFlag("machine-readable output")}},
			{Name: "clear", Summary: "remove every cached result"},
		},
		Docs: "cache-and-coalescing.md",
	},
	{
		Name:    "hook",
		Summary: "the Claude Code PreToolUse hook, and its installation",
		Synopsis: []string{
			"heavyq [--config FILE] hook claude-pretooluse",
			"heavyq [--config FILE] hook claude-posttooluse",
			"heavyq [--config FILE] hook claude-sessionstart",
			"heavyq [--config FILE] hook claude-userpromptsubmit",
			"heavyq [--config FILE] hook claude-stop [Stop|SubagentStop|TaskCompleted|TeammateIdle]",
			"heavyq [--config FILE] hook install claude [--scope local|project|user] [--print] [--force] [--features queue,brief,stop,stance]",
			"heavyq [--config FILE] hook uninstall claude [--scope local|project|user] [--print]",
		},
		Subs: []subSpec{
			{Name: "claude-pretooluse", Summary: "called by Claude Code; reads the hook JSON on stdin, fails open"},
			{Name: "claude-posttooluse", Summary: "the mentor: PostToolUse and PostToolUseFailure hook; reads JSON on stdin, fails open"},
			{Name: "claude-sessionstart", Summary: "SessionStart hook: injects the brief; called by Claude Code, fails open"},
			{Name: "claude-userpromptsubmit", Summary: "UserPromptSubmit hook: enters a stance on a keyword and injects it once; called by Claude Code, fails open"},
			{Name: "claude-stop", Args: "[event]", Summary: "the stop gate (Stop, SubagentStop, TaskCompleted, TeammateIdle); called by Claude Code, fails open",
				NextWord: []string{"Stop", "SubagentStop", "TaskCompleted", "TeammateIdle"}},
			{Name: "install", Args: "claude", Summary: "add the hook to Claude Code settings", NextWord: []string{"claude"}, Flags: []flagSpec{
				scopeFlag(), {Long: "print", Help: "print the resulting settings instead of writing them"},
				{Long: "force", Help: "install even when the heavyq Claude Code plugin is enabled (the hook would run twice)"},
				{Long: "features", Value: "LIST", Choices: []string{"queue", "brief", "stop", "stance", "queue,brief,stop,stance"},
					Help: "hooks to install, comma separated: queue, brief, stop, stance (exactly these; others of heavyq's are removed; default queue)"}}},
			{Name: "uninstall", Args: "claude", Summary: "remove the hook again", NextWord: []string{"claude"}, Flags: []flagSpec{
				scopeFlag(), {Long: "print", Help: "print the resulting settings instead of writing them"}}},
		},
		Notes: "The hook never blocks an agent: on any doubt it leaves the command unchanged and exits 0.",
		Docs:  "claude-code-hook.md",
	},
	{
		Name:    "mentor",
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
	},
	{
		Name:    "stance",
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
	},
	{
		Name:     "trust",
		Summary:  "trust this repository's own stances (a content hash; an edit asks again)",
		Synopsis: []string{"heavyq trust [--list|--revoke]"},
		Flags: []flagSpec{
			{Long: "list", Help: "list trusted repositories"},
			{Long: "revoke", Help: "forget this repository"},
		},
		Notes: "Stances in .heavyq/stances are listed but never injected until you run this from your own terminal\n" +
			"(it is refused under a coding agent). Your own and the built-in stances need no trust.",
		Docs: "stances.md",
	},
	{
		Name:     "doctor",
		Summary:  "check the ledger index and rebuild it",
		Synopsis: []string{"heavyq doctor [--reindex]"},
		Flags:    []flagSpec{{Long: "reindex", Help: "rebuild the ledger index from the ledger"}},
		Docs:     "ledger.md",
	},
	{
		Name:     "completion",
		Summary:  "print a shell completion script (bash, zsh or fish)",
		Synopsis: []string{"heavyq completion bash|zsh|fish"},
		NextWord: []string{"bash", "zsh", "fish"},
		Notes: "bash:  source <(heavyq completion bash)         # or write it to a bash-completion directory\n" +
			"zsh:   heavyq completion zsh > \"${fpath[1]}/_heavyq\"\n" +
			"fish:  heavyq completion fish > ~/.config/fish/completions/heavyq.fish",
		Docs: "install.md",
	},
	{
		Name:         "help",
		Summary:      "show the usage, the help of one command, or the agent card",
		Synopsis:     []string{"heavyq help [agents|<command>]"},
		NextWord:     []string{"agents"},
		NextCommands: true,
	},
	{
		Name:     "version",
		Summary:  "print the version",
		Synopsis: []string{"heavyq version"},
	},
}

// scopeFlag is --scope of the hook installer.
func scopeFlag() flagSpec {
	return flagSpec{Long: "scope", Value: "SCOPE", Choices: []string{"local", "project", "user"},
		Help: "local (.claude/settings.local.json), project (.claude/settings.json) or user (~/.claude/settings.json)"}
}
