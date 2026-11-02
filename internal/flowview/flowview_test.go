package flowview

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/flow"
	"github.com/LysCongThanh/heavyq/internal/flowstate"
	"github.com/LysCongThanh/heavyq/internal/forecast"
)

const sample = `{"schema":1,"repo":"/r","updated":"2026-11-02T14:00:00Z","goal":"ship it","run":null,
"items":{"total":8,"proven":5,"running":2,"blocked":1,"claimed_unproven":0,"open":0},
"needs_you":[{"item":"doc-7","lane":"docs","session":"s1","reason":"permission_prompt: Bash(npm run e2e)","since":"2026-11-02T13:50:00Z"}],
"constraint":{"state":"ci_wait","share":0.41,"significant":true},
"throughput":{"proven_per_hour":2.1,"p50_claim_to_proof_s":1800,"window_s":14400,"n":9},
"lanes":[{"lane":"api","state":"proving","item":"login-api","since":"2026-11-02T13:30:00Z"}],
"session":{"model":"m","agent_version":"2.1","rate_limits":{"five_hour_pct":61,"seven_day_pct":20}}}`

var now = time.Date(2026, 11, 2, 14, 0, 0, 0, time.UTC)

func readSample(t *testing.T, body string) *flow.Snapshot {
	t.Helper()
	p := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := flowstate.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestForecastAdapter(t *testing.T) {
	s := readSample(t, sample)
	f := Forecast(s, now)
	// 3 remaining x 30 min / 2 running lanes = 45 min.
	if f.Status != forecast.OnTrack || f.ETA != now.Add(45*time.Minute) || f.Confidence != forecast.ConfMedium {
		t.Errorf("forecast: %+v", f)
	}
	s.Throughput = flow.Throughput{}
	if f := Forecast(s, now); f.Status != forecast.Unknown {
		t.Errorf("no throughput must be unknown, got %+v", f)
	}
}

// throughput.last_proof is what makes at_risk-by-stall possible: lanes work, the
// median is 30 minutes and nothing was proven for over twice that.
func TestForecastUsesLastProofForStall(t *testing.T) {
	stalled := strings.Replace(sample, `"n":9}`, `"n":9,"last_proof":"2026-11-02T12:50:00Z"}`, 1)
	if f := Forecast(readSample(t, stalled), now); f.Status != forecast.AtRisk || !strings.Contains(f.Reason, "no proof for") {
		t.Errorf("70 minutes without a proof, median 30: %+v", f)
	}
	fresh := strings.Replace(sample, `"n":9}`, `"n":9,"last_proof":"2026-11-02T13:40:00Z"}`, 1)
	if f := Forecast(readSample(t, fresh), now); f.Status != forecast.OnTrack {
		t.Errorf("a proof 20 minutes ago is on track: %+v", f)
	}
	null := strings.Replace(sample, `"n":9}`, `"n":9,"last_proof":null}`, 1)
	if f := Forecast(readSample(t, null), now); f.Status != forecast.OnTrack {
		t.Errorf("a null last_proof cannot tell a stall: %+v", f)
	}
}

func TestForecastBoundsByTheRunsExpectedEnd(t *testing.T) {
	run := `"run":{"id":"r1","started":"2026-11-02T10:00:00Z","expected_end":"2026-11-02T14:20:00Z","goal":"g","items":8,"parallel":3}`
	s := readSample(t, strings.Replace(sample, `"run":null`, run, 1))
	if f := Forecast(s, now); f.Status != forecast.AtRisk {
		t.Errorf("45 minutes of work left, the run ends in 20: %+v", f)
	}
}

func TestNotifyFactsKeyAndRedactNeeds(t *testing.T) {
	facts := NotifyFacts(readSample(t, sample), now)
	if len(facts.Needs) != 1 || !strings.HasPrefix(facts.Needs[0].Text, "doc-7: permission_prompt") || facts.Proven != 5 || facts.Total != 8 {
		t.Errorf("%+v", facts)
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
