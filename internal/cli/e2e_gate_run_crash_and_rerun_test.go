package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LysCongThanh/heavyq/internal/proof"
)

// gateRepoRuns commits a heavyq.toml whose gate "ready" runs cmds.
func gateRepoRuns(t *testing.T, h *rig, cmds ...string) string {
	t.Helper()
	repo := gitRepo(t, h)
	toml := "[[gate]]\nname = \"ready\"\nrun = ['" + strings.Join(cmds, "', '") + "']\nprio = 3\n"
	if err := os.WriteFile(filepath.Join(repo, "heavyq.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, h, repo, "add", "heavyq.toml")
	gitIn(t, h, repo, "commit", "-q", "-m", "add gate")
	return repo
}

// A gate command that dies of its own signal (a crash, the OOM killer) or
// exits 78 itself failed: --keep-going goes on with the others and the gate
// ends "not proven" (exit 1) with its table, in sequence and in parallel. Only
// a stop of heavyq itself (a signal, an operator cancel) is passed through.
func TestE2EGateCommandKilledByItsOwnSignalIsAFailure(t *testing.T) {
	for _, mode := range []string{"--keep-going", "--parallel"} {
		t.Run(mode, func(t *testing.T) {
			h := newRig(t, baseConfig)
			count := fmt.Sprintf("echo run >> %s/count-b", h.root)
			gateRepoRuns(t, h, "kill -9 $$", "exit 78", count)
			rc, out := h.run("gate", "run", mode)
			if rc != 1 || h.runs("b") != 1 {
				t.Fatalf("rc=%d runs of the last command=%d\n%s", rc, h.runs("b"), out)
			}
			if !strings.Contains(out, "NOT PROVEN") || strings.Contains(out, "not run") {
				t.Fatalf("the gate must report its table:\n%s", out)
			}
		})
	}
}

// With cache_failures, the failure stored by a run under load must not be
// replayed by the re-run that `gate run` advises ("rerun once before
// debugging"): that re-run really runs, on a calm machine.
func TestE2EGateRerunOfUnderLoadFailureReallyRuns(t *testing.T) {
	h := newRig(t, busyLimits+"[cache]\ncache_failures = true\n")
	h.fakeMetrics(busyReading)
	_, cmd := flagGateRepo(t, h)
	h.touch("fail-flag")
	if rc, out := h.run("gate", "run"); rc != 1 {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
	if rc, g := h.gateJSON(); rc != 1 || g.Commands[0].Class != proof.ClassUnderLoad {
		t.Fatalf("under load first: rc=%d %+v", rc, g.Commands[0])
	}
	if err := os.Remove(filepath.Join(h.root, "fail-flag")); err != nil {
		t.Fatal(err)
	}
	h.fakeMetrics(idleReading)
	rc, out := h.run("gate", "run")
	if r := h.runOfGateCommand(cmd); r.Cache == "hit" || r.Exit != 0 {
		t.Fatalf("the re-run replayed the stored failure: rc=%d %+v\n%s", rc, r, out)
	}
}
