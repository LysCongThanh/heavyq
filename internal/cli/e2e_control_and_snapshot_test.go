package cli

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/procinfo"
	"github.com/LysCongThanh/heavyq/internal/snapshot"
)

func (h *rig) history() []history.Record {
	h.t.Helper()
	recs, err := history.ReadLast(h.root+"/history.jsonl", 0)
	if err != nil {
		h.t.Fatal(err)
	}
	return recs
}

func (h *rig) statusJSON() snapshot.Snapshot {
	h.t.Helper()
	p := h.start("status", "--json")
	if rc := p.wait(h.t, 30*time.Second); rc != 0 {
		h.t.Fatalf("status --json rc=%d\n%s", rc, p.out)
	}
	var s snapshot.Snapshot
	if err := json.Unmarshal([]byte(p.out.String()), &s); err != nil {
		h.t.Fatalf("status --json is not JSON: %v\n%s", err, p.out)
	}
	return s
}

func TestStatusJSONExplainsEveryWaitingTicket(t *testing.T) {
	h := newRig(t, baseConfig) // 1 heavy + 1 light slot, gate off
	hold := h.start("4", "--lane", "holder", "--", "sh", "-c", h.gateFile("go"))
	h.running(1)
	h.start("3", "--lane", "first", "--", "true")
	h.waiting(1)
	h.start("3", "--lane", "second", "--", "env", "API_TOKEN=abc123", "true")
	h.waiting(2)
	s := h.statusJSON()
	if s.Schema != 1 || len(s.Running) != 1 || len(s.Waiting) != 2 || len(s.Machines) != 1 {
		t.Fatalf("snapshot shape: %+v", s)
	}
	if r := s.Running[0]; r.Lane != "holder" || r.Machine != "local" || r.Slot != "heavy-1" || r.ID == "" {
		t.Fatalf("running = %+v", r)
	}
	w1, w2 := s.Waiting[0], s.Waiting[1]
	if w1.Lane != "first" || w1.Position != 1 || w1.Reasons["local"] != "all heavy slots busy" || w1.BypassInS != -1 {
		t.Fatalf("head = %+v (gate off: no bypass countdown)", w1)
	}
	if w2.Reasons["local"] != "behind first (p3)" {
		t.Fatalf("second reason = %q", w2.Reasons["local"])
	}
	if !strings.Contains(w2.Cmd, "API_TOKEN=***") || strings.Contains(w2.Cmd, "abc123") {
		t.Fatalf("secret not redacted: %q", w2.Cmd)
	}
	m := s.Machines[0]
	if m.Admits != "all heavy slots busy" || len(m.Slots) != 2 || m.Slots[0].Lane != "holder" || m.Slots[1].State != "free" {
		t.Fatalf("machine = %+v", m)
	}
	h.touch("go")
	hold.wait(t, 30*time.Second)
}

func TestCancelAndReprioFromCLI(t *testing.T) {
	h := newRig(t, baseConfig)
	hold := h.start("5", "--lane", "holder", "--", "sh", "-c", "sleep 60")
	h.running(1)
	low := h.start("7", "--lane", "low", "--", "true")
	h.waiting(1)
	gone := h.start("7", "--lane", "gone", "--", "true")
	h.waiting(2)

	// reprio: "gone" (behind "low") jumps ahead.
	gonePid := strconv.Itoa(gone.cmd.Process.Pid)
	if rc, out := h.run("reprio", gonePid, "2"); rc != 0 {
		t.Fatalf("reprio rc=%d: %s", rc, out)
	}
	h.until("reprio applied", func() bool {
		w := h.snapshot().Waiting
		return len(w) == 2 && w[0].Lane == "gone" && w[0].Prio == 2
	})
	if rc, _ := h.run("reprio", strconv.Itoa(hold.cmd.Process.Pid), "1"); rc != ExitUsage {
		t.Fatal("a running ticket must refuse a priority change")
	}

	// cancel a waiting ticket: it leaves the queue with 78 and is recorded.
	if rc, out := h.run("cancel", gonePid); rc != 0 {
		t.Fatalf("cancel rc=%d: %s", rc, out)
	}
	if rc := gone.wait(t, 20*time.Second); rc != ExitCancelled {
		t.Fatalf("cancelled waiting ticket rc=%d\n%s", rc, gone.out)
	}
	// cancel the running one: SIGTERM to the command, 78, slot freed.
	if rc, _ := h.run("cancel", strconv.Itoa(hold.cmd.Process.Pid)); rc != 0 {
		t.Fatal("cancel running failed")
	}
	if rc := hold.wait(t, 20*time.Second); rc != ExitCancelled {
		t.Fatalf("cancelled running ticket rc=%d\n%s", rc, hold.out)
	}
	if rc := low.wait(t, 20*time.Second); rc != 0 {
		t.Fatalf("next ticket rc=%d", rc)
	}
	var results []string
	for _, r := range h.history() {
		results = append(results, r.Lane+":"+r.Outcome())
	}
	if got := strings.Join(results, ","); got != "gone:cancelled,holder:cancelled,low:ran" {
		t.Fatalf("history outcomes = %s", got)
	}
	if rc, _ := h.run("cancel", "999999"); rc != ExitUsage {
		t.Fatal("unknown pid must be a usage error")
	}
}

