package preflight

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/hook"
)

// Rules are the Bash permission rules of Claude Code settings, merged over
// every settings file that was found.
type Rules struct {
	Allow, Deny, Ask []string // the specifier inside Bash(...), "" for a bare "Bash"
	// Bypass: defaultMode is bypassPermissions, or a rule allows every Bash command.
	Bypass bool
	// HookInstalled: a settings file runs the heavyq PreToolUse hook (or enables the plugin).
	HookInstalled bool
}

// Merge adds the rules of one settings file. Anything that is not the shape
// Claude Code documents is ignored, so a strange file costs rules, not a crash:
// the checks then say "may prompt", the conservative answer.
func (r *Rules) Merge(settings []byte) error {
	var doc struct {
		Permissions struct {
			Allow       []string `json:"allow"`
			Deny        []string `json:"deny"`
			Ask         []string `json:"ask"`
			DefaultMode string   `json:"defaultMode"`
		} `json:"permissions"`
		Hooks json.RawMessage `json:"hooks"`
	}
	if err := json.Unmarshal(settings, &doc); err != nil {
		return err
	}
	add := func(dst *[]string, in []string) {
		for _, e := range in {
			if spec, ok := bashSpec(e); ok {
				*dst = append(*dst, spec)
			}
		}
	}
	add(&r.Allow, doc.Permissions.Allow)
	add(&r.Deny, doc.Permissions.Deny)
	add(&r.Ask, doc.Permissions.Ask)
	if doc.Permissions.DefaultMode == "bypassPermissions" {
		r.Bypass = true
	}
	var hooks map[string][]hookGroup
	_ = json.Unmarshal(doc.Hooks, &hooks) // a malformed hooks block only costs the hook detection
	for _, g := range hooks["PreToolUse"] {
		for _, h := range g.Hooks {
			if strings.Contains(h.Command, hook.Subcommand) {
				r.HookInstalled = true
			}
		}
	}
	if hook.PluginEnabled(settings) {
		r.HookInstalled = true
	}
	for _, s := range r.Allow {
		if s == "" || s == "*" {
			r.Bypass = true
		}
	}
	return nil
}

type hookGroup struct {
	Hooks []struct {
		Command string `json:"command"`
	} `json:"hooks"`
}

// bashSpec extracts the specifier of a "Bash" or "Bash(spec)" rule.
func bashSpec(rule string) (string, bool) {
	rule = strings.TrimSpace(rule)
	if rule == "Bash" {
		return "", true
	}
	if strings.HasPrefix(rule, "Bash(") && strings.HasSuffix(rule, ")") {
		return strings.TrimSpace(rule[len("Bash(") : len(rule)-1]), true
	}
	return "", false
}

// matchRule reports whether the rule specifier covers cmd, a single simple
// command. "git status:*" is a prefix on a word boundary (the command alone
// counts); any other "*" matches any text, spaces included; with no "*" the
// whole command must be equal.
func matchRule(spec, cmd string) bool {
	if spec == "" {
		return true
	}
	if pre, ok := strings.CutSuffix(spec, ":*"); ok {
		return cmd == pre || strings.HasPrefix(cmd, pre+" ")
	}
	if !strings.Contains(spec, "*") {
		return cmd == spec
	}
	parts := strings.Split(spec, "*")
	for i, p := range parts {
		parts[i] = regexp.QuoteMeta(p)
	}
	re, err := regexp.Compile(`^(?s)` + strings.Join(parts, ".*") + `$`)
	return err == nil && re.MatchString(cmd)
}

func anyMatch(specs []string, cmd string) bool {
	for _, s := range specs {
		if matchRule(s, cmd) {
			return true
		}
	}
	return false
}

// readOnly are commands Claude Code runs without asking. The list is short on
// purpose: a command missing here is reported as "may prompt".
var readOnly = map[string]bool{
	"cd": true, "ls": true, "pwd": true, "cat": true, "head": true, "tail": true, "wc": true,
	"echo": true, "true": true, "false": true, "grep": true, "sort": true, "uniq": true,
	"which": true, "date": true, "whoami": true, "diff": true,
}

// readOnlyGit are git subcommands Claude Code runs without asking.
var readOnlyGit = map[string]bool{"status": true, "diff": true, "log": true, "show": true}

func isReadOnly(cmd string) bool {
	w := strings.Fields(cmd)
	if len(w) == 0 {
		return true
	}
	if w[0] == "git" && len(w) > 1 {
		return readOnlyGit[w[1]]
	}
	return readOnly[w[0]]
}
