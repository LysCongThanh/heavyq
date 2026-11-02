package cli

// jsonFlag is the --json switch most read commands share.
func jsonFlag(help string) flagSpec { return flagSpec{Long: "json", Help: help} }

// commandTable lists every command in the order of the usage text. Keep it in
// step with Main, the flag parsers and `usage`; the drift test says what is off.
var commandTable = append(append([]commandSpec{}, queueCommands...), moreCommands...)

// queueCommands: running, watching and controlling the queue, gates and items.
var queueCommands = []commandSpec{
	{
		Name:    "run",
		Summary: "run a command through the queue at a priority (1 first, 9 last)",
		Synopsis: []string{
			"heavyq [--config FILE] <prio 1-9> [flags] -- <command...>",
		},
		Flags: []flagSpec{
			{Long: "lane", Value: "NAME", Help: "label shown in status (default: $HEAVYQ_LANE or the directory name)"},
			{Long: "light", Help: "light job: may use light slots and jump ahead into them"},
			{Long: "max-wait", Value: "DUR", Help: "give up after DUR in the queue (exit 75); \"90\", \"90s\", \"5m\""},
			{Long: "remote", Value: "WHERE", Choices: []string{"local", "auto"}, Help: "local | auto | <remote name> (default: auto when safe)"},
			{Long: "needs-ram", Help: "place by free RAM only (ignore load)"},
			{Long: "repo", Value: "DIR", Path: "dir", Help: "repository to ship for remote runs (default: git root of cwd)"},
			{Long: "ref", Value: "REF", Help: "commit to run remotely (default: HEAD)"},
			{Long: "base-ref", Value: "R/B", Help: "also recreate refs/remotes/R/B remotely (e.g. origin/main)"},
			{Long: "with", Value: "REPO[@REF]", Path: "dir", Help: "sibling repo needed next to the main one (repeatable)"},
			{Long: "needs", Value: "POOL[:N],..", Help: "hold N values of named pools while it runs (local only)"},
			{Long: "cache", Help: "reuse the result of an identical run on the same clean commit"},
			{Long: "session", Value: "ID", Help: "who ran it, recorded in history (default: $HEAVYQ_SESSION)"},
			{Long: "item", Value: "ID", Help: "work item it belongs to (default: $HEAVYQ_ITEM)"},
			{Long: "gate", Value: "NAME", Help: "gate it is part of (ids: 1-80 characters from A-Za-z0-9._:-)"},
			{Long: "digest", Help: "end with a result digest (always on for agents)"},
			{Long: "verbose", Short: "v", Help: "explain cache decisions"},
		},
		Notes: "Lower priority numbers run first; equal priorities are FIFO. The command's exit code is heavyq's,\n" +
			"except 2 usage, 75 max-wait, 76 remote unreachable too long, 77 remote setup failed, 78 cancelled,\n" +
			"255 connection to the remote lost. In agent mode (CLAUDECODE, ... set) output is a queued line, a\n" +
			"running line and a digest; see `heavyq help agents`.",
		Docs: "queue.md",
	},
	{
		Name:     "status",
		Summary:  "show what runs, what waits and why",
		Synopsis: []string{"heavyq [--config FILE] status [--json]"},
		Flags:    []flagSpec{jsonFlag("print the machine-readable snapshot (schema 1)")},
		Notes:    "Prints the story, the running and waiting tickets, machines with their slots, and CPU by lane.",
		Docs:     "queue.md",
	},
	{
		Name:     "why",
		Summary:  "say in words why the queue is as it is, or why a lane waits",
		Synopsis: []string{"heavyq [--config FILE] why [lane]"},
		Notes:    "Without a lane: the story the dashboard shows. With a lane: its running and waiting jobs, the reason\nper machine and its last recorded run. A lane that is not queued is idle, not an error.",
		Docs:     "queue.md",
	},
	{
		Name:    "gate",
		Summary: "prove that this exact code passed the commands of a gate",
		Synopsis: []string{
			"heavyq [--config FILE] gate status [name] [--json]",
			"heavyq [--config FILE] gate run [name] [--parallel] [--keep-going] [--lane NAME]",
		},
		Subs: []subSpec{
			{Name: "status", Args: "[name]", Summary: "is every command proven on this exact code? (exit 0 yes, 1 no, 2 usage)",
				Flags: []flagSpec{jsonFlag("machine-readable output (schema in docs/gates.md)")}},
			{Name: "run", Args: "[name]", Summary: "run the unproven commands through the queue with --cache", Flags: []flagSpec{
				{Long: "parallel", Help: "start every unproven command at once (the queue still limits how many run)"},
				{Long: "keep-going", Help: "run the remaining commands after one fails"},
				{Long: "lane", Value: "NAME", Help: "lane name (default: $HEAVYQ_LANE or the repository directory name)"},
			}},
		},
		Notes: "name defaults to \"ready\", else to the only gate in heavyq.toml. Without a heavyq.toml, the gate \"ready\"\n" +
			"is detected from go.mod, package.json, pytest config or Cargo.toml; `heavyq init` writes it down.",
		Docs: "gates.md",
	},
	{
		Name:    "item",
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
	},
	{
		Name:     "init",
		Summary:  "write heavyq.toml (the gate) for this repository",
		Synopsis: []string{"heavyq init [--agents-md] [--force] [--print]"},
		Flags: []flagSpec{
			{Long: "agents-md", Help: "add the heavyq section to AGENTS.md (and CLAUDE.md if present)"},
			{Long: "force", Help: "overwrite an existing heavyq.toml"},
			{Long: "print", Help: "print heavyq.toml to stdout and write nothing"},
		},
		Notes: "Never overwrites an existing heavyq.toml without --force.",
		Docs:  "gates.md",
	},
	{
		Name:     "dash",
		Summary:  "serve the loopback web dashboard",
		Synopsis: []string{"heavyq [--config FILE] dash [--control] [--addr 127.0.0.1:0] [--open]"},
		Flags: []flagSpec{
			{Long: "addr", Value: "ADDR", Help: "listen address (loopback only; port 0 picks a free one)"},
			{Long: "control", Help: "allow cancel, priority changes and pause/resume from the page"},
			{Long: "interval", Value: "DUR", Help: "snapshot period of the live stream (at least 1s)"},
			{Long: "open", Help: "open the dashboard in the default browser"},
		},
		Docs: "dashboard.md",
	},
	{
		Name:     "top",
		Summary:  "monitor the queue in a terminal pane (a live screen, or one frame for tmux)",
		Synopsis: []string{"heavyq [--config FILE] top [--control] [--mode full|focus|lanes|machines|line] [--interval 2s] [--line] [--once]"},
		Flags: []flagSpec{
			{Long: "ascii", Help: "ASCII glyphs only (automatic for a non-UTF-8 locale and TERM=dumb)"},
			{Long: "bell", Help: "ring the terminal bell when something new needs you"},
			{Long: "control", Help: "allow cancel (inline y/n), priority and pause/resume from the keyboard"},
			{Long: "interval", Value: "DUR", Help: "snapshot period (at least 1s)"},
			{Long: "line", Help: "print the one-line strip once and exit (tmux status-right); same as --mode line --once"},
			{Long: "mode", Value: "MODE", Choices: []string{"full", "focus", "lanes", "machines", "line"},
				Help: "display mode (default: the last one you used, else full); m cycles at runtime"},
			{Long: "once", Help: "print one frame of the mode to stdout and exit"},
			{Long: "width", Value: "N", Help: "with --once or --line: the width to lay out for (default: the terminal's, else $COLUMNS)"},
		},
		Notes: "Keys: j k g G Tab move, Enter explains the selected lane, / filters, m changes the mode, r refreshes, y copies the\n" +
			"selected command, ? helps, q quits; with --control also c (cancel, asks y/n), + and - (priority), p (pause).\n" +
			"The last mode is remembered in $XDG_STATE_HOME/heavyq/top-mode.",
		Docs: "top.md",
	},
	{
		Name:     "cancel",
		Summary:  "cancel a ticket or a running heavyq process",
		Synopsis: []string{"heavyq cancel <ticket|pid>"},
		Notes:    "The cancelled run exits 78.",
		Docs:     "queue.md",
	},
	{
		Name:     "reprio",
		Summary:  "change the priority of a waiting ticket",
		Synopsis: []string{"heavyq reprio <ticket|pid> <1-9>"},
		Docs:     "queue.md",
	},
	{
		Name:     "pause",
		Summary:  "stop admitting new jobs on a machine",
		Synopsis: []string{"heavyq [--config FILE] pause <machine>"},
		Notes:    "Running jobs are not affected. `heavyq resume <machine>` undoes it.",
		Docs:     "queue.md",
	},
	{
		Name:     "resume",
		Summary:  "admit jobs on a paused machine again",
		Synopsis: []string{"heavyq [--config FILE] resume <machine>"},
		Docs:     "queue.md",
	},
}