func TestPauseBlocksAdmissionAndGiveUpIsRecorded(t *testing.T) {
	h := newRig(t, baseConfig)
	if rc, out := h.run("pause", "local"); rc != 0 {
		t.Fatalf("pause rc=%d: %s", rc, out)
	}
	if rc, _ := h.run("pause", "nowhere"); rc != ExitUsage {
		t.Fatal("unknown machine must be refused")
	}
	rc, out := h.run("3", "--lane", "blocked", "--max-wait", "300ms", "--", "true")
	if rc != ExitMaxWait {
		t.Fatalf("paused machine admitted a job: rc=%d\n%s", rc, out)
	}
	if s := h.statusJSON(); !s.Machines[0].Paused || !strings.HasPrefix(s.Machines[0].Admits, "paused") {
		t.Fatalf("machine = %+v", s.Machines[0])
	}
	if rc, _ := h.run("resume", "local"); rc != 0 {
		t.Fatal("resume failed")
	}
	if rc, out := h.run("3", "--lane", "after", "--", "true"); rc != 0 {
		t.Fatalf("after resume rc=%d\n%s", rc, out)
	}
	recs := h.history()
	if len(recs) != 2 || recs[0].Outcome() != history.OutcomeGaveUp || recs[0].Exit != ExitMaxWait ||
		recs[0].Machine != "" || recs[1].Outcome() != history.OutcomeRan {
		t.Fatalf("history = %+v", recs)
	}
}

// H2 regression: a cancelled command's orphan that ignores SIGTERM is killed
// before the slot is released.
func TestCancelReachesOrphansBeforeFreeingSlot(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("process-tree cancel is Linux-only")
	}
	h := newRig(t, baseConfig)
	pf := filepath.Join(h.root, "orphan.pid")
	job := h.start("5", "--lane", "tree", "--", "sh", "-c",
		`(trap '' TERM; sleep 300) & echo $! > `+pf+`; sleep 300`)
	h.running(1)
	h.until("orphan started", func() bool { return h.read("orphan.pid") != "" })
	orphan, _ := strconv.Atoi(strings.TrimSpace(h.read("orphan.pid")))
	if rc, _ := h.run("cancel", strconv.Itoa(job.cmd.Process.Pid)); rc != 0 {
		t.Fatal("cancel failed")
	}
	if rc := job.wait(t, 30*time.Second); rc != ExitCancelled {
		t.Fatalf("rc=%d\n%s", rc, job.out)
	}
	// A killed orphan may linger briefly as a zombie until init reaps it.
	h.until("orphan gone", func() bool { return !procinfo.Default.Running(orphan) })
	_ = syscall.Kill(orphan, syscall.SIGKILL)
}

// H1 regression: a cancel filed under the old name reaches a ticket whose
// priority changed.
func TestCancelAfterReprio(t *testing.T) {
	h := newRig(t, baseConfig)
	hold := h.start("5", "--lane", "holder", "--", "sh", "-c", h.gateFile("go"))
	h.running(1)
	w := h.start("6", "--lane", "w", "--", "true")
	h.waiting(1)
	oldID := h.snapshot().Waiting[0].Name
	if rc, _ := h.run("reprio", oldID, "3"); rc != 0 {
		t.Fatal("reprio failed")
	}
	h.until("reprio applied", func() bool { s := h.snapshot(); return len(s.Waiting) == 1 && s.Waiting[0].Prio == 3 })
	if rc, out := h.run("cancel", oldID); rc != 0 { // stale id from an older snapshot
		t.Fatalf("cancel by stale id rc=%d: %s", rc, out)
	}
	if rc := w.wait(t, 20*time.Second); rc != ExitCancelled {
		t.Fatalf("rc=%d\n%s", rc, w.out)
	}
	h.touch("go")
	hold.wait(t, 20*time.Second)
}

// H3 regression: the bypass countdown starts when a ticket becomes head.
func TestBypassCountsFromHeadOfLine(t *testing.T) {
	h := newRig(t, "[queue]\npoll = \"50ms\"\nforce_after = \"1h\"\n[local]\nheavy_slots = 1\nlight_slots = 0\n")
	hold := h.start("5", "--lane", "holder", "--", "sh", "-c", h.gateFile("go"))
	h.running(1)
	h.start("5", "--lane", "head", "--", "true")
	h.waiting(1)
	h.until("head clock published", func() bool { return !h.snapshot().Waiting[0].HeadSince.IsZero() })
	s := h.statusJSON()
	if b := s.Waiting[0].BypassInS; b < 3500 || b > 3600 {
		t.Fatalf("bypass_in_s = %.0f, want ~3600", b)
	}
	h.touch("go")
	hold.wait(t, 20*time.Second)
}
