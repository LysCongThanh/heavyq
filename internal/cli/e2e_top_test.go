package cli

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
)

var escapeSeq = regexp.MustCompile("\x1b")

// `heavyq top --once` and `--line` print real frames of a real queue: a held
// run and a waiting one, in every mode, without touching the terminal.
func TestTopOncePrintsEveryModeOfARealQueue(t *testing.T) {
	h := newRig(t, baseConfig)
	h.env = append(h.env, "XDG_STATE_HOME="+t.TempDir()) // never read the user's remembered mode
	hold := h.start("5", "--lane", "holder", "--", "sh", "-c", h.gateFile("go"))
	h.running(1)

	rc, out := h.run("top", "--once", "--width", "100")
	if rc != 0 {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
	for _, want := range []string{"heavyq  1 running", "LANES", "holder", "MACHINES", "local", "q quit"} {
		if !strings.Contains(out, want) {
			t.Errorf("full frame lacks %q:\n%s", want, out)
		}
	}
	if escapeSeq.MatchString(out) {
		t.Errorf("output to a pipe must carry no escape sequences:\n%q", out)
	}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if n := len([]rune(line)); n > 100 {
			t.Errorf("line of %d runes exceeds --width 100: %q", n, line)
		}
	}

	// What needs you depends on the host the test runs on (a machine exception when
	// outsiders hold the RAM), so focus is checked for its shape, not its content.
	for mode, want := range map[string]string{"focus": "NEEDS YOU", "lanes": "state", "machines": "MACHINES"} {
		if rc, out := h.run("top", "--once", "--mode", mode, "--width", "100"); rc != 0 || !strings.Contains(out, want) ||
			(mode == "focus" && strings.Contains(out, "LANES")) {
			t.Errorf("mode %s: rc=%d, want %q in\n%s", mode, rc, want, out)
		}
	}
	rc, line := h.run("top", "--line")
	if rc != 0 || strings.Count(strings.TrimRight(line, "\n"), "\n") != 0 || !strings.HasPrefix(line, "hq 1 run") {
		t.Errorf("--line rc=%d, want one line starting 'hq 1 run':\n%q", rc, line)
	}
	if rc, ascii := h.run("top", "--once", "--ascii", "--width", "100"); rc != 0 || strings.ContainsAny(ascii, "●○▲▪▫") {
		t.Errorf("--ascii rc=%d:\n%s", rc, ascii)
	}

	h.touch("go")
	if rc := hold.wait(t, 30*time.Second); rc != 0 {
		t.Fatalf("holder rc=%d: %s", rc, hold.out)
	}
}

func TestTopRefusesBadInputAndNonTerminals(t *testing.T) {
	h := newRig(t, baseConfig)
	h.env = append(h.env, "XDG_STATE_HOME="+t.TempDir())
	for _, args := range [][]string{
		{"top", "--mode", "bogus"}, {"top", "--interval", "100ms"}, {"top", "extra"}, {"top", "--width", "-3"}, {"top", "--bogus"},
	} {
		if rc, out := h.run(args...); rc != ExitUsage {
			t.Errorf("%v: rc=%d want 2\n%s", args, rc, out)
		}
	}
	// The e2e rig pipes stdout: a live screen must refuse and say what to use instead.
	rc, out := h.run("top")
	if rc != ExitUsage || !strings.Contains(out, "needs a terminal") || !strings.Contains(out, "--once") {
		t.Errorf("top on a pipe: rc=%d\n%s", rc, out)
	}
}

// The control actions behind --control are the dashboard's: they reach the
// real mailbox and refuse what the dashboard refuses.
func TestTopActionsUseTheControlMailboxWithTheDashboardChecks(t *testing.T) {
	h := newSerialRig(t, baseConfig) // t.Setenv below
	hold := h.start("5", "--lane", "holder", "--", "sh", "-c", h.gateFile("go"))
	h.running(1)
	wait := h.start("6", "--lane", "later", "--", "true")
	h.waiting(1)

	t.Setenv("HEAVYQ_DIR", h.root+"/q")
	cfg, err := config.Load(filepath.Join(h.root, "config.toml"), true)
	if err != nil {
		t.Fatal(err)
	}
	a, err := newTopActions(newLiveConfig(cfg, newProbeCache(cfg.Queue.ProbeTimeout.Duration)).get)
	if err != nil {
		t.Fatal(err)
	}
	snap := h.snapshot()
	running, waiting := snap.Running[0].Name, snap.Waiting[0].Name

	if err := a.Prio(running, 2); err == nil {
		t.Error("a running ticket must not change priority")
	}
	if err := a.Prio(waiting, 2); err != nil {
		t.Errorf("Prio on a waiting ticket: %v", err)
	}
	if err := a.Prio(waiting, 12); err == nil {
		t.Error("priority 12 must be refused")
	}
	if err := a.Cancel("../../etc/passwd"); err == nil {
		t.Error("a path as ticket id must be refused")
	}
	if err := a.Cancel("5-00000000000000000000-1-ghost"); err == nil {
		t.Error("cancel of a ticket that does not exist must fail")
	}
	if err := a.SetPaused("nowhere", true); err == nil {
		t.Error("pause of an unknown machine must fail")
	}
	if err := a.Cancel(waiting); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if rc := wait.wait(t, 30*time.Second); rc != ExitCancelled {
		t.Fatalf("the cancelled run exited %d, want %d", rc, ExitCancelled)
	}
	h.touch("go")
	if rc := hold.wait(t, 30*time.Second); rc != 0 {
		t.Fatalf("holder rc=%d: %s", rc, hold.out)
	}
}
