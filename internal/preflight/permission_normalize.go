package preflight

import (
	"slices"
	"strings"
)

// Claude Code strips a fixed set of process wrappers and a leading assignment of
// some environment variables before it matches an allow rule, so `Bash(go test:*)`
// covers `timeout 30 go test ./...` and `NODE_ENV=test go test ./...`. Deny and ask
// rules match past every leading assignment. The wrapper list is documented
// (code.claude.com/docs/en/permissions, "Wrappers"); the list of safe variables
// is not, so only the ones seen to be stripped in testdata/permission-corpus are
// known, and any other assignment makes the answer uncertain.
var strippedWrappers = map[string]bool{"timeout": true, "time": true, "nice": true, "nohup": true, "stdbuf": true, "command": true, "builtin": true, "noglob": true}

// safeEnvVars are leading assignments seen to be stripped by Claude Code.
var safeEnvVars = []string{"NODE_ENV", "CI", "GOOS", "GOARCH", "CGO_ENABLED", "RUST_LOG", "NO_COLOR", "TZ", "LANG"}

// normalized is a simple command as the rules see it.
type normalized struct {
	allowText   string // wrappers and safe assignments stripped: what an allow rule matches
	denyText    string // wrappers and every assignment stripped: what a deny or ask rule matches
	unknownEnv  bool   // a leading assignment of a variable that is not known to be stripped
	hasWrappers bool   // allowText differs from the command by a stripped wrapper
}

// normalize strips wrappers and leading assignments from one simple command.
func normalize(cmd string) normalized {
	n := normalized{allowText: cmd, denyText: cmd}
	words := shellFields(cmd)
	allowAt, denyAt := 0, 0 // index of the first word that is kept
	safe := true            // every assignment so far is known to be stripped
	for i := 0; i < len(words); {
		w := words[i]
		switch {
		case isAssignment(w):
			name, _, _ := strings.Cut(w, "=")
			if !slices.Contains(safeEnvVars, name) {
				safe = false
				n.unknownEnv = true
			}
			i++
			denyAt = i
			if safe {
				allowAt = i
			}
		case strippedWrappers[w]:
			i = skipWrapperArgs(words, i)
			denyAt = i
			if safe {
				allowAt, n.hasWrappers = i, true
			}
		case w == "xargs" && i+1 < len(words) && !strings.HasPrefix(words[i+1], "-"):
			i++ // bare xargs only: "xargs -n1 grep" is an xargs command
			denyAt = i
			if safe {
				allowAt, n.hasWrappers = i, true
			}
		default:
			i = len(words)
		}
	}
	n.allowText = strings.Join(words[allowAt:], " ")
	n.denyText = strings.Join(words[denyAt:], " ")
	return n
}

// skipWrapperArgs returns the index of the first word after the wrapper at i and
// its own arguments: its flags, and for timeout the duration, for nice the value of -n.
func skipWrapperArgs(words []string, i int) int {
	wrapper := words[i]
	i++
	for i < len(words) && strings.HasPrefix(words[i], "-") {
		if wrapper == "nice" && words[i] == "-n" || wrapper == "timeout" && (words[i] == "-s" || words[i] == "-k") {
			i++ // the flag takes a value
		}
		i++
	}
	if wrapper == "timeout" && i < len(words) {
		i++ // the duration
	}
	return min(i, len(words))
}

// isAssignment reports whether w is NAME=value.
func isAssignment(w string) bool {
	name, _, ok := strings.Cut(w, "=")
	if !ok || name == "" {
		return false
	}
	for i, c := range name {
		if c != '_' && !(c >= 'A' && c <= 'Z') && !(c >= 'a' && c <= 'z') && !(i > 0 && c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}
