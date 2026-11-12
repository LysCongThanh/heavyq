package cli

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/fleet"
	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/procinfo"
	"github.com/LysCongThanh/heavyq/internal/queue"
	"github.com/LysCongThanh/heavyq/internal/redact"
)

// heavyqInfraExits are heavyq's own codes that are not the command's verdict (docs/exit-codes.md): 77 remote
// preparation failed. 75 and 76 never ran (gave up) and 78 was cancelled; their records are left out or
// classed by their result.
var heavyqInfraExits = []int{77}

// maxLedgerRecords bounds the ledger tail a fleet report reads (it reads by time window, from the end).
const maxLedgerRecords = 200_000

// collectFleet reads every queue and probes every machine (in parallel, each within timeout), then
// assembles the report. Nothing here writes: tickets are not pruned, slots are not taken.
func collectFleet(cfg *config.Config, now time.Time, window, timeout time.Duration) fleet.Report {
	since := now.Add(-window)
	var notes []string
	queues := []fleet.QueueInput{heavyqQueueInput(since, &notes)}
	for _, f := range cfg.Foreign {
		queues = append(queues, foreignQueueInput(f, since, &notes))
	}
	machines := probeFleet(cfg, timeout)
	return fleet.Assemble(fleet.Inputs{Now: now, Since: since, Machines: machines, Queues: queues, Notes: notes})
}

// heavyqQueueInput is heavyq's own queue: the ledger's window and the live tickets of the queue dir.
func heavyqQueueInput(since time.Time, notes *[]string) fleet.QueueInput {
	q := fleet.QueueInput{Name: fleet.OwnerHeavyq, Kind: fleet.KindHeavyq}
	recs, err := history.ReadTail(history.DefaultPath(), history.TailLimit{Since: since, Max: maxLedgerRecords})
	if err != nil {
		*notes = append(*notes, "heavyq ledger: "+err.Error())
	}
	q.History = err == nil // a missing ledger is a history without runs
	q.Truncated = len(recs) >= maxLedgerRecords
	class := fleet.NewClassifier(heavyqInfraExits)
	for _, r := range recs {
		if r.Outcome() == history.OutcomeGaveUp || r.Cache == "hit" || r.Cache == "coalesced" {
			continue // never ran here: no run time, no verdict
		}
		run := fleet.Run{Queue: q.Name, Machine: r.Machine, Lane: r.Lane, Prio: r.Prio, Light: r.Light, End: r.End,
			WaitS: r.WaitSecs, RunS: r.RunSecs, Exit: r.Exit, Class: class.Class(r.Exit)}
		if o := r.Outcome(); o == history.OutcomeCancelled || o == history.OutcomeInterrupted {
			run.Class = fleet.ClassInfra
		}
		q.Runs = append(q.Runs, run)
	}
	d := &queue.Dir{Path: QueueDir()} // List only: Open would create the dir, Prune would delete
	s, err := d.List()
	if err != nil {
		return q // no queue dir yet: nothing waits
	}
	for _, t := range append(append([]*queue.Ticket{}, s.Running...), s.Waiting...) {
		if !procinfo.Default.Alive(t.Pid, t.Start) {
			q.Stale++
			continue
		}
		q.Tickets = append(q.Tickets, fleet.Ticket{Queue: q.Name, Lane: t.Lane, Prio: t.Prio, Pid: t.Pid, Since: t.Since,
			Running: t.Running, Machine: t.Machine, Slot: t.Slot, Claimed: t.ClaimedAt, Cmd: redact.Command(t.Cmd)})
	}
	return q
}

