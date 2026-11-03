package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/proof"
)

// #38: a gate command longer than the 200 bytes the ledger shows is proven by
// its hash, a proven one is not run again, and another long command that
// shares its first 200 bytes is not taken for it.
func TestE2EGateLongCommandIsProvable(t *testing.T) {
	h := newRig(t, baseConfig)
	repo := gitRepo(t, h)
	prefix := fmt.Sprintf("echo run >> %s/long-count; true %s", h.root, strings.Repeat("#", 300))
	long, other := prefix+" one", prefix+" two"
	toml := "[[gate]]\nname = \"ready\"\nrun = ['" + long + "', '" + other + "']\n"
	if err := os.WriteFile(filepath.Join(repo, "heavyq.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, h, repo, "add", ".")
	gitIn(t, h, repo, "commit", "-q", "-m", "gate")

	// Only the first command runs, directly: it is proven, the second is not.
	if rc, out := h.run(append([]string{"3", "--"}, proof.Argv(long)...)...); rc != 0 {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
	r := h.runOfGateCommand(long)
	if len(r.Cmd) != 200 || !strings.HasSuffix(r.Cmd, "...") || r.CmdHash != history.CmdHash(proof.Argv(long)) {
		t.Fatalf("the display form is cut (%d bytes) but the hash covers the whole command: %+v", len(r.Cmd), r)
	}
	rc, g := h.gateJSON()
	if rc != 1 || g.Commands[0].State != proof.Proven || g.Commands[1].State != proof.Missing {
		t.Fatalf("rc=%d %+v", rc, g.Commands)
	}
	// The gate runs what is missing, and only that.
	if rc, out := h.run("gate", "run"); rc != 0 || strings.Count(h.read("long-count"), "run") != 2 {
		t.Fatalf("rc=%d runs=%q\n%s", rc, h.read("long-count"), out)
	}
	rc, out := h.run("gate", "run")
	if rc != 0 || strings.Count(out, "proven:") != 2 || strings.Count(h.read("long-count"), "run") != 2 {
		t.Fatalf("a proven long command must not run again: rc=%d runs=%q\n%s", rc, h.read("long-count"), out)
	}
	if h.runOfGateCommand(long).Cmd != h.runOfGateCommand(other).Cmd {
		t.Fatal("the two commands should look alike in the ledger, or this test proves nothing")
	}
}

// A remote run records the machine as the probe placement used saw it.
func TestE2ERemoteRunRecordsTheProbeReading(t *testing.T) {
	h := newRig(t, "")
	h.setConfig(remoteConfig(h, ""))
	gitRepo(t, h)
	if rc, out := h.run("3", "--remote", "box", "--", "sh", "-c", "exit 0"); rc != 0 {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
	r := h.lastRecord()
	if r.Machine != "box" || r.CmdHash != history.CmdHash([]string{"sh", "-c", "exit 0"}) {
		t.Fatalf("%+v", r)
	}
	if runtime.GOOS == "linux" && (r.Cores < 1 || r.MaxLoad <= 0 || r.JobMB != 3072 || r.FreeMB <= 0) {
		t.Fatalf("a remote run records the probe's reading and the remote's limits: %+v", r)
	}
}
