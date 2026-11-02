package cli

// The command table entry of `heavyq hook` and its dispatch; the handler is in hook_command.go.
func init() {
	registerCommand(commandSpec{
		Name:   "hook",
		Order:  180,
		Config: cfgPathOnly,
		Run: func(inv *invocation) int {
			return hookCommand(inv.CfgPath, inv.Explicit, inv.args(), inv.Stdin, inv.Stdout, inv.Stderr)
		},
		Summary: "the Claude Code PreToolUse hook, and its installation",
		Synopsis: []string{
			"heavyq [--config FILE] hook claude-pretooluse",
			"heavyq [--config FILE] hook claude-posttooluse",
			"heavyq [--config FILE] hook claude-sessionstart",
			"heavyq [--config FILE] hook claude-userpromptsubmit",
			"heavyq [--config FILE] hook claude-stop [Stop|SubagentStop|TaskCompleted|TeammateIdle]",
			"heavyq [--config FILE] hook claude-flow",
			"heavyq [--config FILE] hook claude-sessionmeta",
			"heavyq [--config FILE] hook install claude [--scope local|project|user] [--print] [--force] [--features queue,brief,stop,stance,flow]",
			"heavyq [--config FILE] hook uninstall claude [--scope local|project|user] [--print]",
		},
		Subs: []subSpec{
			{Name: "claude-pretooluse", Summary: "called by Claude Code; reads the hook JSON on stdin, fails open"},
			{Name: "claude-posttooluse", Summary: "the mentor: PostToolUse and PostToolUseFailure hook; reads JSON on stdin, fails open"},
			{Name: "claude-sessionstart", Summary: "SessionStart hook: injects the brief; called by Claude Code, fails open"},
			{Name: "claude-userpromptsubmit", Summary: "UserPromptSubmit hook: enters a stance on a keyword and injects it once; called by Claude Code, fails open"},
			{Name: "claude-stop", Args: "[event]", Summary: "the stop gate (Stop, SubagentStop, TaskCompleted, TeammateIdle); called by Claude Code, fails open",
				NextWord: []string{"Stop", "SubagentStop", "TaskCompleted", "TeammateIdle"}},
			{Name: "claude-flow", Summary: "observer: records permission prompts, idle prompts, StopFailure and model switches for `heavyq flow`; never decides; called by Claude Code, fails open"},
			{Name: "claude-sessionmeta", Summary: "reads the status-line JSON on stdin and keeps the session's model, agent version and rate limits; prints nothing"},
			{Name: "install", Args: "claude", Summary: "add the hook to Claude Code settings", NextWord: []string{"claude"}, Flags: []flagSpec{
				scopeFlag(), {Long: "print", Help: "print the resulting settings instead of writing them"},
				{Long: "force", Help: "install even when the heavyq Claude Code plugin is enabled (the hook would run twice)"},
				{Long: "features", Value: "LIST", Choices: []string{"queue", "brief", "stop", "stance", "flow", "queue,brief,stop,stance,flow"},
					Help: "hooks to install, comma separated: queue, brief, stop, stance, flow (exactly these; others of heavyq's are removed; default queue)"}}},
			{Name: "uninstall", Args: "claude", Summary: "remove the hook again", NextWord: []string{"claude"}, Flags: []flagSpec{
				scopeFlag(), {Long: "print", Help: "print the resulting settings instead of writing them"}}},
		},
		Notes: "The hook never blocks an agent: on any doubt it leaves the command unchanged and exits 0.",
		Docs:  "claude-code-hook.md",
	})
}

// scopeFlag is --scope of the hook installer.
func scopeFlag() flagSpec {
	return flagSpec{Long: "scope", Value: "SCOPE", Choices: []string{"local", "project", "user"},
		Help: "local (.claude/settings.local.json), project (.claude/settings.json) or user (~/.claude/settings.json)"}
}
