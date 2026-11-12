// Package toolcheck decides which tools a run needs on the machine it runs on, and remembers which ones a
// remote has (#148): the command words a command line starts with, seen through env, sh -c and the gate
// wrapper (FromArgv); the names given with --tools or a gate's tools key (ParseList, CheckList); and the
// answers of each remote, cached in the state directory with a TTL (Cache). Asking a remote is
// internal/remote's ProbeTools; this package does no I/O but the cache file.
package toolcheck

import (
	"fmt"
	"regexp"
	"strings"
)

// MaxTools bounds the tools one run or gate may name.
const MaxTools = 32

// NamePattern is a plain command name: no path, no shell syntax, no leading '-' or '.' (also the schema's).
const NamePattern = `^[A-Za-z0-9_][A-Za-z0-9._+-]{0,63}$`

var nameRe = regexp.MustCompile(NamePattern)

// Common are the tools `heavyq doctor` lists for every remote. A probe asks for them as well, so one round
// trip also answers for the commands most runs start with.
var Common = []string{"go", "node", "npx", "pnpm", "python3", "cargo", "docker"}

// Valid reports whether name is a plain command name heavyq may ask a remote about.
func Valid(name string) bool { return nameRe.MatchString(name) }

// ParseList reads a --tools value: plain command names separated by commas. An empty entry or a name that
// is not plain is an error, never skipped; a name given twice counts once.
func ParseList(v string) ([]string, error) {
	parts := strings.Split(v, ",")
	if err := CheckList(parts); err != nil {
		return nil, err
	}
	return Union(parts), nil
}

// CheckList validates a list of tool names, such as a gate's tools key: each one a plain command name, and
// at most MaxTools different ones.
func CheckList(names []string) error {
	for i, n := range names {
		if !Valid(n) {
			return fmt.Errorf("entry %d %q is not a plain command name (want %s: no paths, spaces or shell syntax)", i+1, n, nameRe)
		}
	}
	if n := len(Union(names)); n > MaxTools {
		return fmt.Errorf("%d tools; at most %d", n, MaxTools)
	}
	return nil
}

// Union joins lists of names in order, each name once.
func Union(lists ...[]string) []string {
	var out []string
	seen := map[string]bool{}
	for _, l := range lists {
		for _, n := range l {
			if !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
	}
	return out
}

// Reason is the wait reason of a remote that lacks the tools missing: what `why` and `status` print for it.
func Reason(missing []string) string {
	return "lacks " + strings.Join(missing, ", ") + " (see its env_path)"
}
