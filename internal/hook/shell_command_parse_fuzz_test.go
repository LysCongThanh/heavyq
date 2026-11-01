package hook

import (
	"slices"
	"strings"
	"testing"
)

// maxFuzzInput bounds every fuzz input: the hook sees one agent-written
// command, and a bounded input keeps a single execution fast.
const maxFuzzInput = 64 << 10

// fuzzSeedCommands are the shapes the table tests pin: wrapped, left alone,
// and refused. They give the fuzzer real structure to mutate.
var fuzzSeedCommands = []string{
	"npm test", "npm run test", "pnpm run build", "yarn test --coverage",
	"go test -race -count=1 ./internal/...", "go build ./...", "pytest -x tests/",
	"python3 -m pytest -q", "cargo test --workspace", "make test", "./gradlew test",
	"npm run lint", "pnpm run typecheck", "  go test ./...  ",
	"go test -run 'TestA|TestB' ./...", `go test -run "TestA" ./...`, `"go" test ./...`,
	"cd api && npm test", "npm test 2>&1", "go test ./... 2>&1 | tail -50",
	"npm test | grep -v warn", "cd web && npm run build 2>&1 | head -n 40",
	"CI=1 npm test", `NODE_OPTIONS="--max-old-space-size=4096" npm run build`,
	"ls -la", "npm run test:e2e", "echo go test",
	"heavyq 4 -- npm test", "/usr/local/bin/heavyq 2 --light -- npm run lint",
	"cd api && heavyq 3 -- go test ./...", "HEAVYQ_ACTIVE=1 npm test",
	"npm test; echo done", "npm test && npm run build", "npm test || true", "npm test &",
	"go test $(go list ./...)", "go test `go list ./...`", "go test > out.txt",
	"npm test | tee log | tail", "(cd api && npm test)", "{ npm test; }", "npm test # comment",
	`go test -run "$T" ./...`, "go test 'unterminated", "npm test\nrm x", "cd a b && npm test",
	"FOO=1 cd api && npm test", "npm test | FOO=1 tail", "npm test 2>&1 extra",
	`go test "it's" 'a"b' ''`, `A="x y" B='' npm test`, "'A=1' npm test", "", " ", "\t",
}

// FuzzParse checks the parser never panics and that anything it accepts is
// stable: quoting the parsed argv and parsing again gives the same argv.
// Refusing the second parse is allowed only for a word with a single quote,
// which ShellQuote escapes with a backslash the parser rejects by design.
func FuzzParse(f *testing.F) {
	for _, s := range fuzzSeedCommands {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		if len(src) > maxFuzzInput {
			t.Skip("input over the fuzz bound")
		}
		p, err := Parse(src)
		if err != nil {
			return
		}
		if len(p.Words) == 0 || p.ProgAt < 0 || p.ProgAt >= len(src) || p.EnvStart > p.ProgAt {
			t.Fatalf("Parse(%q) accepted with bad shape: %+v", src, p)
		}
		var b strings.Builder
		for _, e := range p.Env {
			name, val, _ := strings.Cut(e, "=")
			b.WriteString(name + "=" + ShellQuote(val) + " ")
		}
		for i, w := range p.Words {
			q := ShellQuote(w)
			if i == 0 && assignRe.MatchString(q) {
				q = "'" + w + "'" // keep a program that looks like NAME=x a program
			}
			b.WriteString(q + " ")
		}
		again, err := Parse(b.String())
		if err != nil {
			if !strings.Contains(strings.Join(append(slices.Clone(p.Env), p.Words...), ""), "'") {
				t.Fatalf("Parse(%q) ok but re-quoted %q is refused: %v", src, b.String(), err)
			}
			return
		}
		if !slices.Equal(again.Env, p.Env) || !slices.Equal(again.Words, p.Words) {
			t.Fatalf("Parse(%q) argv %q %q\nre-parsed %q as %q %q", src, p.Env, p.Words, b.String(), again.Env, again.Words)
		}
	})
}
