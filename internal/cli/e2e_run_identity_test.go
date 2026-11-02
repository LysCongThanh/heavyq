package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/LysCongThanh/heavyq/internal/proof"
)

// identityConfig puts FEATURE_X on the cache.env allowlist: its value is part
// of the identity of a run (#46).
const identityConfig = baseConfig + "[cache]\nenv = [\"FEATURE_X\"]\n"

// oneCommandGate commits a heavyq.toml with a single-command gate "ready".
func oneCommandGate(t *testing.T, h *rig) {
	t.Helper()
	repo := gitRepo(t, h)
	toml := fmt.Sprintf("[[gate]]\nname = \"ready\"\nrun = ['echo run >> %s/count-a']\nprio = 3\n", h.root)
	if err := os.WriteFile(filepath.Join(repo, "heavyq.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, h, repo, "add", "heavyq.toml")
	gitIn(t, h, repo, "commit", "-q", "-m", "add gate")
}

// withEnv runs fn with kv added to the environment of the heavyq processes.
func (h *rig) withEnv(kv string, fn func()) {
	saved := h.env
	h.env = append(append([]string(nil), saved...), kv)
	defer func() { h.env = saved }()
	fn()
}

// The false-proof hole: a run under FEATURE_X=1 must not prove the gate when
// FEATURE_X is not set, and a run without it must not prove it with the
// variable set. Re-running under the other environment really runs (it is
// another run, so the cache does not answer either).
func TestE2ERunUnderAnotherEnvironmentDoesNotProveTheGate(t *testing.T) {
	for _, c := range []struct {
		name, ran, asked string
	}{
		{"passed with the variable, asked without", "FEATURE_X=1", "FEATURE_X_UNSET_PLACEHOLDER=1"},
		{"passed without, asked with the variable", "FEATURE_X_UNSET_PLACEHOLDER=1", "FEATURE_X=1"},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newRig(t, identityConfig)
			oneCommandGate(t, h)
			h.withEnv(c.ran, func() {
				if rc, out := h.run("gate", "run"); rc != 0 || h.runs("a") != 1 {
					t.Fatalf("first run rc=%d runs=%d\n%s", rc, h.runs("a"), out)
				}
				if rc, g := h.gateJSON(); rc != 0 || !g.Proven || h.lastRecord().RunID == "" {
					t.Fatalf("the same environment proves: rc=%d %+v", rc, g)
				}
			})
			h.withEnv(c.asked, func() {
				rc, g := h.gateJSON()
				if rc != 1 || g.Proven || g.Commands[0].State != proof.Missing {
					t.Fatalf("another environment must not be proven: rc=%d %+v", rc, g)
				}
				if rc, out := h.run("gate", "run"); rc != 0 || h.runs("a") != 2 {
					t.Fatalf("it must really run under the new environment: rc=%d runs=%d\n%s", rc, h.runs("a"), out)
				}
				if rc, g := h.gateJSON(); rc != 0 || !g.Proven {
					t.Fatalf("rc=%d %+v", rc, g)
				}
			})
		})
	}
}

// Every ledger record carries the same run_id for the same run, and a
// different one when the allowlisted environment differs.
func TestE2ELedgerRecordsRunID(t *testing.T) {
	h := newRig(t, identityConfig)
	gitRepo(t, h)
	run := func(env string) string {
		var id string
		h.withEnv(env, func() {
			if rc, out := h.run("4", "--", "true"); rc != 0 {
				t.Fatalf("rc=%d\n%s", rc, out)
			}
			r := h.lastRecord()
			if r.RunID == "" || r.CmdHash == "" || len(r.RunID) != 64 {
				t.Fatalf("record: %+v", r)
			}
			id = r.RunID
		})
		return id
	}
	a, b, c := run("FEATURE_X=1"), run("FEATURE_X=1"), run("FEATURE_X=2")
	if a != b || a == c {
		t.Fatalf("run ids: %s %s %s", a, b, c)
	}
}
