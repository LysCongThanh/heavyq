package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Runs keep the ledger index fresh as they end; `doctor` reports it and
// `doctor --reindex` rebuilds it after it was lost.
func TestDoctorReportsAndRebuildsTheLedgerIndex(t *testing.T) {
	h := newRig(t, baseConfig)
	rc, out := h.run("doctor")
	if rc != 0 || !strings.Contains(out, "no runs recorded yet") {
		t.Fatalf("doctor on a fresh machine rc=%d:\n%s", rc, out)
	}
	for _, lane := range []string{"alpha", "beta", "alpha"} {
		if rc, out := h.run("5", "--lane", lane, "--", "true"); rc != 0 {
			t.Fatalf("run rc=%d:\n%s", rc, out)
		}
	}
	rc, out = h.run("doctor")
	for _, want := range []string{"ledger index: fresh", "lanes:    2", "records:  3"} {
		if rc != 0 || !strings.Contains(out, want) {
			t.Fatalf("doctor rc=%d, missing %q:\n%s", rc, want, out)
		}
	}
	if err := os.RemoveAll(filepath.Join(h.root, "history.jsonl.index")); err != nil {
		t.Fatal(err)
	}
	if rc, out = h.run("doctor"); rc != 1 || !strings.Contains(out, "ledger index: missing") || !strings.Contains(out, "doctor --reindex") {
		t.Fatalf("doctor without index rc=%d:\n%s", rc, out)
	}
	rc, out = h.run("doctor", "--reindex")
	if rc != 0 || !strings.Contains(out, "reindexed 3 records") || !strings.Contains(out, "ledger index: fresh") {
		t.Fatalf("doctor --reindex rc=%d:\n%s", rc, out)
	}
	if rc, out = h.run("doctor", "--nope"); rc != ExitUsage {
		t.Fatalf("bad flag rc=%d:\n%s", rc, out)
	}
	// why <lane> reads the lane's last run from the index.
	if rc, out = h.run("why", "alpha"); rc != 0 || strings.Contains(out, "no run of this lane in the history yet") {
		t.Fatalf("why alpha rc=%d:\n%s", rc, out)
	}
}
