package preflight

import (
	"fmt"
	"slices"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/redact"
)

// judge evaluates one command the way Claude Code would: through the hook's
// rewrite first, then deny, ask and allow rules segment by segment. The rules it
// follows were checked against recorded Claude Code decisions
// (testdata/permission-corpus, TestPermissionMatcherCorpus).
func judge(cmd string, in PermInput) verdict {
	text, approved := cmd, false
	if in.Rewrite != nil {
		if rewritten, ok, wrapped := in.Rewrite(cmd); wrapped {
			text, approved = rewritten, ok
		}
	}
	segs, simple := splitSegments(text)
	if !simple {
		return judgeComplex(cmd, text, approved, in)
	}
	result := verdict{ok: true}
	for _, seg := range segs {
		v := judgeSegment(seg, text != cmd, approved, in)
		switch {
		case v.ok:
		case v.uncertain:
			if result.ok {
				result = v // the first doubt stays, but a later sure answer still wins
			}
		default:
			return v
		}
	}
	return result
}

// judgeComplex: a command whose parts cannot be told apart ($( ), backticks, a
// dangling &&) is covered only by an exact rule or the hook's own approval: "x:*"
// must not approve "x $(rm -rf ~)". Recorded: `echo $(whoami)` was refused under
// a Bash(echo:*) rule, and `echo $(date)` ran under an exact rule, so even a
// built-in read-only command asks when it substitutes.
func judgeComplex(cmd, text string, approved bool, in PermInput) verdict {
	if !anyMatch(in.Rules.Deny, text) && (approved || slices.Contains(in.Rules.Allow, text)) {
		return verdict{ok: true}
	}
	return verdict{reason: "its shell form is too complex to match against rules", rule: ruleFor(cmd)}
}

// judgeSegment evaluates one simple command. Definite answers: deny and ask
// rules (which match past wrappers and any leading assignment), an allow rule, a
// command that is built in as read-only, and everything none of those cover.
// Doubts, which warn: a leading assignment of a variable Claude Code may or may
// not strip, a command in the background, a subshell or a shell construct (all
// seen to differ from the documentation), and commands that may be read-only.
func judgeSegment(seg segment, rewritten, approved bool, in PermInput) verdict {
	n := normalize(seg.text)
	deny := []string{seg.text, n.denyText}
	switch {
	case anyMatchAny(in.Rules.Deny, deny):
		return verdict{denied: true, reason: clipText(redact.Command(seg.text), 50)}
	case anyMatchAny(in.Rules.Ask, deny):
		return verdict{reason: "an ask rule matches " + clipText(redact.Command(seg.text), 50), rule: ruleFor(seg.text)}
	case approved:
		return verdict{ok: true}
	case seg.fileRedirect && !in.Rules.EditAllowed:
		return verdict{reason: "it writes to " + clipText(redact.Command(seg.target), 40) + " with a redirection, which asks unless an Edit rule allows the file",
			rule: "Edit(" + editTarget(seg.target) + ")"}
	case seg.fileRedirect:
		return verdict{uncertain: true, reason: "it redirects output to a file and whether an Edit rule allows that is not checked", rule: "Edit(" + editTarget(seg.target) + ")"}
	}
	covered := anyMatchAny(in.Rules.Allow, []string{seg.text, n.allowText})
	if !covered {
		switch readOnlyKind(n.allowText) {
		case roYes:
			covered = true
		case roMaybe:
			return verdict{uncertain: true, reason: "it may be one of the commands Claude Code runs without asking", rule: ruleFor(seg.text)}
		}
	}
	if covered {
		switch {
		case seg.background:
			return verdict{uncertain: true, reason: "it runs in the background (&), which Claude Code was seen to ask about", rule: ruleFor(seg.text)}
		case seg.grouped && readOnlyKind(n.allowText) != roYes:
			return verdict{uncertain: true, reason: "it runs in a subshell ( ), which Claude Code was seen to ask about", rule: ruleFor(seg.text)}
		case seg.control && readOnlyKind(n.allowText) != roYes:
			return verdict{uncertain: true, reason: "it is inside a for/while/if construct, which Claude Code handles in ways that differ", rule: ruleFor(seg.text)}
		}
		return verdict{ok: true}
	}
	if n.unknownEnv && (anyMatch(in.Rules.Allow, n.denyText) || readOnlyKind(n.denyText) != roNo) {
		return unsureEnv(seg, n)
	}
	if rewritten {
		return verdict{reason: fmt.Sprintf("the heavyq hook rewrites it to `%s` and no allow rule covers that (or set hook.permission = \"allow\")", clipText(redact.Command(seg.text), 90)),
			rule: ruleFor(seg.text)}
	}
	return verdict{reason: "no allow rule covers it", rule: ruleFor(seg.text)}
}

// unsureEnv: an allow rule covers the command only without its leading
// assignment, and Claude Code strips the assignment of some variables only.
func unsureEnv(seg segment, n normalized) verdict {
	return verdict{uncertain: true, rule: ruleFor(n.denyText),
		reason: "its leading " + firstAssignment(seg.text) + " may not be stripped before the allow rule is matched"}
}

func firstAssignment(cmd string) string {
	if w := shellFields(cmd); len(w) > 0 {
		name, _, _ := strings.Cut(w[0], "=")
		return name + "= assignment"
	}
	return "assignment"
}

func editTarget(target string) string {
	if strings.HasPrefix(target, "/") || strings.HasPrefix(target, "~") || strings.HasPrefix(target, ".") {
		return target
	}
	return "./" + target
}

func anyMatchAny(specs, cmds []string) bool {
	for _, c := range cmds {
		if anyMatch(specs, c) {
			return true
		}
	}
	return false
}
