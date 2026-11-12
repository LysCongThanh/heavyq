package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A waiting ticket polls for itself (there is no daemon), so it must see an
// edit of the config file the way a fresh `heavyq status` or `why` does: the
// story they tell is the decision the waiter makes on its next poll.

// reloadConfig is a local machine with the given heavy slot lock files and
// load ratio. The load reading is pinned with fakeMetrics(busyReading) (load 9
// on 8 cores): a ratio of 0.5 refuses a second job, 1.875 admits it.
func reloadConfig(locks []string, ratio string) string {
	quoted := make([]string, len(locks))
	for i, l := range locks {
		quoted[i] = fmt.Sprintf("%q", l)
	}
	return "[queue]\npoll = \"50ms\"\nforce_after = \"1h\"\n[local]\nlight_slots = 1\njob_mb = 1\nlight_mb = 1\n" +
		"max_load_ratio = " + ratio + "\nlegacy_lock_files = [" + strings.Join(quoted, ", ") + "]\n"
}

// finished reports whether p has exited, without waiting.
func (p *proc) finished() bool {
	select {
	case rc := <-p.done:
		p.done <- rc
		return true
	default:
		return false
	}
}

// The field report: a ticket waits for the only heavy slot, held by another
// run, on a machine over its load limit. The operator adds a second slot and
// raises the load limit. The waiting ticket starts on its next poll, while the
// first run still holds its slot, and a ticket submitted later is not stuck
// behind it.
func TestE2EWaitingTicketPicksUpAnEditedConfig(t *testing.T) {
	root := realTempDir(t)
	a, b := filepath.Join(root, "a.lock"), filepath.Join(root, "b.lock")
	h := newRig(t, reloadConfig([]string{a}, "0.5"))
	h.fakeMetrics(busyReading)
	hold := h.start("4", "--lane", "holder", "--", "sh", "-c", h.gateFile("go"))
	h.running(1)
	waiter := h.start("1", "--lane", "waiter", "--", "true")
	h.waiting(1)
	if rc, out := h.run("why", "waiter"); rc != 0 || !strings.Contains(out, "local: load 9.00 >= 4.0") {
		t.Fatalf("before the edit the load limit keeps the waiter out (rc=%d):\n%s", rc, out)
	}

	h.setConfig(reloadConfig([]string{a, b}, "1.875"))
	if rc := waiter.wait(t, 15*time.Second); rc != 0 {
		t.Fatalf("waiter rc=%d:\n%s", rc, waiter.out)
	}
	if hold.finished() {
		t.Fatalf("the holder must still hold its slot: the waiter started on the new one:\n%s", hold.out)
	}
	if rc, out := h.run("1", "--lane", "probe", "--", "true"); rc != 0 {
		t.Fatalf("a later ticket runs on the new slot: rc=%d\n%s", rc, out)
	}
	h.touch("go")
	hold.wait(t, 20*time.Second)
}

// An edit that does not load (here an unsafe max_load_ratio) changes nothing:
// the waiter keeps the last good config, says so once, and picks up the next
// good edit.
func TestE2EWaitingTicketKeepsTheLastGoodConfigOnABadEdit(t *testing.T) {
	root := realTempDir(t)
	a, b := filepath.Join(root, "a.lock"), filepath.Join(root, "b.lock")
	h := newRig(t, reloadConfig([]string{a, b}, "0.5"))
	h.fakeMetrics(busyReading)
	hold := h.start("4", "--lane", "holder", "--", "sh", "-c", h.gateFile("go"))
	h.running(1)
	waiter := h.start("3", "--lane", "waiter", "--", "true")
	h.waiting(1)

	h.setConfig(reloadConfig([]string{a, b}, "100"))
	time.Sleep(time.Second) // about 20 polls
	if len(h.snapshot().Waiting) != 1 || waiter.finished() {
		t.Fatalf("a config that does not load must not admit anything:\n%s", waiter.out)
	}
	if n := strings.Count(waiter.out.String(), "keeping the last good config"); n != 1 {
		t.Fatalf("the bad edit is reported once, got %d:\n%s", n, waiter.out)
	}
	h.setConfig(reloadConfig([]string{a, b}, "1.875"))
	if rc := waiter.wait(t, 15*time.Second); rc != 0 {
		t.Fatalf("the next good edit admits the waiter: rc=%d\n%s", rc, waiter.out)
	}
	h.touch("go")
	hold.wait(t, 20*time.Second)
}

// A ticket that waits under another config file than the one `why` reads must
// not be promised a start that its own config refuses.
func TestE2EWhyDoesNotPromiseAStartUnderAnotherConfig(t *testing.T) {
	root := realTempDir(t)
	a, b := filepath.Join(root, "a.lock"), filepath.Join(root, "b.lock")
	h := newRig(t, reloadConfig([]string{a, b}, "64"))
	narrow := filepath.Join(root, "narrow.toml")
	if err := os.WriteFile(narrow, []byte(reloadConfig([]string{a}, "64")), 0o600); err != nil {
		t.Fatal(err)
	}
	hold := h.start("4", "--lane", "holder", "--", "sh", "-c", h.gateFile("go"))
	h.running(1)
	h.start("--config", narrow, "3", "--lane", "waiter", "--", "true")
	h.waiting(1)
	time.Sleep(300 * time.Millisecond) // several polls: its own config keeps it waiting
	if len(h.snapshot().Waiting) != 1 {
		t.Fatal("the waiter's own config has one slot, held by the holder")
	}
	rc, out := h.run("why", "waiter")
	if rc != 0 || strings.Contains(out, "starts on the next poll") || !strings.Contains(out, "another config") {
		t.Fatalf("why must not promise a start the waiter's config refuses (rc=%d):\n%s", rc, out)
	}
	h.touch("go")
	hold.wait(t, 20*time.Second)
}

// Needs are checked against the pools at submit (a need no pool can meet is a
// usage error), and again when the config changes while the ticket waits: an
// edit that removes the pool it waits for makes it give up (exit 2) instead of
// waiting for ever, or taking values of a pool that no longer exists.
func TestE2EWaitingTicketGivesUpWhenAnEditDropsItsPool(t *testing.T) {
	const pool = "[[pool]]\nname = \"one\"\nsize = 1\n"
	const noPool = "[queue]\npoll = \"50ms\"\nresource_gate = false\n[local]\nheavy_slots = 2\nlight_slots = 1\n" + calmLimits
	h := newRig(t, noPool+pool)
	hold := h.start("4", "--lane", "holder", "--needs", "one", "--", "sh", "-c", h.gateFile("go"))
	h.running(1)
	waiter := h.start("3", "--lane", "waiter", "--needs", "one", "--", "true")
	h.waiting(1)

	h.setConfig(noPool)
	if rc := waiter.wait(t, 15*time.Second); rc != ExitUsage || !strings.Contains(waiter.out.String(), "--needs no longer fits") {
		t.Fatalf("rc=%d, want %d:\n%s", rc, ExitUsage, waiter.out)
	}
	h.touch("go")
	hold.wait(t, 20*time.Second)
}
