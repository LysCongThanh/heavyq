package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The worker cap counts the heavy slots in use (busy now, or the most at once in the window), every number
// shown, and a foreign queue's local slot locks in a shared directory are read through its slot glob.
func TestE2EFleetCapCountsSlotsInUseAndReadsSlotGlobs(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("slot holders come from /proc/locks")
	}
	if _, err := exec.LookPath("flock"); err != nil {
		t.Skip("flock(1) not installed")
	}
	h := newRig(t, "")
	shared := filepath.Join(h.root, "shared-tmp") // the queue's locks beside another program's
	if err := os.MkdirAll(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"fq-1.lock", "fq-2.lock", "fq-l1.lock", "other-1.lock"} {
		if err := os.WriteFile(filepath.Join(shared, f), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	holder := exec.Command("flock", filepath.Join(shared, "fq-2.lock"), "sleep", "60")
	holder.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-holder.Process.Pid, syscall.SIGKILL); _ = holder.Wait() })
	h.setConfig(fleetFixture(t, h) + "slot_globs = { local = \"" + shared + "/fq-[0-9]*.lock\" }\n")
	if rc, out := h.run("3", "--remote", "local", "--lane", "gate", "--", "true"); rc != 0 { // one heavyq run in the ledger
		t.Fatalf("rc=%d\n%s", rc, out)
	}
	time.Sleep(200 * time.Millisecond) // flock has long taken its lock; this only spares a loaded machine

	rc, out := h.run("fleet", "--json", "--timeout", "45s")
	if rc != 0 {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
	type capQueue struct {
		Owner      string `json:"owner"`
		BusyNow    int    `json:"busy_now"`
		WindowPeak int    `json:"window_peak"`
		Configured int    `json:"configured"`
		Counted    int    `json:"counted"`
		Basis      string `json:"basis"`
	}
	var r struct {
		Machines []struct {
			Name  string
			Slots []struct {
				Owner       string
				Busy, Total int
			}
			Cap *struct {
				Workers    int        `json:"workers"`
				Cores      int        `json:"cores"`
				HeavySlots int        `json:"heavy_slots"`
				Queues     []capQueue `json:"queues"`
				Why        string     `json:"why"`
			} `json:"worker_cap"`
		}
	}
	if err := json.Unmarshal([]byte(out[strings.Index(out, "{"):]), &r); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	caps, slots := map[string]map[string]capQueue{}, map[string]string{}
	for _, m := range r.Machines {
		if m.Cap == nil {
			continue
		}
		caps[m.Name] = map[string]capQueue{}
		for _, q := range m.Cap.Queues {
			caps[m.Name][q.Owner] = q
		}
		for _, s := range m.Slots {
			if s.Owner == "bashq" {
				slots[m.Name] = fmt.Sprintf("%d/%d", s.Busy, s.Total)
			}
		}
		if !strings.Contains(m.Cap.Why, "heavy slots in use") || m.Cap.Workers != max(1, m.Cap.Cores/max(1, m.Cap.HeavySlots)) {
			t.Errorf("%s: cap %+v", m.Name, m.Cap)
		}
	}
	if slots["local"] != "1/2" {
		t.Errorf("local bashq slots from the glob: %q, want 1/2 (fq-l1 and other-1 are not its heavy slots)\n%s", slots["local"], out)
	}
	want := map[string]map[string]capQueue{
		"local": { // heavyq: one run in the window, 1 heavy slot; bashq: no machine file, one glob slot busy
			"heavyq": {Owner: "heavyq", WindowPeak: 1, Configured: 1, Counted: 1, Basis: "used"},
			"bashq":  {Owner: "bashq", BusyNow: 1, Counted: 1, Basis: "used"},
		},
		"watch": { // a running ticket there, ten overlapping runs, bounded by its 3 SLOTS; heavyq is not there
			"bashq": {Owner: "bashq", BusyNow: 1, WindowPeak: 10, Configured: 3, Counted: 3, Basis: "used"},
		},
	}
	for m, qs := range want {
		if len(caps[m]) != len(qs) {
			t.Errorf("%s: cap queues %+v, want %+v", m, caps[m], qs)
		}
		for owner, q := range qs {
			if caps[m][owner] != q {
				t.Errorf("%s %s: %+v, want %+v", m, owner, caps[m][owner], q)
			}
		}
	}
}

// A foreign history that cannot be found, or whose lines all fail to parse, is no history: the worker cap
// counts the queue's configured slots and the report says why. Taking either for "ran nothing" would count 0
// and suggest more workers than the machine can carry.
func TestE2EFleetCapFallsBackWhenAForeignHistoryIsUnusable(t *testing.T) {
	h := newRig(t, "")
	now := time.Now().Unix()
	line := fmt.Sprintf("%d\tlocal\tapi\t1\t0\t5\t60\t0\n", now-120)
	var cfg strings.Builder
	cfg.WriteString(baseConfig)
	for _, q := range []struct{ name, history, extra string }{
		{"typo", "", ""}, // the path names no file
		{"badcols", line + line, `columns = ["machine", "exit", "end_epoch"]` + "\n"}, // every line fails to parse
		{"torn", "half a li\n" + line, ""},                                            // one torn line beside a run: still a history
	} {
		dir := filepath.Join(h.root, q.name)
		if err := os.MkdirAll(filepath.Join(dir, "machines.d"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "machines.d", "local.env"), []byte("SLOTS=\"s1 s2\"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if q.history != "" {
			if err := os.WriteFile(filepath.Join(dir, "history.tsv"), []byte(q.history), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		fmt.Fprintf(&cfg, "[[foreign]]\nname = %q\nhistory = %q\nmachines_dir = %q\n%s", q.name,
			filepath.Join(dir, "history.tsv"), filepath.Join(dir, "machines.d"), q.extra)
	}
	h.setConfig(cfg.String())
	rc, out := h.run("fleet", "--json", "--timeout", "45s")
	if rc != 0 {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
	var r struct {
		Machines []struct {
			Name string
			Cap  *struct {
				Queues []struct {
					Owner, Basis string
					Counted      int
					WindowPeak   int `json:"window_peak"`
				} `json:"queues"`
			} `json:"worker_cap"`
		}
		Notes []string `json:"notes"`
	}
	if err := json.Unmarshal([]byte(out[strings.Index(out, "{"):]), &r); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	got := map[string]string{}
	for _, m := range r.Machines {
		if m.Name == "local" && m.Cap != nil {
			for _, q := range m.Cap.Queues {
				got[q.Owner] = fmt.Sprintf("%s %d peak %d", q.Basis, q.Counted, q.WindowPeak)
			}
		}
	}
	want := map[string]string{"typo": "configured 2 peak 0", "badcols": "configured 2 peak 0", "torn": "used 1 peak 1"}
	for owner, w := range want {
		if got[owner] != w {
			t.Errorf("local %s: %q, want %q\n%s", owner, got[owner], w, out)
		}
	}
	notes := strings.Join(r.Notes, "\n")
	for _, w := range []string{"typo history: open ", "badcols history: none of the 2 lines read parsed",
		"torn history: 1 lines did not parse"} {
		if !strings.Contains(notes, w) {
			t.Errorf("notes lack %q:\n%s", w, notes)
		}
	}
	if strings.Count(notes, "the worker cap counts its configured slots") != 2 {
		t.Errorf("the two unusable histories must say the cap falls back:\n%s", notes)
	}
}
