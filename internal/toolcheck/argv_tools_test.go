package toolcheck

import (
	"reflect"
	"testing"
)

func TestFromArgvSeesThroughObviousWrappers(t *testing.T) {
	for _, c := range []struct {
		name string
		argv []string
		want []string
	}{
		{"plain", []string{"go", "test", "./..."}, []string{"go"}},
		{"a path is not a tool", []string{"./scripts/e2e.sh"}, nil},
		{"absolute env", []string{"/usr/bin/env", "FOO=1", "npx", "tsc"}, []string{"npx"}},
		{"env options", []string{"env", "-i", "A=1", "B=2", "node", "x.js"}, []string{"env", "node"}},
		{"env option with a value stops", []string{"env", "-u", "X", "go"}, []string{"env"}},
		{"sh -c", []string{"sh", "-c", "npx vitest run"}, []string{"sh", "npx"}},
		{"bash -euo pipefail -c", []string{"bash", "-euo", "pipefail", "-c", "pnpm test"}, []string{"bash", "pnpm"}},
		{"bash -lc with an assignment", []string{"bash", "-lc", "FOO=1 BAR=2 go test ./..."}, []string{"bash", "go"}},
		{"first command of a list", []string{"sh", "-c", "cd web && pnpm test"}, []string{"sh", "cd"}},
		{"env inside the script", []string{"sh", "-c", "env CI=1 cargo test; echo done"}, []string{"sh", "env", "cargo"}},
		{"a function definition is not a command", []string{"sh", "-c", "f() { x; }; f"}, []string{"sh"}},
		{"a quoted first word is not parsed", []string{"sh", "-c", "'my tool' arg"}, []string{"sh"}},
		{"an expansion is not parsed", []string{"sh", "-c", "$TOOL run"}, []string{"sh"}},
		{"a word glued to a quote is not parsed", []string{"sh", "-c", `go"x" test`}, []string{"sh"}},
		{"a redirect ends the word", []string{"sh", "-c", "psql<schema.sql"}, []string{"sh", "psql"}},
		{"a script file", []string{"bash", "scripts/e2e.sh"}, []string{"bash"}},
		{"shell without operands", []string{"sh", "-c"}, []string{"sh"}},
		{"env then a quote", []string{"sh", "-c", "env HEAVYQ_TEST_NORACE=1 go test -skip 'x' ./..."}, []string{"sh", "env", "go"}},
		{"a pipeline", []string{"sh", "-c", "gofmt -l . | (! grep .)"}, []string{"sh", "gofmt"}},
		{"a shell running a script", []string{"sh", "-c", "bash scripts/check.sh"}, []string{"sh", "bash"}},
		{"nice without options", []string{"nice", "make", "-j8"}, []string{"nice", "make"}},
		{"nice with options stops", []string{"nice", "-n", "5", "make"}, []string{"nice"}},
		{"duplicates once", []string{"sh", "-c", "sh -x"}, []string{"sh"}},
		{"not a name", []string{"$(evil)"}, nil},
		{"empty", nil, nil},
	} {
		if got := FromArgv(c.argv); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: FromArgv(%q) = %q, want %q", c.name, c.argv, got, c.want)
		}
	}
}
