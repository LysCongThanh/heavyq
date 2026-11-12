package historytest

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The timing budgets (hook path, ledger tail, flow-state read, ...) only mean something on a quiet machine
// without the race detector. The fast gate (heavyq.toml) skips them with `-skip '<pattern>'` because its
// packages run in parallel, and CI runs them alone in a dedicated step with the same `-run '<pattern>'`.
// Every test the skip pattern hides must therefore (a) live in a package that dedicated step lists, or it
// never runs on a pull request, and (b) skip itself under -short and -race, or the race suite (gate `ready`)
// measures race-instrumented code next to every other package and fails on noise.

const repoRoot = "../../.."

var (
	fastSkipRe   = regexp.MustCompile(`go test [^"]*-skip '([^']+)'`)
	ciLatencyRe  = regexp.MustCompile(`(?m)^\s*run:\s*go test [^\n]*-run '([^']+)'([^\n]*)$`)
	testFuncRe   = regexp.MustCompile(`(?m)^func (Test\w+)\(t \*testing\.T\) \{\n((?:.*\n){0,3})`)
	raceGuardStr = "testing.Short() || historytest.RaceEnabled"
)

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// timingTest is a test the fast gate skips, with the package directory it lives in.
type timingTest struct{ name, dir, head string }

func skippedTimingTests(t *testing.T, skip *regexp.Regexp) []timingTest {
	t.Helper()
	var out []timingTest
	err := filepath.WalkDir(filepath.Join(repoRoot, "internal"), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range testFuncRe.FindAllStringSubmatch(string(b), -1) {
			if skip.MatchString(m[1]) {
				rel, _ := filepath.Rel(repoRoot, filepath.Dir(path))
				out = append(out, timingTest{name: m[1], dir: "./" + filepath.ToSlash(rel), head: m[2]})
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

func TestEveryTimingTestTheFastGateSkipsRunsInItsOwnCIStep(t *testing.T) {
	m := fastSkipRe.FindStringSubmatch(readRepoFile(t, "heavyq.toml"))
	if m == nil {
		t.Fatal("heavyq.toml: the fast gate has no `go test -skip '<pattern>'`")
	}
	pattern := m[1]
	var step []string
	for _, s := range ciLatencyRe.FindAllStringSubmatch(readRepoFile(t, ".github/workflows/ci.yml"), -1) {
		if s[1] == pattern {
			step = strings.Fields(s[2])
		}
	}
	if step == nil {
		t.Fatalf("ci.yml: no step runs `go test -run '%s'` (the fast gate's skip pattern)", pattern)
	}
	listed := map[string]bool{}
	for _, f := range step {
		listed[f] = true
	}
	tests := skippedTimingTests(t, regexp.MustCompile(pattern))
	if len(tests) == 0 {
		t.Fatalf("no test matches %q: the scan is broken", pattern)
	}
	for _, tt := range tests {
		if !listed[tt.dir] {
			t.Errorf("%s (%s) is skipped by the fast gate but the ci.yml latency step does not list %s: it never runs on a pull request", tt.name, tt.dir, tt.dir)
		}
	}
}

func TestEveryTimingTestTheFastGateSkipsSkipsItselfUnderRace(t *testing.T) {
	m := fastSkipRe.FindStringSubmatch(readRepoFile(t, "heavyq.toml"))
	if m == nil {
		t.Fatal("heavyq.toml: the fast gate has no `go test -skip '<pattern>'`")
	}
	for _, tt := range skippedTimingTests(t, regexp.MustCompile(m[1])) {
		if !strings.Contains(tt.head, raceGuardStr) {
			t.Errorf("%s (%s) is a timing test without `if %s { t.Skip(...) }` at its top: gate ready runs it under -race", tt.name, tt.dir, raceGuardStr)
		}
	}
}
