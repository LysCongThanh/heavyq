package preflight

import (
	"fmt"
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

// verdict of one command. The zero value is "would prompt": no allow rule can
// match it, and nothing built in covers it.
type verdict struct {
	ok        bool
	denied    bool   // a deny rule matches: definite
	uncertain bool   // the matcher cannot tell (see judgeSegment): a warning, not a block
	reason    string // why not, one clause
	rule      string // the rule that would fix it
}

// maxPromptFindings bounds each list; the rest are counted.
const maxPromptFindings = 8

// CheckPermissions: the commands the run needs would not prompt in Claude Code.
// A command blocks (NO-GO) only when the matcher is sure: a deny or ask rule
// matches it, or no allow rule (nor anything built in) can cover it. A command
// the matcher cannot decide, because Claude Code's behavior for that shape is
// not documented or was seen to differ from the documentation (see
// testdata/permission-corpus), "may prompt" as a warning.
func CheckPermissions(in PermInput) []Finding {
	var blocks, warns []Finding
	seen := map[string]bool{}
	var extraBlocks, extraWarns int
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
		subject := clipText(redact.Command(cmd), 60)
		switch {
		case v.denied:
			if len(blocks) >= maxPromptFindings {
				extraBlocks++
				continue
			}
			blocks = append(blocks, Finding{Severity: Block, Subject: subject,
				Text: fmt.Sprintf("`%s` is denied by permissions.deny: %s", subject, v.reason),
				Fix:  "edit " + in.Settings + ": remove the deny rule, or change the proof"})
		case v.uncertain:
			if len(warns) >= maxPromptFindings {
				extraWarns++
				continue
			}
			warns = append(warns, Finding{Severity: Warn, Subject: subject,
				Text: fmt.Sprintf("`%s` may prompt: %s", subject, v.reason),
				Fix:  fmt.Sprintf("edit %s: add \"%s\" to permissions.allow, or run it once under `claude -p --permission-prompts none` to see", in.Settings, v.rule)})
		default:
			if len(blocks) >= maxPromptFindings {
				extraBlocks++
				continue
			}
			blocks = append(blocks, Finding{Severity: Block, Subject: subject,
				Text: fmt.Sprintf("`%s` may prompt: %s", subject, v.reason),
				Fix:  fmt.Sprintf("edit %s: add \"%s\" to permissions.allow", in.Settings, v.rule)})
		}
	}
	out := blocks
	if extraBlocks > 0 {
		out = append(out, Finding{Severity: Block, Subject: "more",
			Text: fmt.Sprintf("%d more commands may prompt", extraBlocks), Fix: "edit " + in.Settings + ": allow them in permissions.allow"})
	}
	out = append(out, warns...)
	if extraWarns > 0 {
		out = append(out, Finding{Severity: Warn, Subject: "more unsure",
			Text: fmt.Sprintf("%d more commands may prompt (not sure)", extraWarns), Fix: "claude -p --permission-prompts none \"<prompt>\""})
	}
	if len(blocks)+len(warns) > 0 {
		out = append(out, Finding{Severity: Warn, Subject: "headless",
			Text: "an unattended `claude -p` run hangs on a prompt nobody answers; deny instead, so a missing rule is a failure you can read",
			Fix:  "claude -p --permission-prompts none \"<prompt>\""})
	}
	return out
}

// ruleFor is the allow rule that would cover command text: its first word (two
// for the usual "tool verb" commands), with ":*". A rewritten heavyq command
// is covered by its exact prefix up to the "--" instead, as docs/claude-code.md
// advises, never by a blanket "heavyq:*".
func ruleFor(cmd string) string {
	if w := strings.Fields(cmd); len(w) > 0 && wrappers[w[0]] {
		return "Bash(" + cmd + ")" // "sh -c:*" would allow anything: only the exact command
	}
	// heavyq, or its absolute path when the hook writes that (not on PATH).
	if pre, rest, ok := strings.Cut(cmd, " -- "); ok && isHeavyq(pre) {
		return "Bash(" + pre + " -- " + firstWords(rest, 2) + " *)"
	}
	return "Bash(" + firstWords(cmd, 2) + ":*)"
}

// isHeavyq reports whether the words before a rewrite's "--" start with heavyq.
func isHeavyq(pre string) bool {
	w := strings.Fields(pre)
	return len(w) > 1 && (w[0] == "heavyq" || strings.HasSuffix(w[0], "/heavyq"))
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