// foreignQueueInput reads one [[foreign]] queue: its history window, live tickets and machine files.
// What cannot be read becomes a note; the report goes on.
func foreignQueueInput(f config.ForeignConfig, since time.Time, notes *[]string) fleet.QueueInput {
	q := fleet.QueueInput{Name: f.Name, Kind: fleet.KindForeign, HeavySlots: map[string]int{}, TotalSlots: map[string]int{}}
	note := func(what string, err error) { *notes = append(*notes, fmt.Sprintf("%s %s: %v", f.Name, what, err)) }
	if f.History != "" {
		h, err := fleet.ReadHistoryTSV(fleet.HistorySource{Path: f.History, Columns: f.Columns, Since: since})
		if err != nil {
			note("history", err)
		}
		q.History = err == nil
		class := fleet.NewClassifier(f.InfraExits)
		for _, r := range h.Runs {
			r.Queue, r.Machine, r.Class = f.Name, f.MachineName(r.Machine), class.Class(r.Exit)
			q.Runs = append(q.Runs, r)
		}
		q.Truncated = h.Truncated
		if h.Truncated {
			note("history", errors.New("read up to its byte bound: the window is partial"))
		}
	}
	if f.Tickets != "" {
		ts, err := fleet.ReadTickets(f.Tickets, nil)
		if err != nil {
			note("tickets", err)
		}
		for _, t := range ts.Tickets {
			t.Queue = f.Name
			if t.Machine != "" {
				t.Machine = f.MachineName(t.Machine)
			}
			q.Tickets = append(q.Tickets, t)
		}
		q.Stale = ts.Stale
	}
	if f.MachinesDir != "" {
		ms, err := fleet.ReadMachineFiles(f.MachinesDir)
		if err != nil {
			note("machines_dir", err)
		}
		for name, m := range ms {
			q.HeavySlots[f.MachineName(name)] = m.Heavy
			q.TotalSlots[f.MachineName(name)] = m.Heavy + m.Light
		}
	}
	return q
}

// probeFleet probes local, the remotes and the observed machines in parallel. A machine that does not
// answer within timeout is unreachable; it never holds the others.
func probeFleet(cfg *config.Config, timeout time.Duration) []fleet.MachineInput {
	type target struct {
		in  fleet.MachineInput
		req fleet.ProbeRequest
	}
	foreignSlots := func(machine string) []fleet.SlotDir {
		var out []fleet.SlotDir
		for _, f := range cfg.Foreign {
			if d, ok := f.SlotDirs[machine]; ok {
				out = append(out, fleet.SlotDir{Owner: f.Name, Dir: d})
			}
			if g, ok := f.SlotGlobs[machine]; ok {
				out = append(out, fleet.SlotGlob(f.Name, g))
			}
		}
		return out
	}
	l := cfg.Local
	targets := []target{{
		in:  fleet.MachineInput{Name: l.Name, Role: fleet.RoleLocal, HeavyqSlots: l.HeavySlots, HeavyqTotal: l.HeavySlots + l.LightSlots},
		req: fleet.ProbeRequest{Transport: []string{"sh", "-c"}, Slots: foreignSlots(l.Name)},
	}}
	for _, r := range cfg.Remotes {
		slots := append([]fleet.SlotDir{{Owner: fleet.OwnerHeavyq, Dir: strings.TrimSuffix(r.WorkDir, "/") + "/slots"}}, foreignSlots(r.Name)...)
		targets = append(targets, target{
			in:  fleet.MachineInput{Name: r.Name, Role: fleet.RoleRemote, HeavyqSlots: r.HeavySlots, HeavyqTotal: r.HeavySlots + r.LightSlots},
			req: fleet.ProbeRequest{Transport: r.Transport, WorkDir: r.WorkDir, Slots: slots},
		})
	}
	for _, r := range cfg.Observed {
		targets = append(targets, target{
			in:  fleet.MachineInput{Name: r.Name, Role: fleet.RoleObserveOnly},
			req: fleet.ProbeRequest{Transport: r.Transport, Slots: foreignSlots(r.Name)},
		})
	}
	out := make([]fleet.MachineInput, len(targets))
	var wg sync.WaitGroup
	for i, t := range targets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			t.req.Timeout = timeout
			p, err := fleet.RunProbe(t.req)
			t.in.Err = err
			if err == nil {
				t.in.Probe = &p
			}
			out[i] = t.in
		}()
	}
	wg.Wait()
	return out
}
