package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LysCongThanh/heavyq/internal/history"
)

// The field incident behind the exit canary: a remote's PATH had a program named setsid that forked and exited
// 0, so every run there passed and its proofs were false. Here that program also claims to be util-linux's
// setsid when asked for its version, which defeats the job script's own check: only the canary is left.
const lyingSetsidWrapper = `#!/bin/sh
if [ "$1" = --version ]; then echo "setsid from util-linux 2.39.3"; exit 0; fi
"$@" &
exit 0
`

// lyingRemote configures remote "box" whose env_path puts the lying setsid first, plus extra keys.
func lyingRemote(t *testing.T, h *rig, extra string) {
	t.Helper()
	dir := filepath.Join(h.root, "wrapper-bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "setsid"), []byte(lyingSetsidWrapper), 0o755); err != nil {
		t.Fatal(err)
	}
	h.setConfig(remoteConfig(h, "env_path = \""+dir+"\"\n"+extra))
}

// boxProofs are the ledger lines of runs on box that could count as proof.
func (h *rig) boxProofs() []history.Record {
	var out []history.Record
	for _, r := range h.history() {
		if r.Machine == "box" && r.Result == "" {
			out = append(out, r)
		}
	}
	return out
}

const untrustedBox = "exit codes not trustworthy on box"

func TestE2EExitCanaryRefusesProofsOnARemoteThatLosesExitCodes(t *testing.T) {
	h := newRig(t, "")
	lyingRemote(t, h, "")
	repo := gitRepo(t, h)
	if err := os.WriteFile(filepath.Join(repo, "heavyq.toml"), []byte("[[gate]]\nname = \"ready\"\nrun = ['false']\nprio = 3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, h, repo, "add", "heavyq.toml")
	gitIn(t, h, repo, "commit", "-q", "-m", "gate")

	rc, out := h.run("gate", "run", "--remote", "box")
	if rc == 0 || !strings.Contains(out, untrustedBox) {
		t.Fatalf("a gate on a remote that loses exit codes must not pass: rc=%d\n%s", rc, out)
	}
	if rc, g := h.gateJSON(); rc == 0 || g.Proven {
		t.Fatalf("the gate is proven by a remote that loses exit codes: rc=%d %+v", rc, g)
	}
	if p := h.boxProofs(); len(p) != 0 {
		t.Fatalf("runs on box recorded as proof: %+v", p)
	}
	// --cache stores a result: it is a proof too, refused with the remote setup code.
	if rc, out := h.run("3", "--remote", "box", "--cache", "--", "true"); rc != ExitRemoteSetup || !strings.Contains(out, untrustedBox) {
		t.Fatalf("--cache on an untrusted remote: rc=%d\n%s", rc, out)
	}
	// Refused before the command ran: heavyq's own 77, so the ledger says it gave up, not that it ran.
	if recs := h.history(); len(recs) == 0 || recs[len(recs)-1].Result != history.OutcomeGaveUp {
		t.Fatalf("a run refused by the exit canary must be recorded as gave up: %+v", recs)
	}
	for _, args := range [][]string{{"why"}, {"status"}, {"doctor"}} {
		if _, out := h.run(args...); !strings.Contains(out, untrustedBox) {
			t.Fatalf("heavyq %s does not say %q:\n%s", args[0], untrustedBox, out)
		}
	}
}

func TestE2EExitCanaryOptInRunsButProvesNothing(t *testing.T) {
	h := newRig(t, "")
	lyingRemote(t, h, "allow_untrusted_exit = true\n")
	gitRepo(t, h)
	rc, out := h.run("3", "--remote", "box", "--", "sh", "-c", "exit 3")
	if !strings.Contains(out, untrustedBox) {
		t.Fatalf("the run must say the exit code is not trustworthy: rc=%d\n%s", rc, out)
	}
	recs := h.history()
	if len(recs) != 1 || recs[0].Machine != "box" || recs[0].Result != history.OutcomeSuspect {
		t.Fatalf("an opted-in run on an untrusted remote must be recorded as suspect: %+v", recs)
	}
	// A proof stays refused even with the opt-in.
	if rc, out := h.run("3", "--remote", "box", "--strict", "--", "true"); rc != ExitRemoteSetup || !strings.Contains(out, untrustedBox) {
		t.Fatalf("a strict run on an untrusted remote: rc=%d\n%s", rc, out)
	}
	if p := h.boxProofs(); len(p) != 0 {
		t.Fatalf("runs on box recorded as proof: %+v", p)
	}
}
