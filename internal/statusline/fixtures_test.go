package statusline

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// now is the clock of every test: the state below was materialised 5 s before.
var now = time.Date(2026, 11, 2, 14, 0, 0, 0, time.UTC)

func init() { time.Local = time.UTC } // ETAs are clock times; pin the zone

// fx is a flow state fixture, in the shape of the #102 contract (schema 1).
type fx struct {
	proven, running, blocked int
	needs                    string // JSON array
	constraint               string // JSON object or null
	n                        int    // throughput samples
	run                      string // JSON object or null
	updated                  string
}

func (f fx) json() string {
	def := func(s, d string) string {
		if s == "" {
			return d
		}
		return s
	}
	upd := def(f.updated, "2026-11-02T13:59:55Z")
	return fmt.Sprintf(`{"schema":1,"repo":"/r","updated":%q,"goal":"ship","run":%s,
"items":{"total":8,"proven":%d,"running":%d,"blocked":%d,"claimed_unproven":0,"open":0},
"needs_you":%s,"constraint":%s,
"throughput":{"proven_per_hour":2.1,"p50_claim_to_proof_s":1800,"window_s":14400,"n":%d},
"lanes":[{"lane":"api","state":"proving","item":"login-api","since":"2026-11-02T13:30:00Z"}],
"session":null}`, upd, def(f.run, "null"), f.proven, f.running, f.blocked, def(f.needs, "[]"), def(f.constraint, "null"), f.n)
}

const (
	ciWait   = `{"state":"ci_wait","share":0.41,"significant":true}`
	oneNeed  = `[{"item":"doc-7","lane":"docs","session":"s1","reason":"permission_prompt: Bash(npm run e2e)","since":"2026-11-02T13:50:00Z"}]`
	lateRun  = `{"id":"r1","started":"2026-11-02T10:00:00Z","expected_end":"2026-11-02T14:30:00Z","goal":"ship","items":8,"parallel":2}`
	emptyRun = `{"schema":1,"repo":"/r","updated":"2026-11-02T13:59:55Z","goal":"","run":null,"items":{"total":0},"needs_you":[],"constraint":null,"throughput":null,"lanes":[],"session":null}`
)

// the scenarios of the golden segments.
var scenarios = []struct{ name, state string }{
	{"idle", fx{proven: 5, n: 9}.json()},
	{"busy", fx{proven: 5, running: 2, constraint: ciWait, n: 9}.json()},
	{"needs-you", fx{proven: 5, running: 2, blocked: 1, needs: oneNeed, constraint: ciWait, n: 9}.json()},
	{"at-risk", fx{proven: 5, running: 2, run: lateRun, n: 9}.json()},
	{"done", fx{proven: 8, n: 9}.json()},
	{"unknown", fx{proven: 1, n: 1}.json()},
	{"empty", emptyRun},
	{"no-state", ""},
}

// repoWith makes a real git repository with the given flow state (none when
// state is "") and returns its work tree and common dir.
func repoWith(t *testing.T, state string) (root, common string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	common = filepath.Join(root, ".git")
	if state != "" {
		p := filepath.Join(common, "heavyq", "flow", "state.json")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(state), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root, common
}
