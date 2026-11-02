package preflight

import "strings"

// readOnlyness says whether Claude Code runs a command without asking.
type readOnlyness int

const (
	roNo    readOnlyness = iota // it asks (or an allow rule must cover it)
	roYes                       // built in as read-only: never asks
	roMaybe                     // Claude Code's set may include it, or its flags decide: unknown
)

// readOnly are commands Claude Code runs without asking (the permissions page
// lists ls, cat, echo, pwd, head, tail, grep, find, wc, which, diff, stat, du, cd
// and the read-only forms of git; the set is not configurable and "includes" more).
var readOnly = map[string]bool{
	"cd": true, "ls": true, "pwd": true, "cat": true, "head": true, "tail": true, "wc": true,
	"echo": true, "true": true, "false": true, "grep": true, "sort": true, "uniq": true,
	"which": true, "date": true, "whoami": true, "diff": true, "stat": true, "du": true,
	"find": true, "sed": true,
}

// maybeReadOnly are commands that are plausibly in Claude Code's undocumented
// remainder of the set: they are never reported as a definite prompt.
var maybeReadOnly = map[string]bool{
	"tree": true, "file": true, "basename": true, "dirname": true, "realpath": true, "readlink": true,
	"cut": true, "tr": true, "awk": true, "jq": true, "id": true, "uname": true, "hostname": true,
	"ps": true, "df": true, "free": true, "printenv": true, "uptime": true, "nproc": true,
	"test": true, "[": true, "sleep": true, "seq": true, "sha256sum": true, "md5sum": true, "less": true,
}

// readOnlyGit are git subcommands Claude Code runs without asking, and
// maybeReadOnlyGit the ones whose read-only forms it may also run.
var (
	readOnlyGit      = map[string]bool{"status": true, "diff": true, "log": true, "show": true}
	maybeReadOnlyGit = map[string]bool{"branch": true, "remote": true, "tag": true, "rev-parse": true, "ls-files": true,
		"blame": true, "describe": true, "shortlog": true, "grep": true, "ls-tree": true, "cat-file": true, "config": true}
)

// findActions make find run commands or write files: it always asks for them.
var findActions = map[string]bool{"-exec": true, "-execdir": true, "-ok": true, "-okdir": true, "-delete": true,
	"-fprint": true, "-fprint0": true, "-fprintf": true, "-fls": true, "-files0-from": true}

// readOnlyKind classifies one simple command (wrappers and assignments already stripped).
func readOnlyKind(cmd string) readOnlyness {
	w := shellFields(cmd)
	if len(w) == 0 {
		return roYes
	}
	args := w[1:]
	switch prog := w[0]; {
	case prog == "git":
		if len(args) == 0 {
			return roNo
		}
		switch {
		case readOnlyGit[args[0]] && !hasPrefixAny(args, "--output"):
			return withGlobCaution(args, roYes)
		case maybeReadOnlyGit[args[0]]:
			return roMaybe
		}
		return roNo
	case prog == "find":
		for _, a := range args {
			if findActions[a] {
				return roNo
			}
		}
		return withGlobCaution(args, roYes)
	case prog == "sed" && (hasPrefixAny(args, "-i", "--in-place") || hasShortFlag(args, 'i')):
		return roNo
	case prog == "sort" && (hasPrefixAny(args, "-o", "--output")):
		return roNo
	case prog == "sed" || prog == "sort":
		return withGlobCaution(args, roYes)
	case readOnly[prog]:
		return roYes
	case maybeReadOnly[prog]:
		return roMaybe
	}
	return roNo
}

// withGlobCaution: an unquoted glob next to a write-capable flag may expand to
// that flag, so Claude Code asks, but the exact rule is not modelled here.
func withGlobCaution(args []string, k readOnlyness) readOnlyness {
	for _, a := range args {
		if !strings.HasPrefix(a, "'") && !strings.HasPrefix(a, `"`) && strings.ContainsAny(a, "*?[") {
			return roMaybe
		}
	}
	return k
}

func hasPrefixAny(args []string, prefixes ...string) bool {
	for _, a := range args {
		for _, p := range prefixes {
			if strings.HasPrefix(a, p) {
				return true
			}
		}
	}
	return false
}

// hasShortFlag reports whether a cluster of short flags such as -ni contains c.
func hasShortFlag(args []string, c byte) bool {
	for _, a := range args {
		if len(a) > 1 && a[0] == '-' && a[1] != '-' && strings.IndexByte(a, c) > 0 {
			return true
		}
	}
	return false
}
