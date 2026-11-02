package flowview

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/forecast"
)

const sample = `{"schema":1,"repo":"/r","updated":"2026-11-02T14:00:00Z","goal":"ship it","run":null,
"items":{"total":8,"proven":5,"running":2,"blocked":1,"claimed_unproven":0,"open":0},
"needs_you":[{"item":"doc-7","lane":"docs","session":"s1","reason":"permission_prompt: Bash(npm run e2e)","since":"2026-11-02T13:50:00Z"}],
"constraint":{"state":"ci_wait","share":0.41,"significant":true},
"throughput":{"proven_per_hour":2.1,"p50_claim_to_proof_s":1800,"window_s":14400,"n":9},
"lanes":[{"lane":"api","state":"proving","item":"login-api","since":"2026-11-02T13:30:00Z"}],
"session":{"model":"m","agent_version":"2.1","rate_limits":{"five_hour_pct":61,"seven_day_pct":20}}}`

func writeState(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadContractSample(t *testing.T) {
	s, err := Read(writeState(t, sample))
	if err != nil {
		t.Fatal(err)
	}
	if s.Items.Total != 8 || s.Items.Proven != 5 || s.NeedCount() != 1 || s.AllProven() || s.Empty() {
		t.Errorf("counts wrong: %+v", s.Items)
	}
	if s.Constraint == nil || s.Constraint.State != "ci_wait" || !s.Constraint.Significant {
		t.Errorf("constraint: %+v", s.Constraint)
	}
	if s.RunInfo() != nil {
		t.Error("a null run must read as no run")
	}
	if s.Throughput.N != 9 || s.Session.RateLimits.FiveHourPct != 61 {
		t.Errorf("throughput/session: %+v %+v", s.Throughput, s.Session)
	}
}

func TestReadRejects(t *testing.T) {
	if _, err := Read(filepath.Join(t.TempDir(), "none.json")); err != ErrNoState {
		t.Errorf("missing file: %v, want ErrNoState", err)
	}
	for name, body := range map[string]string{"schema 2": `{"schema":2}`, "garbage": `{nope`, "empty": ``} {
		if _, err := Read(writeState(t, body)); err == nil || err == ErrNoState {
			t.Errorf("%s: err %v, want a parse or schema error", name, err)
		}
	}
}

func TestRunInfoToleratesUnknownShapes(t *testing.T) {
	run := `{"id":"r1","started":"2026-11-02T10:00:00Z","expected_end":"2026-11-02T16:00:00Z","goal":"g","items":8,"parallel":3}`
	s, err := Read(writeState(t, strings.Replace(sample, `"run":null`, `"run":`+run, 1)))
	if err != nil {
		t.Fatal(err)
	}
	r := s.RunInfo()
	if r == nil || r.ID != "r1" || r.ExpectedEnd.Hour() != 16 {
		t.Fatalf("run: %+v", r)
	}
	// A run whose field has another type is ignored; the state still reads.
	s, err = Read(writeState(t, strings.Replace(sample, `"run":null`, `"run":{"id":7}`, 1)))
	if err != nil || s.RunInfo() != nil {
		t.Errorf("odd run: err %v, info %+v", err, s.RunInfo())
	}
}

func TestForecastAdapter(t *testing.T) {
	s, _ := Read(writeState(t, sample))
	now := time.Date(2026, 11, 2, 14, 0, 0, 0, time.UTC)
	f := s.Forecast(now)
	// 3 remaining x 30 min / 2 running lanes = 45 min.
	if f.Status != forecast.OnTrack || f.ETA != now.Add(45*time.Minute) || f.Confidence != forecast.ConfMedium {
		t.Errorf("forecast: %+v", f)
	}
	s.Throughput = nil
	if f := s.Forecast(now); f.Status != forecast.Unknown {
		t.Errorf("no throughput must be unknown, got %+v", f)
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// The finder must agree with `git rev-parse --git-common-dir`, for the main
// checkout, a subdirectory and a linked work tree, without running git itself.
func TestCommonDirOfMatchesGit(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(base, "main")
	if err := os.MkdirAll(filepath.Join(main, "sub", "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, main, "init", "-q")
	git(t, main, "commit", "-q", "--allow-empty", "-m", "x")
	linked := filepath.Join(base, "linked")
	git(t, main, "worktree", "add", "-q", linked)

	want := filepath.Join(main, ".git")
	for _, dir := range []string{main, filepath.Join(main, "sub", "deep"), linked} {
		got, ok := CommonDirOf(dir)
		if !ok || got != want {
			t.Errorf("CommonDirOf(%s) = %q, %v; want %q", dir, got, ok, want)
		}
	}
	if _, ok := CommonDirOf(base); ok {
		t.Error("a directory outside any repository must not resolve")
	}
}
