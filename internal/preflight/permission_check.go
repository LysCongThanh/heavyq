package preflight

import (
	"fmt"
	"slices"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/redact"
)

// Rewriter says what the heavyq PreToolUse hook does to cmd: the command Claude
// Code then evaluates the permission rules against, and whether the hook
// approves it itself (hook.permission = "allow"). wrapped is false when the
// hook leaves cmd alone.
type Rewriter func(cmd string) (rewritten string, approved, wrapped bool)

// PermInput is what CheckPermissions reads.
type PermInput struct {
	Commands []string // as the agent would type them: proofs, gate commands, heavyq's own
	Rules    Rules
	Rewrite  Rewriter // nil when the hook is not installed
	Settings string   // the file a fix should edit, for the text
}

// verdicts of one command.
type verdict struct {
	ok     bool
	denied bool
	reason string // why not, one clause
	rule   string // the allow rule that would fix it
}

// maxPromptFindings bounds the list; the rest are counted.
const maxPromptFindings = 8

// CheckPermissions: the commands the run needs would not prompt in Claude Code.
// A command that is not covered by an allow rule (or by the hook's own approval)
// "may prompt": the finding blocks, because a prompt nobody answers stalls the
// run. It is conservative on purpose, so a false alarm costs one allow rule.
func CheckPermissions(in PermInput) []Finding {
	var out []Finding
	seen := map[string]bool{}
	var extra int
	prompts := 0
	for _, cmd := range in.Commands {
		cmd = strings.TrimSpace(cmd)
		if cmd == "" || seen[cmd] || in.Rules.Bypass {
			continue
		}
		seen[cmd] = true
		v := judge(cmd, in)
		if v.ok {
			continue
		}
		if prompts++; prompts > maxPromptFindings {
			extra++
			continue
		}
		subject := clipText(redact.Command(cmd), 60)
		if v.denied {
			out = append(out, Finding{Severity: Block, Subject: subject,
				Text: fmt.Sprintf("`%s` is denied by permissions.deny: %s", subject, v.reason),
				Fix:  "edit " + in.Settings + ": remove the deny rule, or change the proof"})
			continue
		}
		out = append(out, Finding{Severity: Block, Subject: subject,
			Text: fmt.Sprintf("`%s` may prompt: %s", subject, v.reason),
			Fix:  fmt.Sprintf("edit %s: add \"%s\" to permissions.allow", in.Settings, v.rule)})
	}
	if extra > 0 {
		out = append(out, Finding{Severity: Block, Subject: "more",
			Text: fmt.Sprintf("%d more commands may prompt", extra), Fix: "edit " + in.Settings + ": allow them in permissions.allow"})
	}
	if prompts > 0 {
		out = append(out, Finding{Severity: Warn, Subject: "headless",
			Text: "an unattended `claude -p` run hangs on a prompt nobody answers; deny instead, so a missing rule is a failure you can read",
			Fix:  "claude -p --permission-prompts none \"<prompt>\""})
	}
	return out
}

// judge evaluates one command the way Claude Code would: through the hook's
// rewrite first, then deny, ask and allow rules segment by segment.
func judge(cmd string, in PermInput) verdict {
	text, approved := cmd, false
	if in.Rewrite != nil {
		if rewritten, ok, wrapped := in.Rewrite(cmd); wrapped {
			text, approved = rewritten, ok
		}
	}
	segs, simple := splitCommand(text)
	if !simple {
		// Only an exact rule (or the hook's own approval) can cover a command
		// whose parts cannot be told apart: "x:*" must not approve "x $(rm -rf ~)".
		if !anyMatch(in.Rules.Deny, text) && (approved || slices.Contains(in.Rules.Allow, text)) {
			return verdict{ok: true}
		}
		return verdict{reason: "its shell form is too complex to match against rules", rule: ruleFor(cmd)}
	}
	for _, seg := range segs {
		switch {
		case anyMatch(in.Rules.Deny, seg):
			return verdict{denied: true, reason: clipText(redact.Command(seg), 50)}
		case anyMatch(in.Rules.Ask, seg):
			return verdict{reason: "an ask rule matches " + clipText(redact.Command(seg), 50), rule: ruleFor(seg)}
		case approved, anyMatch(in.Rules.Allow, seg), isReadOnly(seg):
		default:
			if text != cmd {
				return verdict{reason: fmt.Sprintf("the heavyq hook rewrites it to `%s` and no allow rule covers that (or set hook.permission = \"allow\")", clipText(redact.Command(text), 90)),
					rule: ruleFor(seg)}
			}
			return verdict{reason: "no allow rule covers it", rule: ruleFor(seg)}
		}
	}
	return verdict{ok: true}
}

// ruleFor is the allow rule that would cover command text: its first word (two
// for the usual "tool verb" commands), with ":*". A rewritten heavyq command
// is covered by its exact prefix up to the "--" instead, as docs/claude-code.md
// advises, never by a blanket "heavyq:*".
func ruleFor(cmd string) string {
	if w := strings.Fields(cmd); len(w) > 0 && wrappers[w[0]] {
		return "Bash(" + cmd + ")" // "sh -c:*" would allow anything: only the exact command
	}
	if pre, rest, ok := strings.Cut(cmd, " -- "); ok && strings.HasPrefix(pre, "heavyq ") {
		return "Bash(" + pre + " -- " + firstWords(rest, 2) + " *)"
	}
	return "Bash(" + firstWords(cmd, 2) + ":*)"
}

// wrappers run other commands: a prefix rule on them allows whatever follows.
var wrappers = map[string]bool{"sh": true, "bash": true, "zsh": true, "env": true, "sudo": true, "eval": true, "exec": true, "xargs": true}

// firstWords is the first n words of s, fewer when the next one is a flag
// ("make -j" is a rule on "make", not on a flag).
func firstWords(s string, n int) string {
	w := strings.Fields(s)
	for i := 1; i < len(w) && i < n; i++ {
		if strings.HasPrefix(w[i], "-") {
			n = i
			break
		}
	}
	if len(w) > n {
		w = w[:n]
	}
	return strings.Join(w, " ")
}

func clipText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n-1], "") + "~"
}
