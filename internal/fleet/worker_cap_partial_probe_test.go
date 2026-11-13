package fleet

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// cutAfterBase is a transport that runs the real probe script on this machine and ends its answer after
// "done base", then hangs past the timeout: a machine that sent its numbers and then stalled in the slot step.
var cutAfterBase = []string{"sh", "-c", `sh -c "$1" | sed '/^done base$/q'; exec sleep 30`, "cut"}

// holdSlots creates n slot lock files in dir and holds every one with a flock(1) process.
func holdSlots(t *testing.T, dir string, n int) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= n; i++ {
		f := filepath.Join(dir, strconv.Itoa(i)+".lock")
		if err := os.WriteFile(f, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		c := exec.Command("flock", f, "sleep", "60")
		c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = syscall.Kill(-c.Process.Pid, syscall.SIGKILL); _ = c.Wait() })
	}
}

// busySlots probes the slots until owner's n locks show busy (flock takes them a moment after it starts).
func busySlots(t *testing.T, slots []SlotDir, owner string, n int) Probe {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		p, err := RunProbe(ProbeRequest{Transport: localTransport, Timeout: 20 * time.Second, Slots: slots})
		if err != nil {
			t.Fatal(err)
		}
		busy := 0
		for _, s := range p.Slots {
			if s.Owner == owner && s.Busy {
				busy++
			}
		}
		if busy == n {
			return p
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d of %s's %d slots busy: %+v", busy, owner, n, p.Slots)
		}
	}
}

// renderText is RenderText's output as a string.
func renderText(r Report) string {
	var b strings.Builder
	RenderText(&b, r)
	return b.String()
}

// A probe cut after the machine's numbers, before its slot step ended, does not know how many slots are busy.
// The worker cap must not take that for "nothing busy" (cores / 1 run): a queue whose locks were not read
// counts its configured slots, says so in every output, and with no configured slots known there is no cap.
// The control is the same machine and locks read whole: every slot is busy, so the cap is cores / 4 either way.
func TestWorkerCapOfAProbeCutBeforeItsSlotsCountsTheConfiguredSlots(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("slot holders come from /proc/locks")
	}
	for _, tool := range []string{"flock", "sed"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " not installed")
		}
	}
	root := t.TempDir()
	slots := []SlotDir{{Owner: OwnerHeavyq, Dir: filepath.Join(root, "slots")}, {Owner: "fq", Dir: filepath.Join(root, "fq")}}
	holdSlots(t, slots[0].Dir, 4)
	holdSlots(t, slots[1].Dir, 1)
	heavyq := QueueInput{Name: OwnerHeavyq, Kind: KindHeavyq, History: true} // a ledger with no ended run yet
	machine := func(p Probe, queues ...QueueInput) Machine {
		now := time.Now()
		r := Assemble(Inputs{Now: now, Since: now.Add(-time.Hour), Queues: queues,
			Machines: []MachineInput{{Name: "box", Role: RoleRemote, Probe: &p, HeavyqSlots: 4, HeavyqTotal: 4}}})
		return r.Machines[0]
	}

	whole := busySlots(t, slots[:1], OwnerHeavyq, 4)
	if whole.SlotsUnread != nil || whole.Cores < 2 {
		t.Skipf("control: unread %v, cores %d (the cut would not show on one core)", whole.SlotsUnread, whole.Cores)
	}
	want := max(1, whole.Cores/4)
	if c := machine(whole, heavyq).Cap; c == nil || c.Workers != want || c.HeavySlots != 4 || c.Queues[0].Basis != CapBasisUsed {
		t.Fatalf("control cap %+v, want %d workers from 4 busy slots", c, want)
	}

	start := time.Now()
	cut, err := RunProbe(ProbeRequest{Transport: cutAfterBase, Timeout: 2 * time.Second, Slots: slots})
	if err != nil || time.Since(start) > 10*time.Second {
		t.Fatalf("cut probe: %v after %s", err, time.Since(start))
	}
	if cut.Cores != whole.Cores || len(cut.Slots) != 0 || !slices.Equal(cut.SlotsUnread, []string{OwnerHeavyq, "fq"}) ||
		!strings.HasPrefix(cut.Partial, "no slot states, top consumers and attribution (") {
		t.Fatalf("cut probe: cores %d slots %v unread %v partial %q", cut.Cores, cut.Slots, cut.SlotsUnread, cut.Partial)
	}

	m := machine(cut, heavyq)
	c := m.Cap
	if c == nil || c.Workers != want || c.HeavySlots != 4 || m.CapUnknown != "" {
		t.Fatalf("cut cap %+v (unknown %q), want %d workers from 4 configured slots, not cores / 1", c, m.CapUnknown, want)
	}
	if q := c.Queues[0]; q.Basis != CapBasisConfigured || q.Counted != 4 || q.Note != CapNoteSlotsUnread {
		t.Fatalf("cut term %+v", q)
	}
	if len(m.Slots) != 1 || !m.Slots[0].Unread || m.Slots[0].Busy != 0 || m.Slots[0].Total != 4 {
		t.Fatalf("cut slots %+v: heavyq's must say they were not read", m.Slots)
	}
	if strings.Contains(c.Why, "now 0") || strings.Contains(c.Why, "no heavy slot in use") || !strings.Contains(c.Why, "heavyq 4 configured, slots not read") {
		t.Fatalf("cut why %q", c.Why)
	}
	r := Report{Machines: []Machine{m}}
	t.Logf("cut, configured:\n%s%s", renderText(r), RenderAgent(r))
	doc, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	for name, out := range map[string]string{"text": renderText(r), "markdown": string(RenderMarkdown(r)),
		"agent": RenderAgent(r), "json": string(doc)} {
		if !strings.Contains(out, "slots not read") {
			t.Errorf("%s output does not say the slots were not read:\n%s", name, out)
		}
		if name != "json" && !strings.Contains(out, "heavyq ?/4") {
			t.Errorf("%s output shows a busy count it did not read:\n%s", name, out)
		}
	}

	// A foreign queue probed there with no configured slots may hold any number: no cap, and why.
	fq := QueueInput{Name: "fq", Kind: KindForeign, History: true}
	m = machine(cut, heavyq, fq)
	if m.Cap != nil || !strings.HasPrefix(m.CapUnknown, "fq: slots not read") {
		t.Fatalf("unknown share: cap %+v, unknown %q", m.Cap, m.CapUnknown)
	}
	r = Report{Machines: []Machine{m}}
	t.Logf("cut, unknown:\n%s%s%s", renderText(r), RenderAgent(r), RenderMarkdown(r))
	for name, out := range map[string]string{"text": renderText(r), "markdown": string(RenderMarkdown(r)), "agent": RenderAgent(r)} {
		if !strings.Contains(out, "unknown") || !strings.Contains(out, "fq: slots not read") {
			t.Errorf("%s output does not say there is no cap:\n%s", name, out)
		}
	}
	// With its slots configured it counts them too.
	fq.HeavySlots = map[string]int{"box": 2}
	if c := machine(cut, heavyq, fq).Cap; c == nil || c.HeavySlots != 6 || c.Workers != max(1, whole.Cores/6) {
		t.Fatalf("both configured: %+v", c)
	}
}
