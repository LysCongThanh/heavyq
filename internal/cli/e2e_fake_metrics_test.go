package cli

import (
	"strings"
	"testing"

	"github.com/LysCongThanh/heavyq/internal/proof"
	"github.com/LysCongThanh/heavyq/internal/snapshot"
)

// fakeMetrics pins the machine reading every heavyq process of the rig sees
// (the test-only override of internal/metrics, honored only with HEAVYQ_TEST=1).
// It makes the load-driven part of "under load" deterministic: no test has to
// hope that the machine running the suite is, or is not, busy.
func (h *rig) fakeMetrics(spec string) {
	h.t.Helper()
	h.env = append(append([]string(nil), h.env...), "HEAVYQ_TEST=1", "HEAVYQ_TEST_FAKE_METRICS="+spec)
}

const (
	// idleReading: far under every limit of baseConfig-like configs.
	idleReading = "load1=0.1,free_mb=99999,cores=8"
	// busyReading: load 9 on 8 cores, over a max_load_ratio of 0.5 (limit 4.00).
	busyReading = "load1=9,free_mb=99999,cores=8"
	// busyLimits: the resource gate is off, but a reading of 4.00 load or more
	// counts as loaded; the RAM budget is tiny so only the load can explain.
	busyLimits = "[queue]\npoll = \"50ms\"\nresource_gate = false\n[local]\nheavy_slots = 1\nlight_slots = 1\n" +
		"job_mb = 1\nlight_mb = 1\nmax_load_ratio = 0.5\n"
)

// A failure that started while the 1-minute load was over the limit is
// under_load, from the reading stamped on the ledger: never failed, never a
// red gate.
func TestE2EGateFailureAtHighLoadIsUnderLoad(t *testing.T) {
	h := newRig(t, busyLimits)
	h.fakeMetrics(busyReading)
	_, cmd := flagGateRepo(t, h)
	h.touch("fail-flag")
	if rc, out := h.run("gate", "run"); rc != 1 {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
	r := h.runOfGateCommand(cmd)
	if r.Forced || r.Cores != 8 || r.Load1 != 9 || r.FreeMB != 99999 || r.MaxLoad != 4 || r.JobMB != 1 {
		t.Fatalf("the ledger must carry the pinned reading: %+v", r)
	}
	rc, g := h.gateJSON()
	c := g.Commands[0]
	if rc != 1 || c.State != proof.Failed || c.Class != proof.ClassUnderLoad || c.Reason != "under load: load 9.00 >= 4.00 at start" {
		t.Fatalf("rc=%d %+v", rc, c)
	}

	// Under load is not a verdict about the code: the Tower raises no red gate.
	s := h.statusJSON()
	if got := exceptionsOf(s, snapshot.KindRedGate); len(got) != 0 {
		t.Fatalf("a failure under load must not raise red_gate: %+v", got)
	}
	if len(s.Gates) != 1 || s.Gates[0].State != snapshot.GateFailed {
		t.Fatalf("gates=%+v", s.Gates)
	}
}

// The same failure on a calm reading is a plain failure and does raise the
// red gate, whatever the real machine is doing at that moment.
func TestE2EGateFailureAtLowLoadIsFailedAndRaisesRedGate(t *testing.T) {
	h := newRig(t, busyLimits) // same limits as the test above: only the reading differs
	h.fakeMetrics(idleReading)
	_, cmd := flagGateRepo(t, h)
	h.touch("fail-flag")
	if rc, out := h.run("gate", "run"); rc != 1 {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
	if r := h.runOfGateCommand(cmd); r.Load1 != 0.1 || r.Cores != 8 || r.Forced {
		t.Fatalf("the ledger must carry the pinned reading: %+v", r)
	}
	rc, g := h.gateJSON()
	if c := g.Commands[0]; rc != 1 || c.State != proof.Failed || c.Class != proof.ClassFailed || c.Reason != "" {
		t.Fatalf("rc=%d %+v", rc, c)
	}
	s := h.statusJSON()
	if got := exceptionsOf(s, snapshot.KindRedGate); len(got) != 1 || got[0].Severity != snapshot.SeverityCritical {
		t.Fatalf("a plain failure raises red_gate: %+v", s.Exceptions)
	}
}

// A loop is about the agent repeating itself on the same code, so it does not
// look at the machine: two failures at high load still raise it (the gate
// verdict is what stays "under load").
func TestE2ETowerLoopIgnoresLoad(t *testing.T) {
	h := newRig(t, busyLimits)
	h.fakeMetrics(busyReading)
	gitRepo(t, h)
	for range 2 {
		if rc, out := h.run("3", "--lane", "agent", "--", "sh", "-c", "exit 3"); rc != 3 {
			t.Fatalf("rc=%d\n%s", rc, out)
		}
	}
	s := h.statusJSON()
	if got := exceptionsOf(s, snapshot.KindLoop); len(got) != 1 || got[0].Evidence.Count != 2 {
		t.Fatalf("two failures on the same code are a loop, load or not: %+v", s.Exceptions)
	}
}

// The override is a test hook: without HEAVYQ_TEST=1 the spec is ignored and
// the ledger carries the real machine, not the pinned one.
func TestE2EFakeMetricsNeedTestMode(t *testing.T) {
	h := newRig(t, baseConfig)
	h.env = append(append([]string(nil), h.env...), "HEAVYQ_TEST_FAKE_METRICS="+busyReading)
	gitRepo(t, h)
	if rc, out := h.run("3", "--", "sh", "-c", "exit 0"); rc != 0 {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
	r := h.lastRecord()
	if r.Load1 == 9 && r.Cores == 8 && r.FreeMB == 99999 {
		t.Fatalf("the override was honored without HEAVYQ_TEST=1: %+v", r)
	}
	if strings.Contains(r.LoadWhy(), "9.00") {
		t.Fatalf("%q", r.LoadWhy())
	}
}
