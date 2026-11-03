package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/proof"
)

// countedPipe is a shell command whose first pipeline stage does not exist
// (exit 0 from `cat`, a not-found line on stderr), counting its runs.
func countedPipe(h *rig, tag string) []string {
	return []string{"sh", "-c", fmt.Sprintf("echo run >> %s/count-%s; nosuchcmd-xyz | cat", h.root, tag)}
}

func TestE2EPassWithNotFoundLineIsSuspect(t *testing.T) {
	h := newRig(t, baseConfig).human()
	gitRepo(t, h)
	argv := countedPipe(h, "p")
	rc, out := h.run(append([]string{"4", "--"}, argv...)...)
	if rc != 0 {
		t.Fatalf("exit stays 0 by default, got %d\n%s", rc, out)
	}
	if !strings.Contains(out, "heavyq: PASS? the command exited 0") || !strings.Contains(out, "not found") ||
		!strings.Contains(out, "--strict") || !strings.Contains(out, "warning: this shell command has a pipe") {
		t.Fatalf("missing the PASS? warning or the pipe warning:\n%s", out)
	}
	if rec := h.lastRecord(); rec.Exit != 0 || rec.Result != history.OutcomeSuspect {
		t.Fatalf("ledger must say exit 0 + suspect: %+v", rec)
	}
}

func TestE2ESuspectIsNeitherCachedNorAHit(t *testing.T) {
	h := newRig(t, baseConfig).human()
	gitRepo(t, h)
	argv := countedPipe(h, "c")
	args := append([]string{"4", "--cache", "-v", "--"}, argv...)
	_, out1 := h.run(args...)
	rc, out2 := h.run(args...)
	if rc != 0 || h.runs("c") != 2 {
		t.Fatalf("the rerun must run again (runs=%d)\n%s", h.runs("c"), out2)
	}
	if !strings.Contains(out1, "nothing stored") || strings.Contains(out2, "cached result") {
		t.Fatalf("first run must not store:\n%s\n---\n%s", out1, out2)
	}
	for _, rec := range h.history() {
		if rec.Cache == "hit" || rec.Cache == "coalesced" || rec.Result != history.OutcomeSuspect {
			t.Fatalf("unexpected record %+v", rec)
		}
	}
}

func TestE2EStrictTurnsSuspectIntoExit127(t *testing.T) {
	h := newRig(t, baseConfig).human()
	gitRepo(t, h)
	rc, out := h.run(append([]string{"4", "--strict", "--"}, countedPipe(h, "s")...)...)
	if rc != 127 || !strings.Contains(out, "heavyq: FAIL exit 127 (strict):") {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
	if rec := h.lastRecord(); rec.Exit != 127 || rec.Result != history.OutcomeSuspect {
		t.Fatalf("%+v", rec)
	}
	// An ordinary line that mentions "not found" is not suspect.
	rc, out = h.run("4", "--strict", "--", "sh", "-c", "echo 'user: 12: record: not found'; echo 'sh: 1: foo: not found because'")
	if rc != 0 || strings.Contains(out, "PASS?") {
		t.Fatalf("false positive: rc=%d\n%s", rc, out)
	}
}

func TestE2ESuspectDigestVerdicts(t *testing.T) {
	h := newAgentRig(t)
	gitRepo(t, h)
	rc, out := h.run(append([]string{"4", "--"}, countedPipe(h, "d")...)...)
	if rc != 0 || !strings.Contains(out, "heavyq: PASS? ") || strings.Contains(out, "heavyq: PASS ") || !strings.Contains(out, "next: ") {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
	jh := *h
	jh.env = append(append([]string{}, h.env...), "HEAVYQ_OUTPUT=json")
	if _, out = jh.run(append([]string{"4", "--"}, countedPipe(h, "d")...)...); !strings.Contains(out, `"verdict":"PASS?"`) {
		t.Fatalf("json digest:\n%s", out)
	}
	if rc, out = h.run(append([]string{"4", "--strict", "--"}, countedPipe(h, "d")...)...); rc != 127 || !strings.Contains(out, "heavyq: FAIL ") {
		t.Fatalf("strict digest rc=%d\n%s", rc, out)
	}
}

// suspectGate commits a gate with one command.
func suspectGate(t *testing.T, h *rig, cmd string) {
	t.Helper()
	repo := gitRepo(t, h)
	toml := fmt.Sprintf("[[gate]]\nname = \"ready\"\nrun = [%q]\nprio = 3\n", cmd)
	if err := os.WriteFile(filepath.Join(repo, "heavyq.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, h, repo, "add", "heavyq.toml")
	gitIn(t, h, repo, "commit", "-q", "-m", "add gate")
}

func TestE2EGatePipelineFailsUnderPipefail(t *testing.T) {
	h := newRig(t, baseConfig).human()
	suspectGate(t, h, "false | cat")
	if rc, out := h.run("gate", "run", "ready"); rc != 1 {
		t.Fatalf("a failing first stage must fail the gate, rc=%d\n%s", rc, out)
	}
	if rc, g := h.gateJSON(); rc != 1 || g.Proven || g.Commands[0].State != proof.Failed {
		t.Fatalf("%d %+v", rc, g)
	}
	if rec := h.lastRecord(); rec.Exit != 1 || rec.Result != "" {
		t.Fatalf("%+v", rec)
	}
}

func TestE2EGateWithNotFoundLineIsNotProven(t *testing.T) {
	h := newRig(t, baseConfig).human()
	// exits 0, yet prints what a shell prints for a missing command
	suspectGate(t, h, "echo 'sh: 1: tsc: not found'")
	rc, out := h.run("gate", "run", "ready")
	if rc != 1 || !strings.Contains(out, "FAIL exit 127 (strict)") {
		t.Fatalf("a gate is a proof: rc=%d\n%s", rc, out)
	}
	if rec := h.lastRecord(); rec.Exit != 127 || rec.Result != history.OutcomeSuspect {
		t.Fatalf("%+v", rec)
	}
	if _, g := h.gateJSON(); g.Proven || g.Commands[0].State == proof.Proven {
		t.Fatalf("%+v", g)
	}
	// The same argv run by hand without --strict or --gate is a suspect PASS?, and no proof either.
	cmd := "echo 'sh: 1: tsc: not found'"
	if rc, out = h.run(append([]string{"3", "--cache", "--"}, proof.Argv(cmd)...)...); rc != 0 || !strings.Contains(out, "PASS?") {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
	if _, g := h.gateJSON(); g.Proven || g.Commands[0].State == proof.Proven {
		t.Fatalf("a suspect run must not count as proof: %+v", g)
	}
}
