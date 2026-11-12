package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/slots"
)

// cleanEnvRemote is a remote reached through `env -i ... sh -c`: it sees none
// of heavyq's environment, like a login over ssh, and has its own default for
// FEATURE_X. It keeps the real HOME (git on PATH may be a wrapper that finds
// the real git through it) and the rig's own git config.
func cleanEnvRemote(h *rig, extra string) string {
	work := filepath.Join(h.root, "remote-work")
	login := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"),
		"GIT_CONFIG_GLOBAL=" + filepath.Join(h.root, "gitconfig"), "GIT_CONFIG_NOSYSTEM=1", "FEATURE_X=login-default"}
	return baseConfig + "[cache]\nenv = [\"FEATURE_X\"]\n" + `[[remote]]
name = "box"
transport = ["env", "-i", ` + tomlStrings(login) + `, "sh", "-c"]
work_dir = "` + work + `"
git_url = "` + filepath.Join(work, "git") + `"
heavy_slots = 1
` + extra
}

// tomlStrings is vals as the items of a TOML array of strings.
func tomlStrings(vals []string) string {
	q := make([]string, len(vals))
	for i, v := range vals {
		q[i] = strconv.Quote(v)
	}
	return strings.Join(q, ", ")
}

// The cache.env variables are part of what a run is (its run_id and cache
// key). A remote run is recorded with the local values, so it must run with
// them: set when set here, unset when unset here, whatever the remote's own
// environment says.
func TestE2ERemoteRunGetsTheCacheEnvValuesItIsRecordedWith(t *testing.T) {
	h := newRig(t, "")
	h.setConfig(cleanEnvRemote(h, ""))
	gitRepo(t, h)
	argv := []string{"3", "--remote", "box", "--", "sh", "-c", `echo "x=${FEATURE_X-unset}"`}
	h.withEnv("FEATURE_X=1", func() {
		if rc, out := h.run(argv...); rc != 0 || !strings.Contains(out, "x=1\n") {
			t.Fatalf("FEATURE_X=1 here: rc=%d\n%s", rc, out)
		}
	})
	if rc, out := h.run(argv...); rc != 0 || !strings.Contains(out, "x=unset\n") {
		t.Fatalf("FEATURE_X unset here: rc=%d\n%s", rc, out)
	}
}

// A waiting ticket whose machines change while it waits (a --with sibling
// edited, so local is no longer the code asked for) must say so in its ticket
// file: the other waiters schedule against the file, and status shows it. A
// local-only waiter behind it must then get the local slot as soon as it is
// free, instead of waiting for a ticket that can no longer take that slot.
func TestE2EWaitingTicketPublishesItsCurrentMachines(t *testing.T) {
	h, app, lib := withRig(t, "")
	h.setConfig(remoteConfig(h, ""))
	h.cwd = app
	hold := occupyLocal(h)
	remoteHold := h.start("3", "--remote", "box", "--", "sh", "-c", h.gateFile("box-go"))
	h.running(2)
	p := h.start("3", "--with", "lib", "--", "true")
	h.waiting(1)
	// The first round publishes the ticket anyway (its head clock starts), and
	// with it whatever candidates it has then. Edit lib only after that, so the
	// file can only drop local through the candidate-change rewrite.
	h.until("the first round to publish the ticket", func() bool {
		w := h.snapshot().Waiting
		return len(w) == 1 && !w[0].HeadSince.IsZero()
	})
	if c := h.snapshot().Waiting[0].Candidates; !slices.Contains(c, "local") {
		t.Fatalf("local is a candidate at first: %v", c)
	}
	if err := os.WriteFile(filepath.Join(lib, "a.txt"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.until("the ticket file to drop local", func() bool {
		w := h.snapshot().Waiting
		return len(w) == 1 && !slices.Contains(w[0].Candidates, "local")
	})
	behind := h.start("3", "--remote", "local", "--", "true") // same prio, later: the --with ticket is ahead of it
	h.waiting(2)
	h.touch("go") // local is idle now; box stays busy
	if rc := behind.wait(t, 20*time.Second); rc != 0 {
		t.Fatalf("a local-only waiter must take the idle local slot the ticket ahead can no longer use: rc=%d\n%s", rc, behind.out)
	}
	h.touch("box-go")
	for _, q := range []*proc{hold, remoteHold, p} {
		q.wait(t, 30*time.Second)
	}
}

// heldProbeRemote is a remote whose probes block while the file "hold-probe"
// exists in the rig root: such a probe touches "in-probe" and waits for
// "probe-go". Its jobs start at once.
func heldProbeRemote(t *testing.T, h *rig) string {
	t.Helper()
	script := filepath.Join(h.root, "held-probe.sh")
	body := "case \"$1\" in env\\ *) exec bash -c \"$1\" ;; esac\n" +
		"if [ -e " + filepath.Join(h.root, "hold-probe") + " ]; then touch " + filepath.Join(h.root, "in-probe") +
		"; " + h.gateFile("probe-go") + "; fi\nexec bash -c \"$1\"\n"
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

// A cancel that arrives during a scheduling round (here while a remote is
// being probed) must stop the ticket before its command starts, even when that
// round finds a free slot. The probe is held until local is free and the cancel
// is in, so the round that claims local is the one the cancel arrived during.
func TestE2ECancelDuringASchedulingRoundNeverStartsTheCommand(t *testing.T) {
	h := newRig(t, "")
	h.setConfig(heldProbeRemote(t, h))
	gitRepo(t, h)
	dir := filepath.Join(h.root, "remote-work", "slots")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	busy, err := slots.TryLock(filepath.Join(dir, "h1.lock")) // the remote is probed, but busy
	if err != nil || busy == nil {
		t.Fatalf("lock: %v", err)
	}
	defer busy.Release()
	hold := occupyLocal(h)
	p := h.start("3", "--remote", "local,box", "--", "touch", filepath.Join(h.root, "ran"))
	h.waiting(1)
	h.touch("hold-probe")
	h.until("a probe during a round", func() bool { return fileExists(filepath.Join(h.root, "in-probe")) })
	h.touch("go")
	hold.wait(t, 20*time.Second) // local is free while the round still probes
	if rc, out := h.run("cancel", strconv.Itoa(p.cmd.Process.Pid)); rc != 0 {
		t.Fatalf("cancel rc=%d\n%s", rc, out)
	}
	h.touch("probe-go")
	rc := p.wait(t, 30*time.Second)
	if ran := fileExists(filepath.Join(h.root, "ran")); rc != ExitCancelled || ran {
		t.Fatalf("rc=%d ran=%v: the cancelled ticket must not start its command\n%s", rc, ran, p.out)
	}
}

func fileExists(path string) bool { _, err := os.Stat(path); return err == nil }
