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
			"heavyq [--config FILE] hook install claude [--scope local|project|user] [--print] [--force]",
			"heavyq [--config FILE] hook uninstall claude [--scope local|project|user] [--print]",
		},
		Subs: []subSpec{
			{Name: "claude-pretooluse", Summary: "called by Claude Code; reads the hook JSON on stdin, fails open"},
			{Name: "install", Args: "claude", Summary: "add the hook to Claude Code settings", NextWord: []string{"claude"}, Flags: []flagSpec{
				scopeFlag(), {Long: "print", Help: "print the resulting settings instead of writing them"},
				{Long: "force", Help: "install even when the heavyq Claude Code plugin is enabled (the hook would run twice)"}}},
			{Name: "uninstall", Args: "claude", Summary: "remove the hook again", NextWord: []string{"claude"}, Flags: []flagSpec{
				scopeFlag(), {Long: "print", Help: "print the resulting settings instead of writing them"}}},
		},
		Notes: "The hook never blocks an agent: on any doubt it leaves the command unchanged and exits 0.",
		Docs:  "claude-code-hook.md",
	},
	{
		Name:    "stance",
		Summary: "list and show stances (short documents describing a way of thinking)",
		Synopsis: []string{
			"heavyq stance ls [--json]",
			"heavyq stance show <name> [--json]",
		},
		Subs: []subSpec{
			{Name: "ls", Summary: "list stances", Flags: []flagSpec{jsonFlag("machine-readable output")}},
			{Name: "show", Args: "<name>", Summary: "print one stance", Flags: []flagSpec{jsonFlag("machine-readable output")}},
		},
		Notes: "Sources, highest precedence first: <repo>/.heavyq/stances/*.md, the user's stances directory, built-in.",
		Docs:  "stances.md",
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
