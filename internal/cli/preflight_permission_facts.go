package cli

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/hook"
	"github.com/LysCongThanh/heavyq/internal/preflight"
)

// heavyqItemCommand stands for what the agent types to work through items: the
// permission check wants it allowed too (`Bash(heavyq item:*)`).
const heavyqItemCommand = "heavyq item done --run"

// localSettingsFile is the file the permission fixes point to: per user, never committed.
const localSettingsFile = ".claude/settings.local.json"

// permissionResults runs the permission check. It is skipped when there is no
// sign of Claude Code (no settings file, not run from inside it), so a
// repository worked on by another agent is not told to edit settings it does
// not use.
func permissionResults(cfg *config.Config, root string, commands []string) preflight.Results {
	rules, found, problems := preflight.LoadRules(preflight.SettingsPaths(root, claudeConfigDir()))
	res := preflight.Results{Name: preflight.NamePermissions}
	if len(found) == 0 && len(problems) == 0 && os.Getenv("CLAUDECODE") == "" {
		res.Skipped = "no Claude Code settings found (.claude/settings.json, .claude/settings.local.json, ~/.claude/settings.json)"
		return res
	}
	for _, p := range problems {
		res.Found = append(res.Found, preflight.Finding{Severity: preflight.Warn, Subject: "settings",
			Text: "could not read " + oneLine(p, 120) + ": its rules are not counted", Fix: "edit " + localSettingsFile})
	}
	in := preflight.PermInput{Commands: append(append([]string{}, commands...), heavyqItemCommand),
		Rules: rules, Settings: localSettingsFile}
	if rules.HookInstalled && cfg.HookEnabled() {
		in.Rewrite = hookRewriter(cfg, root)
	}
	res.Found = append(res.Found, preflight.CheckPermissions(in)...)
	return res
}

// claudeConfigDir is $CLAUDE_CONFIG_DIR, else ~/.claude ("" when there is no home).
func claudeConfigDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude")
}

// hookRewriter asks the real hook decision (hook.Decide, the pure function
// behind `heavyq hook claude-pretooluse`) what it would do to a Bash command in
// work tree root, with this config's rules: the rewritten command that Claude
// Code's permission rules then see, and whether the hook approves it itself.
func hookRewriter(cfg *config.Config, root string) preflight.Rewriter {
	rules := append([]config.HookRule{}, cfg.Hook.Rules...)
	if cfg.HookDefaultRules() {
		rules = append(rules, hook.DefaultRules...)
	}
	bin := cfg.Hook.Binary
	if bin == "" {
		bin = "heavyq"
	}
	opts := hook.Options{Rules: rules, Permission: cfg.Hook.Permission, MaxWait: cfg.Hook.MaxWait.Duration,
		Binary: bin, Lane: os.Getenv("HEAVYQ_LANE"), LaneFor: worktreeLane}
	return func(cmd string) (string, bool, bool) {
		raw, err := json.Marshal(map[string]any{"hook_event_name": "PreToolUse", "tool_name": "Bash", "cwd": root,
			"tool_input": map[string]any{"command": cmd}})
		if err != nil || len(raw) > maxHookInput { // the hook never reads more than this from its stdin either
			return cmd, false, false
		}
		out, _, err := hook.Decide(raw, opts)
		if err != nil || out == nil {
			return cmd, false, false
		}
		var doc struct {
			Specific struct {
				Decision string                     `json:"permissionDecision"`
				Updated  map[string]json.RawMessage `json:"updatedInput"`
			} `json:"hookSpecificOutput"`
		}
		var rewritten string
		if json.Unmarshal(out, &doc) != nil || json.Unmarshal(doc.Specific.Updated["command"], &rewritten) != nil || rewritten == "" {
			return cmd, false, false
		}
		return rewritten, doc.Specific.Decision == "allow", true
	}
}
