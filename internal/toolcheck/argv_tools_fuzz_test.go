package toolcheck

import (
	"reflect"
	"strings"
	"testing"
)

// FromArgv reads command lines heavyq does not control (an agent's command, through the hook), and
// ParseList reads --tools: whatever they get, they never panic, and every name they return is a plain
// command name, listed once.
func FuzzFromArgv(f *testing.F) {
	for _, s := range []string{"npx vitest run", "FOO=1 env -i A=b go test", "cd web && pnpm test", "f() { x; }; f",
		"'a b' c", "bash -euo pipefail -c 'x'", "nice -n 5 make", "psql<x", "go,node", ",", "a/b,c"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		for _, argv := range [][]string{{"sh", "-c", s}, strings.Fields(s), {"bash", "-lc", s, "x"}} {
			got := FromArgv(argv)
			assertNames(t, got)
			if again := FromArgv(argv); !reflect.DeepEqual(got, again) {
				t.Fatalf("FromArgv(%q) is not stable: %q then %q", argv, got, again)
			}
		}
		if names, err := ParseList(s); err == nil {
			assertNames(t, names)
			if len(names) == 0 {
				t.Fatalf("ParseList(%q) accepted no names", s)
			}
		}
	})
}

func assertNames(t *testing.T, names []string) {
	t.Helper()
	seen := map[string]bool{}
	for _, n := range names {
		if !Valid(n) || seen[n] {
			t.Fatalf("%q: %q is not a plain command name or is listed twice", names, n)
		}
		seen[n] = true
	}
}
