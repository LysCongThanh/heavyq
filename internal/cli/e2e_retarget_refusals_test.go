package cli

import (
	"strings"
	"testing"
	"time"
)

// `heavyq retarget` fails closed: every move it cannot make safely is refused
// with exit 2 before any request is filed, and the ticket keeps its machines.

func TestE2ERetargetRefusesWhatItCannotMoveSafely(t *testing.T) {
	h := newRig(t, "")
	h.setConfig(twoRemotesConfig(h, "[[pool]]\nname = \"pg\"\nvalues = [\"db1\"]\n"))
	gitRepo(t, h)
	hold := h.start("3", "--remote", "local", "--lane", "hold", "--", "sh", "-c", h.gateFile("go"))
	h.running(1)
	here := h.start("3", "--remote", "local", "--lane", "here", "--", "true")
	h.waiting(1)
	pg := h.start("3", "--needs", "pg", "--lane", "pg", "--", "true")
	h.waiting(2)

	for _, c := range []struct {
		name string
		args []string
		want string
	}{
		{"no --remote", []string{pidArg(here)}, "usage: heavyq retarget"},
		{"no ticket", []string{"--remote", "box"}, "usage: heavyq retarget"},
		{"unknown machine", []string{pidArg(here), "--remote", "ghost"}, `"ghost"`},
		{"bad list", []string{pidArg(here), "--remote", "box,box"}, "listed twice"},
		{"running ticket", []string{pidArg(hold), "--remote", "box"}, "already running"},
		{"finished ticket", []string{"999999", "--remote", "box"}, "no ticket"},
		{"queued for this machine only", []string{pidArg(here), "--remote", "box"}, "recorded no commit"},
		{"auto may leave this machine", []string{pidArg(here), "--remote", "auto"}, "queue it again with --remote auto"},
		{"pool values of this machine", []string{pidArg(pg), "--remote", "box2"}, "--needs pg"},
	} {
		t.Run(c.name, func(t *testing.T) {
			rc, out := h.run(append([]string{"retarget"}, c.args...)...)
			if rc != ExitUsage || !strings.Contains(out, c.want) {
				t.Fatalf("rc=%d, want %d with %q\n%s", rc, ExitUsage, c.want, out)
			}
		})
	}

	// A ticket that recorded its commit, waiting for a paused box: a move onto
	// paused machines only is refused too (a paused machine admits nothing new).
	for _, m := range []string{"box", "box2"} {
		if rc, out := h.run("pause", m); rc != 0 {
			t.Fatalf("pause %s: rc=%d\n%s", m, rc, out)
		}
	}
	far := h.start("3", "--remote", "box", "--lane", "far", "--", "true")
	h.waiting(3)
	for _, where := range []string{"box2", "box2,box"} {
		if rc, out := h.run("retarget", pidArg(far), "--remote", where); rc != ExitUsage || !strings.Contains(out, "paused") {
			t.Fatalf("move onto paused %s: rc=%d\n%s", where, rc, out)
		}
	}

	// Nothing moved, and asking for the target a ticket already has changes nothing.
	for lane, want := range map[string]string{"here": "local", "pg": "local", "far": "box"} {
		if got := h.waitingTarget(lane); got != want {
			t.Fatalf("lane %s target = %q after refused moves, want %q", lane, got, want)
		}
	}
	if rc, out := h.run("retarget", pidArg(here), "--remote", "local"); rc != 0 || !strings.Contains(out, "already waits for local") {
		t.Fatalf("same target: rc=%d\n%s", rc, out)
	}
	if rc, out := h.run("cancel", pidArg(far)); rc != 0 {
		t.Fatalf("cancel: rc=%d\n%s", rc, out)
	}
	if rc := far.wait(t, 20*time.Second); rc != ExitCancelled {
		t.Fatalf("cancelled ticket rc=%d\n%s", rc, far.out)
	}
	h.touch("go")
	for _, p := range []*proc{hold, here, pg} {
		if rc := p.wait(t, 30*time.Second); rc != 0 {
			t.Fatalf("rc=%d\n%s", rc, p.out)
		}
	}
}
