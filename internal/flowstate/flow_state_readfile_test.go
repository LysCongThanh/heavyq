package flowstate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The contract sample of docs/flow.md, as another tool would have written it.
const contractSample = `{"schema":1,"repo":"/r","updated":"2026-11-02T14:00:00Z","goal":"ship it","run":null,
"items":{"total":8,"proven":5,"running":2,"blocked":1,"claimed_unproven":0,"open":0},
"needs_you":[{"item":"doc-7","lane":"docs","session":"s1","reason":"permission_prompt: Bash(npm run e2e)","since":"2026-11-02T13:50:00Z"}],
"constraint":{"state":"ci_wait","share":0.41,"significant":true},
"throughput":{"proven_per_hour":2.1,"p50_claim_to_proof_s":1800,"window_s":14400,"n":9,"last_proof":"2026-11-02T13:30:00Z"},
"lanes":[{"lane":"api","state":"proving","item":"login-api","since":"2026-11-02T13:30:00Z"}],
"session":{"model":"m","agent_version":"2.1","rate_limits":{"five_hour_pct":61,"seven_day_pct":20}}}`

func writeBody(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadFileDecodesTheContractSample(t *testing.T) {
	s, err := ReadFile(writeBody(t, contractSample))
	if err != nil {
		t.Fatal(err)
	}
	if s.Items.Total != 8 || s.Items.Proven != 5 || s.NeedCount() != 1 || s.AllProven() || s.Empty() {
		t.Errorf("counts wrong: %+v", s.Items)
	}
	if s.Constraint.State != "ci_wait" || !s.Constraint.Significant {
		t.Errorf("constraint: %+v", s.Constraint)
	}
	if s.RunInfo() != nil {
		t.Error("a null run must read as no run")
	}
	if s.Throughput.N != 9 || s.Throughput.LastProof == nil || s.Session.RateLimits == nil || *s.Session.RateLimits.FiveHourPct != 61 {
		t.Errorf("throughput/session: %+v %+v", s.Throughput, s.Session)
	}
}

func TestReadFileRejectsWhatItCannotTrust(t *testing.T) {
	if _, err := ReadFile(filepath.Join(t.TempDir(), "none.json")); !errors.Is(err, ErrNoState) {
		t.Errorf("missing file: %v, want ErrNoState", err)
	}
	for name, body := range map[string]string{"schema 2": `{"schema":2}`, "garbage": `{nope`, "empty": ``} {
		if _, err := ReadFile(writeBody(t, body)); err == nil || errors.Is(err, ErrNoState) {
			t.Errorf("%s: err %v, want a parse or schema error", name, err)
		}
	}
	if _, err := ReadFile(writeBody(t, `{"schema":1,"goal":"`+strings.Repeat("x", maxStateBytes)+`"}`)); err == nil {
		t.Error("a state file over the size bound must be refused")
	}
}

// A run of another shape (another version wrote it) must not make the state unreadable.
func TestReadFileToleratesARunOfAnotherShape(t *testing.T) {
	s, err := ReadFile(writeBody(t, strings.Replace(contractSample, `"run":null`, `"run":{"id":7}`, 1)))
	if err != nil || s.RunInfo() != nil {
		t.Errorf("odd run: err %v, info %+v", err, s.RunInfo())
	}
	run := `{"id":"r1","started":"2026-11-02T10:00:00Z","expected_end":"2026-11-02T16:00:00Z","goal":"g","items":8,"parallel":3}`
	s, err = ReadFile(writeBody(t, strings.Replace(contractSample, `"run":null`, `"run":`+run, 1)))
	if err != nil {
		t.Fatal(err)
	}
	if r := s.RunInfo(); r == nil || r.ID != "r1" || r.ExpectedEnd == nil || r.ExpectedEnd.Hour() != 16 {
		t.Errorf("run: %+v", r)
	}
}
