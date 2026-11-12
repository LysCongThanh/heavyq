package fleet

import (
	"sort"
	"time"
)

// Assemble turns what was measured and read into the report. It is a pure function of its inputs.
func Assemble(in Inputs) Report {
	window := in.Now.Sub(in.Since)
	r := Report{Schema: ReportSchema, GeneratedAt: in.Now, Since: in.Since, WindowS: window.Seconds(),
		Machines: []Machine{}, Queues: []Queue{}, Notes: append([]string{}, in.Notes...)}
	topN := in.TopN
	if topN <= 0 {
		topN = DefaultTopN
	}
	for _, m := range in.Machines {
		r.Machines = append(r.Machines, assembleMachine(m, in.Queues, window, topN))
	}
	for _, q := range in.Queues {
		r.Queues = append(r.Queues, assembleQueue(q, in.Now, window))
	}
	return r
}

func assembleMachine(in MachineInput, queues []QueueInput, window time.Duration, topN int) Machine {
	m := Machine{Name: in.Name, Role: in.Role, OverloadWhy: []string{}, Slots: []SlotUse{}, Top: []Group{}}
	var mine []Run
	for _, q := range queues {
		for _, r := range q.Runs {
			if r.Machine == in.Name {
				mine = append(mine, r)
			}
		}
	}
	m.Window = Summarize(mine, window)
	if in.Probe == nil {
		if in.Err != nil {
			m.Error = clip(in.Err.Error(), 160)
		}
		return m
	}
	p := in.Probe
	m.Reachable, m.Partial = true, p.Partial
	m.Cores, m.Load1, m.FreeMB, m.TotalMB, m.Pressure = p.Cores, p.Load1, p.FreeMB, p.TotalMB, p.Pressure
	if p.Cores > 0 {
		m.LoadPerCore = round2(p.Load1 / float64(p.Cores))
	}
	m.OverloadWhy = append(m.OverloadWhy, Overload(Sample{Cores: p.Cores, Load1: p.Load1, FreeMB: p.FreeMB, TotalMB: p.TotalMB, Pressure: p.Pressure})...)
	m.Overload = len(m.OverloadWhy) > 0

	groups, outside := Attribute(*p, rootsOf(in, queues))
	m.Top, m.OutsideRSS = append(m.Top, groups[:min(topN, len(groups))]...), outside // [] when nothing was measured
	var caps []SlotCount
	for _, q := range queues {
		heavy, total := q.HeavySlots[in.Name], q.TotalSlots[in.Name]
		if q.Kind == KindHeavyq {
			heavy, total = in.HeavyqSlots, in.HeavyqTotal
		}
		caps = append(caps, SlotCount{Owner: q.Name, Heavy: heavy})
		if u := slotUse(q, in.Name, p.Slots, total); u.Busy > 0 || u.Total > 0 {
			m.Slots = append(m.Slots, u)
		}
	}
	if c, ok := SuggestCap(p.Cores, caps); ok {
		m.Cap = &c
	}
	return m
}

// rootsOf lists the processes known to belong to a queue on this machine: every ticket's own process when
// it is the local machine (ticket dirs are local), and the holder of each busy slot lock.
func rootsOf(in MachineInput, queues []QueueInput) []Root {
	var roots []Root
	for _, q := range queues {
		for _, t := range q.Tickets {
			if in.Role == RoleLocal && t.Pid > 0 {
				roots = append(roots, Root{Pid: t.Pid, Owner: q.Name, Lane: t.Lane})
			}
		}
	}
	for _, s := range in.Probe.Slots {
		if s.Holder <= 0 {
			continue
		}
		root := Root{Pid: s.Holder, Owner: s.Owner}
		for _, q := range queues {
			for _, t := range q.Tickets {
				if q.Name == s.Owner && t.Running && t.Machine == in.Name && t.Slot == s.Name {
					root.Lane = t.Lane
				}
			}
		}
		roots = append(roots, root)
	}
	return roots
}

// slotUse counts a queue's busy slots on a machine: from its slot locks when they were probed, else from
// its running tickets that say they run there.
func slotUse(q QueueInput, machine string, probed []SlotState, total int) SlotUse {
	u := SlotUse{Owner: q.Name, Total: total}
	files := 0
	for _, s := range probed {
		if s.Owner == q.Name {
			files++
			if s.Busy {
				u.Busy++
			}
		}
	}
	if files > 0 {
		if u.Total == 0 {
			u.Total = files
		}
		return u
	}
	for _, t := range q.Tickets {
		if t.Running && t.Machine == machine {
			u.Busy++
		}
	}
	return u
}

func assembleQueue(in QueueInput, now time.Time, window time.Duration) Queue {
	q := Queue{Name: in.Name, Kind: in.Kind, Window: Summarize(in.Runs, window), Machines: []MachineStats{},
		Running: []LiveTicket{}, Waiting: []LiveTicket{}, Stale: in.Stale, Truncated: in.Truncated}
	byMachine := map[string][]Run{}
	for _, r := range in.Runs {
		byMachine[r.Machine] = append(byMachine[r.Machine], r)
	}
	for name, runs := range byMachine {
		q.Machines = append(q.Machines, MachineStats{Machine: name, Window: Summarize(runs, window)})
	}
	sort.Slice(q.Machines, func(i, j int) bool {
		a, b := q.Machines[i], q.Machines[j]
		if a.Window.Runs != b.Window.Runs {
			return a.Window.Runs > b.Window.Runs
		}
		return a.Machine < b.Machine
	})
	for _, t := range in.Tickets {
		lt := LiveTicket{Lane: t.Lane, Prio: t.Prio, Machine: t.Machine, Slot: t.Slot, Cmd: t.Cmd, WaitS: secs(now.Sub(t.Since))}
		if !t.Running {
			q.Waiting = append(q.Waiting, lt)
			continue
		}
		if !t.Claimed.IsZero() {
			lt.WaitS, lt.RunS = secs(t.Claimed.Sub(t.Since)), secs(now.Sub(t.Claimed))
		}
		q.Running = append(q.Running, lt)
	}
	sort.SliceStable(q.Waiting, func(i, j int) bool { return q.Waiting[i].WaitS > q.Waiting[j].WaitS })
	return q
}

func secs(d time.Duration) float64 { return max(0, d.Round(time.Second).Seconds()) }
