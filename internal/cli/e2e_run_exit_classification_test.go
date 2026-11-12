package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
)

// A command that exits 0 but leaves a background process holding its output
// open passed: heavyq stops waiting for the output after WaitDelay, and the
// exit code is still the command's own 0, not a made-up 1.
func TestE2EBackgroundProcessHoldingOutputKeepsExitZero(t *testing.T) {
	h := newRig(t, baseConfig)
	start := time.Now()
	rc, out := h.run("5", "--", "sh", "-c", "sleep 13 & echo built ok")
	if rc != 0 || !strings.Contains(out, "built ok") {
		t.Fatalf("rc=%d after %s\n%s", rc, time.Since(start).Round(time.Second), out)
	}
	if r := h.lastRecord(); r.Exit != 0 || r.Result != "" {
		t.Fatalf("the ledger must record the command's own exit 0: %+v", r)
	}
}

// 77 is heavyq's "remote setup failed" only for a remote run. A local command
// that exits 77 itself (the automake SKIP code, EX_NOPERM) ran and failed.
func TestE2ELocalExit77IsTheCommandsOwnFailure(t *testing.T) {
	h := newAgentRig(t)
	rc, out := h.run("3", "--", "sh", "-c", "echo testing; exit 77")
	if rc != 77 {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
	if strings.Contains(out, "NOT RUN") || strings.Contains(out, "remote setup") || !strings.Contains(out, "heavyq: FAIL") {
		t.Fatalf("a local exit 77 is a failure of the command:\n%s", out)
	}
	if r := h.lastRecord(); r.Exit != 77 || r.Result != "" || r.Machine != "local" {
		t.Fatalf("the ledger must record a run that ran: %+v", r)
	}
}

// lossyTransport is a "remote" whose connection drops when the command touches
// <root>/drop: like ssh, the transport ends with 255 and the remote job is told
// to stop (its stdin lifeline stays with heavyq, so it is killed here instead).
func lossyTransport(t *testing.T, h *rig) string {
	t.Helper()
	script := filepath.Join(h.root, "lossy.sh")
	body := `bash -c "$1" <&0 & job=$!
while [ ! -e ` + filepath.Join(h.root, "drop") + ` ] && kill -0 $job 2>/dev/null; do sleep 0.05; done
if [ -e ` + filepath.Join(h.root, "drop") + ` ]; then kill -TERM $job; wait $job; exit 255; fi
wait $job
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(h.root, "remote-work")
	return baseConfig + `[[remote]]
name = "box"
transport = ["bash", "` + script + `"]
work_dir = "` + work + `"
git_url = "` + filepath.Join(work, "git") + `"
heavy_slots = 1
`
}

// A dropped connection says nothing about the code: the ledger line is not a
// conclusive run, so it neither fails nor proves anything.
func TestE2ERemoteConnectionLossIsNotAResultOfTheCommand(t *testing.T) {
	h := newRig(t, "")
	h.setConfig(lossyTransport(t, h))
	gitRepo(t, h)
	rc, out := h.run("3", "--remote", "box", "--", "sh", "-c", "touch "+filepath.Join(h.root, "drop")+"; sleep 30")
	if rc != ExitConnLost {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
	if r := h.lastRecord(); r.Exit != ExitConnLost || r.Result != history.OutcomeInterrupted {
		t.Fatalf("a lost connection is not a run of the command: %+v", r)
	}
}

// The command's own 255 on a remote is remapped (like 97 and 98), so a 255
// from the transport always means the connection was lost.
func TestE2ERemoteCommandsOwn255IsRemapped(t *testing.T) {
	h := newRig(t, "")
	h.setConfig(remoteConfig(h, ""))
	gitRepo(t, h)
	rc, out := h.run("3", "--remote", "box", "--", "sh", "-c", "exit 255")
	if rc != 96 {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
	if r := h.lastRecord(); r.Exit != 96 || r.Result != "" {
		t.Fatalf("the command ran and failed: %+v", r)
	}
}

// Exit 77 is API: a remote that cannot be prepared (the push fails, or the
// remote slot is taken outside the queue) exits 77 and records "gave up".
func TestE2ERemoteSetupFailureExits77(t *testing.T) {
	t.Run("push fails", func(t *testing.T) {
		h := newRig(t, "")
		cfg := strings.Replace(remoteConfig(h, ""), `git_url = "`+filepath.Join(h.root, "remote-work", "git")+`"`,
			`git_url = "`+filepath.Join(h.root, "config.toml", "git")+`"`, 1)
		h.setConfig(cfg)
		gitRepo(t, h)
		rc, out := h.run("3", "--remote", "box", "--", "true")
		if rc != ExitRemoteSetup {
			t.Fatalf("rc=%d\n%s", rc, out)
		}
		if r := h.lastRecord(); r.Exit != ExitRemoteSetup || r.Result != history.OutcomeGaveUp || r.Machine != "box" {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("job setup fails", func(t *testing.T) {
		h := newRig(t, "")
		h.setConfig(remoteConfig(h, "cache_hook = \"exit 3\"\n"))
		gitRepo(t, h)
		rc, out := h.run("3", "--remote", "box", "--", "touch", filepath.Join(h.root, "ran"))
		if rc != ExitRemoteSetup || !strings.Contains(out, "remote setup on box failed") {
			t.Fatalf("rc=%d\n%s", rc, out)
		}
		if r := h.lastRecord(); r.Result != history.OutcomeGaveUp {
			t.Fatalf("%+v", r)
		}
		if _, err := os.Stat(filepath.Join(h.root, "ran")); err == nil {
			t.Fatal("the command ran although the setup failed")
		}
	})
}
