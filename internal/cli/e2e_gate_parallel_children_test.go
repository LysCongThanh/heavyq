package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
)

const parallelConfig = "[queue]\npoll = \"50ms\"\nresource_gate = false\n[local]\nheavy_slots = 3\nlight_slots = 1\n"

// blockingGate commits a gate "ready" of two commands that print a line, wait
// for a file (a-go, b-go) and print another. Returns the gate process helper.
func blockingGate(t *testing.T, h *rig) {
	t.Helper()
	repo := gitRepo(t, h)
	cmd := func(tag string) string {
		return fmt.Sprintf("echo %s-start; %s; echo %s-end", tag, h.gateFile(tag+"-go"), tag)
	}
	toml := fmt.Sprintf("[[gate]]\nname = \"ready\"\nrun = ['%s', '%s']\nprio = 3\n", cmd("A"), cmd("B"))
	if err := os.WriteFile(filepath.Join(repo, "heavyq.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, h, repo, "add", "heavyq.toml")
	gitIn(t, h, repo, "commit", "-q", "-m", "add gate")
}

// pidOf is the pid of the running ticket whose command contains marker.
func (h *rig) pidOf(marker string) int {
	h.t.Helper()
	for _, r := range h.statusJSON().Running {
		if strings.Contains(r.Cmd, marker) {
			return r.Pid
		}
	}
	h.t.Fatalf("no running ticket for %q", marker)
	return 0
}

// #48: every command of `gate run --parallel` is its own process with its own
// ticket pid, and its output arrives with a `[cmd N]` prefix on every line.
func TestE2EGateParallelCommandsAreSeparateProcessesWithPrefixedOutput(t *testing.T) {
	h := newRig(t, parallelConfig)
	blockingGate(t, h)
	gate := h.start("gate", "run", "--parallel", "--lane", "gl")
	h.running(2)
	s := h.statusJSON()
	pa, pb := h.pidOf("A-start"), h.pidOf("B-start")
	if len(s.Running) != 2 || pa == pb || pa == gate.cmd.Process.Pid || pb == gate.cmd.Process.Pid {
		t.Fatalf("two tickets need two pids of their own (gate pid %d): %d %d", gate.cmd.Process.Pid, pa, pb)
	}
	h.touch("A-go")
	h.touch("B-go")
	if rc := gate.wait(t, 60*time.Second); rc != 0 {
		t.Fatalf("rc=%d\n%s", rc, gate.out)
	}
	out := gate.out.String()
	for _, want := range []string{"[cmd 1] A-start", "[cmd 1] A-end", "[cmd 2] B-start", "[cmd 2] B-end", "[cmd 1] heavyq: "} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	for _, line := range strings.Split(out, "\n") {
		switch line { // what the commands print, bare
		case "A-start", "A-end", "B-start", "B-end":
			t.Errorf("unprefixed command output: %q", line)
		}
	}
	if rc, g := h.gateJSON(); rc != 0 || !g.Proven {
		t.Fatalf("rc=%d %+v", rc, g)
	}
}

// `heavyq cancel <pid>` of one ticket of a parallel gate run stops exactly
// that command; the other one finishes and the run reports the cancel.
func TestE2EGateParallelCancelStopsOnlyOneTicket(t *testing.T) {
	h := newRig(t, parallelConfig)
	blockingGate(t, h)
	gate := h.start("gate", "run", "--parallel")
	h.running(2)
	pa := h.pidOf("A-start")
	if rc, out := h.run("cancel", strconv.Itoa(pa)); rc != 0 {
		t.Fatalf("cancel rc=%d\n%s", rc, out)
	}
	h.running(1)
	if got := h.pidOf("B-start"); got == pa {
		t.Fatalf("the surviving ticket is B, got pid %d", got)
	}
	h.touch("B-go")
	if rc := gate.wait(t, 60*time.Second); rc != ExitCancelled {
		t.Fatalf("an operator cancel is passed through: rc=%d\n%s", rc, gate.out)
	}
	if !strings.Contains(gate.out.String(), "[cmd 2] B-end") || strings.Contains(gate.out.String(), "[cmd 1] A-end") {
		t.Fatalf("only B may finish:\n%s", gate.out)
	}
	var cancelled, ran int
	for _, r := range h.history() {
		switch {
		case r.Result == history.OutcomeCancelled && strings.Contains(r.Cmd, "A-start"):
			cancelled++
		case r.Result == "" && r.Exit == 0 && strings.Contains(r.Cmd, "B-start"):
			ran++
		}
	}
	if cancelled != 1 || ran != 1 {
		t.Fatalf("ledger: cancelled A %d, ran B %d: %+v", cancelled, ran, h.history())
	}
}

// A signal sent to the gate run reaches every child once: they stop and are
// recorded as interrupted, and the run exits 128+n.
func TestE2EGateParallelSignalIsRelayedToEveryChild(t *testing.T) {
	h := newRig(t, parallelConfig)
	blockingGate(t, h)
	gate := h.start("gate", "run", "--parallel")
	h.running(2)
	if err := gate.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if rc := gate.wait(t, 60*time.Second); rc != 128+int(syscall.SIGTERM) {
		t.Fatalf("rc=%d\n%s", rc, gate.out)
	}
	h.until("both tickets gone", func() bool {
		s := h.snapshot()
		return len(s.Running)+len(s.Waiting) == 0
	})
	interrupted := 0
	for _, r := range h.history() {
		if r.Result == history.OutcomeInterrupted {
			interrupted++
		}
	}
	if interrupted != 2 {
		t.Fatalf("both commands must be recorded as interrupted: %+v", h.history())
	}
}
