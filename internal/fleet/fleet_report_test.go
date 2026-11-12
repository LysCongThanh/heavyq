package fleet

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// sampleInputs is a fleet of two probed machines, one dead one and two queues, with enough live tickets
// to push every renderer against its budget.
func sampleInputs() Inputs {
	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	busy := &Probe{OS: "Linux", Cores: 18, Load1: 45, FreeMB: 900, TotalMB: 32000, Pressure: &Pressure{MemFullAvg10: 2},
		Procs: []Proc{{Pid: 100, PPid: 1, Threads: 40, CPU: 1650, RSSKB: 9 << 20, Comm: "node"}, {Pid: 200, PPid: 1, Threads: 3, CPU: 80, RSSKB: 4 << 20, Comm: "chrome"}},
		Args:  map[int]string{100: "node jest --maxWorkers=18 API_TOKEN=hunter2"},
		Slots: []SlotState{{Owner: "bashq", Name: "s1", Busy: true, Holder: 100}, {Owner: "bashq", Name: "s2"}}}
	calm := &Probe{OS: "Linux", Cores: 16, Load1: 3, FreeMB: 20000, TotalMB: 32000}
	in := Inputs{Now: now, Since: now.Add(-24 * time.Hour), TopN: 3,
		Machines: []MachineInput{
			{Name: "local", Role: RoleLocal, Probe: calm, HeavyqSlots: 2, HeavyqTotal: 3},
			{Name: "box", Role: RoleObserveOnly, Probe: busy},
			{Name: "gone", Role: RoleRemote, Err: fmt.Errorf("timed out after 10s"), HeavyqSlots: 2, HeavyqTotal: 2},
		}}
	bash := QueueInput{Name: "bashq", Kind: KindForeign, HeavySlots: map[string]int{"box": 4}, TotalSlots: map[string]int{"box": 6}}
	for i := 0; i < 60; i++ {
		bash.Runs = append(bash.Runs, Run{Queue: "bashq", Machine: "box", Lane: "api", WaitS: float64(60 * i), RunS: 900, Exit: i % 7, Class: map[bool]string{true: ClassOK, false: ClassCode}[i%7 == 0]})
		bash.Tickets = append(bash.Tickets, Ticket{Queue: "bashq", Lane: fmt.Sprintf("lane-with-a-long-name-%02d", i), Prio: 1, Pid: 1000 + i,
			Since: now.Add(-time.Duration(i) * time.Minute), Running: i == 0, Machine: "box", Slot: "s1", Cmd: "bash scripts/verify.sh"})
	}
	hq := QueueInput{Name: OwnerHeavyq, Kind: KindHeavyq, Runs: []Run{{Queue: OwnerHeavyq, Machine: "local", Lane: "gate", WaitS: 5, RunS: 60, Exit: 255, Class: ClassInfra}}}
	in.Queues = []QueueInput{hq, bash}
	return in
}

func TestAssembleFlagsOverloadAndSuggestsACap(t *testing.T) {
	r := Assemble(sampleInputs())
	if r.Schema != ReportSchema || len(r.Machines) != 3 || len(r.Queues) != 2 {
		t.Fatalf("shape: %+v", r)
	}
	box := r.Machines[1]
	if !box.Reachable || !box.Overload || box.LoadPerCore != 2.5 || box.Cap == nil || box.Cap.Workers != 4 {
		t.Fatalf("box: %+v cap %+v", box, box.Cap)
	}
	if len(box.Top) == 0 || box.Top[0].Owner != "bashq" || box.Top[0].Lane != "lane-with-a-long-name-00" || strings.Contains(box.Top[0].Cmd, "hunter2") {
		t.Fatalf("the slot holder's lane comes from the running ticket on that slot: %+v", box.Top)
	}
	if len(box.Slots) != 1 || box.Slots[0].Owner != "bashq" || box.Slots[0].Busy != 1 || box.Slots[0].Total != 6 {
		t.Fatalf("slots by owner: %+v", box.Slots)
	}
	if gone := r.Machines[2]; gone.Reachable || gone.Error == "" || gone.Overload {
		t.Fatalf("unreachable: %+v", gone)
	}
	if local := r.Machines[0]; local.Cap == nil || local.Cap.Workers != 8 {
		t.Fatalf("local: %+v", local.Cap)
	}
	q := r.Queues[1]
	if q.Window.Runs != 60 || len(q.Running) != 1 || len(q.Waiting) != 59 || q.Waiting[0].Lane != "lane-with-a-long-name-59" {
		t.Fatalf("queue: runs=%d running=%d waiting=%d first=%+v", q.Window.Runs, len(q.Running), len(q.Waiting), q.Waiting[0])
	}
	if box.Window.Runs != 60 || r.Machines[0].Window.Runs != 1 || r.Machines[0].Window.RedInfra != 1 {
		t.Fatalf("per machine windows: %+v / %+v", box.Window, r.Machines[0].Window)
	}
}

// Golden-free shape checks: the JSON has the documented keys, the markdown its header, tables and window.
func TestReportShapes(t *testing.T) {
	r := Assemble(sampleInputs())
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"schema", "generated_at", "since", "window_s", "machines", "queues", "notes"} {
		if _, ok := doc[k]; !ok {
			t.Errorf("json: no %q", k)
		}
	}
	m := doc["machines"].([]any)[1].(map[string]any)
	for _, k := range []string{"name", "role", "reachable", "cores", "load1", "load_per_core", "free_mb", "overload", "overload_why", "slots", "top", "worker_cap", "window"} {
		if _, ok := m[k]; !ok {
			t.Errorf("json machine: no %q", k)
		}
	}
	q := doc["queues"].([]any)[1].(map[string]any)
	for _, k := range []string{"name", "kind", "window", "machines", "running", "waiting"} {
		if _, ok := q[k]; !ok {
			t.Errorf("json queue: no %q", k)
		}
	}

	md := string(RenderMarkdown(r))
	for _, want := range []string{"# Fleet report", "Generated at: 2026-11-12T10:00:00Z", "Window: 24h", "## Machines", "## Queues", "OVERLOAD", "unreachable", "| machine |"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown: no %q\n%s", want, md)
		}
	}
	if strings.Contains(md, "hunter2") {
		t.Error("markdown leaks a secret")
	}

	var text bytes.Buffer
	RenderText(&text, r)
	for _, line := range strings.Split(text.String(), "\n") {
		if len([]rune(line)) > 120 {
			t.Errorf("text line too wide (%d): %q", len([]rune(line)), line)
		}
	}
	if !strings.Contains(text.String(), "OVERLOAD") || !strings.Contains(text.String(), "unreachable") {
		t.Errorf("text: %s", text.String())
	}

	digest := RenderAgent(r)
	if len(digest) >= 4096 || !strings.Contains(digest, "OVERLOAD") || strings.Contains(digest, "\x1b") {
		t.Fatalf("agent digest (%d bytes): %s", len(digest), digest)
	}
}

func TestWriteFileAtomicLeavesNoTemporary(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "fleet.md")
	for _, body := range []string{"one\n", "two\n"} {
		if err := WriteFileAtomic(p, []byte(body)); err != nil {
			t.Fatal(err)
		}
		if got, _ := os.ReadFile(p); string(got) != body {
			t.Fatalf("content %q, want %q", got, body)
		}
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 {
		t.Fatalf("leftovers: %v", ents)
	}
	if err := WriteFileAtomic(filepath.Join(dir, "missing", "x.md"), []byte("x")); err == nil {
		t.Fatal("a missing directory is an error")
	}
}
