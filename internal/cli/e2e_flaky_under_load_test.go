package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/proof"
)

// calmConfig never calls a machine loaded: the RAM budget is 1 MB and the load
// limit is the highest the config allows, so a failure can only be "failed" or
// "flaky" whatever the test machine is doing.
const calmConfig = baseConfig + "job_mb = 1\nlight_mb = 1\nmax_load_ratio = 64\n"

// loadedConfig is the opposite for RAM only: the job budget is more RAM than
// any machine has, so a failed run always started with less free RAM than its
// budget (and the load limit is out of play, so the reason is always the RAM).
const loadedConfig = baseConfig + "job_mb = 100000000\nmax_load_ratio = 64\n"

// flagGateRepo commits a heavyq.toml whose gate "ready" has one command that
// fails exactly when <root>/fail-flag exists. The flag is outside the work
// tree, so flipping it changes the outcome without changing the tree.
func flagGateRepo(t *testing.T, h *rig) (repo, cmd string) {
	t.Helper()
	repo = gitRepo(t, h)
	cmd = fmt.Sprintf("test ! -e %s/fail-flag", h.root)
	toml := "[[gate]]\nname = \"ready\"\nrun = ['" + cmd + "']\n"
	if err := os.WriteFile(filepath.Join(repo, "heavyq.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, h, repo, "add", "heavyq.toml")
	gitIn(t, h, repo, "commit", "-q", "-m", "add gate")
	return repo, cmd
}

// runOfGateCommand is the newest ledger record of the gate command cmd.
func (h *rig) runOfGateCommand(cmd string) history.Record {
	h.t.Helper()
	hash := history.CmdHash(proof.Argv(cmd))
	recs := h.history()
	for i := len(recs) - 1; i >= 0; i-- {
		if recs[i].CmdHash == hash {
			return recs[i]
		}
	}
	h.t.Fatalf("no ledger record with the hash of %q in %d records", cmd, len(recs))
	return history.Record{}
}

func skipUnlessMeasurable(t *testing.T, r history.Record) {
	t.Helper()
	if r.Cores == 0 {
		t.Skipf("this machine cannot be measured (%s): no reading to classify", runtime.GOOS)
	}
}

// The same code passes, then fails: flaky, and not proven, whatever comes next.
func TestE2EGateSameTreePassThenFailIsFlaky(t *testing.T) {
	h := newRig(t, calmConfig)
	_, cmd := flagGateRepo(t, h)
	if rc, out := h.run("gate", "run"); rc != 0 {
		t.Fatalf("first gate run rc=%d\n%s", rc, out)
	}
	if rc, g := h.gateJSON(); rc != 0 || !g.Proven || g.Commands[0].Class != "" || g.Commands[0].Passes != 1 {
		t.Fatalf("proven before the failure: rc=%d %+v", rc, g)
	}
	h.touch("fail-flag")
	if rc, out := h.run("3", "--", "sh", "-c", cmd); rc != 1 {
		t.Fatalf("the direct run must fail: rc=%d\n%s", rc, out)
	}

	rc, g := h.gateJSON()
	c := g.Commands[0]
	if rc != 1 || g.Proven || c.State != proof.Failed || c.Class != proof.ClassFlaky || c.Passes != 1 || c.Fails != 1 ||
		c.Exit == nil || *c.Exit != 1 || !strings.Contains(c.Reason, "flaky: 1 pass / 1 fail") {
		t.Fatalf("after pass then fail: rc=%d %+v", rc, g)
	}
	rc, out := h.run("gate", "status")
	for _, want := range []string{"NOT PROVEN", "failed", "(exit 1; flaky: 1 pass / 1 fail on this tree)", "1 failed (1 flaky), 0 missing", "stays unproven"} {
		if rc != 1 || !strings.Contains(out, want) {
			t.Fatalf("gate status text lacks %q (rc=%d):\n%s", want, rc, out)
		}
	}
	if rc, out = h.run("why"); rc != 0 || !strings.Contains(out, "1 failed (1 flaky), 0 missing") {
		t.Fatalf("why (rc=%d):\n%s", rc, out)
	}

	// Running the gate again cannot clear it: the command passes (from the
	// result cache, so it is not counted as another pass), the tree stays flaky.
	rc, out = h.run("gate", "run")
	if rc != 1 || !strings.Contains(out, "flaky: 1 pass / 1 fail") {
		t.Fatalf("gate run on a flaky tree: rc=%d\n%s", rc, out)
	}
	if r := h.runOfGateCommand(cmd); r.Exit != 0 || r.Cache != "hit" {
		t.Fatalf("the rerun should be a cached pass: %+v", r)
	}
	if rc, g = h.gateJSON(); rc != 1 || g.Commands[0].Class != proof.ClassFlaky || g.Commands[0].Passes != 1 {
		t.Fatalf("still flaky, one pass: rc=%d %+v", rc, g.Commands[0])
	}

	// Another tree has no history: the same command is simply unproven there.
	h.edit(h.cwd, "from-commit\nedited\n")
	if rc, g = h.gateJSON(); rc != 1 || g.Commands[0].State != proof.Missing || g.Commands[0].Class != "" {
		t.Fatalf("another tree: rc=%d %+v", rc, g.Commands[0])
	}
}

// A failure whose run was let in past the resource gate by the anti-starvation
// bypass is under_load, and a clean rerun proves the tree.
func TestE2EGateForcedAdmissionFailureIsUnderLoad(t *testing.T) {
	h := newRig(t, `[queue]
poll = "50ms"
force_after = "300ms"
[local]
heavy_slots = 2
light_slots = 0
job_mb = 100000000
`)
	_, cmd := flagGateRepo(t, h)
	h.touch("fail-flag")
	hold := h.start("5", "--lane", "api", "--", "sh", "-c", h.gateFile("go"))
	h.running(1) // an idle machine always admits the first job
	// The resource gate refuses the second job (a job needs more RAM than
	// exists) until it is head of the line for force_after, then forces it in.
	rc, out := h.run("3", "--lane", "web", "--", "sh", "-c", cmd)
	if rc != 1 || !strings.Contains(out, "our turn") {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
	h.touch("go")
	hold.wait(t, 20*time.Second)
	r := h.runOfGateCommand(cmd)
	if !r.Forced || r.Exit != 1 || r.Lane != "web" {
		t.Fatalf("the failed run must be a forced admission: %+v", r)
	}

	rc, g := h.gateJSON()
	c := g.Commands[0]
	if rc != 1 || c.State != proof.Failed || c.Class != proof.ClassUnderLoad || !strings.Contains(c.Reason, "anti-starvation") {
		t.Fatalf("forced failure: rc=%d %+v", rc, c)
	}
	if rc, out = h.run("gate", "status"); rc != 1 || !strings.Contains(out, "1 failed (1 under load), 0 missing") ||
		!strings.Contains(out, "rerun once before debugging: heavyq gate run ready") {
		t.Fatalf("status text (rc=%d):\n%s", rc, out)
	}

	// The cause goes away and the rerun passes: under load is not flaky, so
	// the tree is proven now.
	if err := os.Remove(filepath.Join(h.root, "fail-flag")); err != nil {
		t.Fatal(err)
	}
	if rc, out := h.run("gate", "run"); rc != 0 {
		t.Fatalf("rerun after a failure under load rc=%d\n%s", rc, out)
	}
	if rc, g = h.gateJSON(); rc != 0 || !g.Proven || g.Commands[0].Fails != 1 || g.Commands[0].Passes != 1 {
		t.Fatalf("proven after the rerun: rc=%d %+v", rc, g.Commands[0])
	}
}

// Without any bypass, a failure that started with less free RAM than the job's
// budget is under_load, from the machine reading in the ledger.
func TestE2EGateFailureWithLittleFreeRAMIsUnderLoad(t *testing.T) {
	h := newRig(t, loadedConfig)
	_, cmd := flagGateRepo(t, h)
	h.touch("fail-flag")
	if rc, out := h.run("gate", "run"); rc != 1 {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
	r := h.runOfGateCommand(cmd)
	skipUnlessMeasurable(t, r)
	if r.Forced || r.JobMB != 100000000 || r.FreeMB >= r.JobMB || r.MaxLoad <= 0 {
		t.Fatalf("reading at admission: %+v", r)
	}
	rc, g := h.gateJSON()
	c := g.Commands[0]
	if rc != 1 || c.Class != proof.ClassUnderLoad || !strings.Contains(c.Reason, "MB free < 100000000 MB job budget") {
		t.Fatalf("rc=%d %+v", rc, c)
	}
}

// A plain failure on a machine inside its limits stays "failed".
func TestE2EGatePlainFailureIsFailed(t *testing.T) {
	h := newRig(t, calmConfig)
	_, cmd := flagGateRepo(t, h)
	h.touch("fail-flag")
	if rc, out := h.run("gate", "run"); rc != 1 {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
	r := h.runOfGateCommand(cmd)
	if r.CmdHash != history.CmdHash([]string{"sh", "-c", cmd}) || r.Forced {
		t.Fatalf("%+v", r)
	}
	if runtime.GOOS == "linux" && (r.Cores < 1 || r.JobMB != 1 || r.MaxLoad < 64) {
		t.Fatalf("a local run records the machine at admission: %+v", r)
	}
	rc, g := h.gateJSON()
	c := g.Commands[0]
	if rc != 1 || c.State != proof.Failed || c.Class != proof.ClassFailed || c.Passes != 0 || c.Fails != 1 || c.Reason != "" {
		t.Fatalf("rc=%d %+v", rc, c)
	}
	if rc, out := h.run("gate", "status"); rc != 1 || !strings.Contains(out, "(exit 1)") || !strings.Contains(out, "1 failed, 0 missing") ||
		strings.Contains(out, "flaky") || strings.Contains(out, "under load") {
		t.Fatalf("status text (rc=%d):\n%s", rc, out)
	}
}
