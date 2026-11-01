package cli

import (
	"os"
	"path/filepath"
	"time"

	"github.com/LysCongThanh/heavyq/internal/cache"
	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/procinfo"
	"github.com/LysCongThanh/heavyq/internal/queue"
	"github.com/LysCongThanh/heavyq/internal/redact"
	"github.com/LysCongThanh/heavyq/internal/snapshot"
)

// maxOutsideGroups bounds the "outside the queue" list (largest first).
const maxOutsideGroups = 8

// buildSnapshot assembles the machine-readable state of the queue. It
// probes remotes (cached by probes) and samples CPU for cpuInterval.
func buildSnapshot(cfg *config.Config, probes *probeCache, cpuInterval time.Duration) (snapshot.Snapshot, error) {
	q, err := queue.Open(QueueDir())
	if err != nil {
		return snapshot.Snapshot{}, err
	}
	var remotes []string
	for _, r := range cfg.Remotes {
		remotes = append(remotes, r.Name)
	}
	probes.refresh(cfg, remotes) // before reading the queue: probes may be slow
	q.Prune(procinfo.Default.Alive)
	qs, err := q.List()
	if err != nil {
		return snapshot.Snapshot{}, err
	}
	host, _ := os.Hostname()
	s := snapshot.Snapshot{
		Schema: snapshot.Schema, Version: version(), Host: host,
		Config: snapshot.Config{
			Path: cfg.Path, ResourceGate: cfg.GateEnabled(),
			ForceAfterS: cfg.Queue.ForceAfter.Seconds(), HoldForS: cfg.Queue.HoldFor.Seconds(),
			PollS: cfg.Queue.Poll.Seconds(), RemoteDownMaxS: cfg.Queue.RemoteDownMax.Seconds(),
		},
		Machines: []snapshot.Machine{}, Running: []snapshot.Running{}, Waiting: []snapshot.Waiting{},
		Coalesced: []snapshot.Group{}, CPU: snapshot.CPU{Lanes: []snapshot.Usage{}, Outside: []snapshot.Usage{}},
	}
	perTicket := sampleUsage(&s.CPU, qs, cpuInterval)
	s.GeneratedAt = time.Now()

	s.Machines = append(s.Machines, machineSnapshot(cfg, cfg.Local, qs, probes))
	for _, r := range cfg.Remotes {
		s.Machines = append(s.Machines, machineSnapshot(cfg, r, qs, probes))
	}
	box := controlBox()
	for _, t := range qs.Running {
		u := perTicket[t.Name]
		s.Running = append(s.Running, snapshot.Running{
			Ticket: ticketSnapshot(t, s.GeneratedAt, box.CancelRequested(t.Name)), Machine: t.Machine, Slot: t.Slot,
			ClaimedAt: t.ClaimedAt, RunS: s.GeneratedAt.Sub(t.ClaimedAt).Seconds(), Forced: t.Forced,
			CPUPct: u.CPUPct, RSSMB: u.RSSMB,
		})
	}
	s.Waiting = waitingSnapshots(cfg, qs, probes, s.GeneratedAt)
	s.Coalesced = coalescedSnapshot()
	s.Story = snapshot.Story(s, nil, s.GeneratedAt)
	return s, nil
}

// ticketSnapshot converts the shared fields of a queue ticket.
func ticketSnapshot(t *queue.Ticket, now time.Time, cancel bool) snapshot.Ticket {
	cands := t.Candidates
	if cands == nil {
		cands = []string{}
	}
	return snapshot.Ticket{
		ID: t.Name, Pid: t.Pid, Lane: t.Lane, Prio: t.Prio, Light: t.Light, Target: t.Target,
		Candidates: cands, Cmd: redact.Command(t.Cmd), Since: t.Since,
		WaitS: waitSecs(t, now), CancelRequested: cancel,
	}
}

// waitSecs is how long t waited: until its claim when running, else so far.
func waitSecs(t *queue.Ticket, now time.Time) float64 {
	if t.Running && !t.ClaimedAt.IsZero() {
		return t.ClaimedAt.Sub(t.Since).Seconds()
	}
	return now.Sub(t.Since).Seconds()
}

// sampleUsage fills cpu (lanes and outside the queue) and returns the usage
// of each running local ticket's process tree, keyed by ticket name.
func sampleUsage(cpu *snapshot.CPU, qs queue.Snapshot, interval time.Duration) map[string]procinfo.GroupUsage {
	out := map[string]procinfo.GroupUsage{}
	if !procinfo.Default.Available() {
		return out
	}
	cpu.Available, cpu.IntervalMS = true, int(interval.Milliseconds())
	roots, lanes := map[int]string{}, map[string]string{}
	for _, t := range qs.Running {
		if t.Machine == config.LocalName {
			roots[t.Pid] = t.Name
			lanes[t.Name] = t.Lane
		}
	}
	byRoot, outside := procinfo.Default.Breakdown(roots, interval)
	byLane := map[string]*snapshot.Usage{}
	var order []string
	for _, g := range byRoot {
		out[g.Group] = g
		lane := lanes[g.Group]
		u := byLane[lane]
		if u == nil {
			u = &snapshot.Usage{Name: lane}
			byLane[lane] = u
			order = append(order, lane)
		}
		u.Procs, u.CPUPct, u.RSSMB = u.Procs+g.Procs, u.CPUPct+g.CPUPct, u.RSSMB+g.RSSMB
	}
	for _, l := range order {
		cpu.Lanes = append(cpu.Lanes, *byLane[l])
	}
	cpu.OutsideTotal.Name = "outside"
	for i, g := range outside {
		u := snapshot.Usage{Name: g.Group, Procs: g.Procs, CPUPct: g.CPUPct, RSSMB: g.RSSMB}
		if i < maxOutsideGroups {
			cpu.Outside = append(cpu.Outside, u)
		}
		cpu.OutsideTotal.Procs += u.Procs
		cpu.OutsideTotal.CPUPct += u.CPUPct
		cpu.OutsideTotal.RSSMB += u.RSSMB
	}
	return out
}

// coalescedSnapshot lists identical runs waiting for one result.
func coalescedSnapshot() []snapshot.Group {
	f := &cache.Flight{Dir: filepath.Join(QueueDir(), "inflight")}
	out := []snapshot.Group{}
	party := func(p cache.Party) snapshot.Party {
		return snapshot.Party{Lane: p.Lane, Pid: p.Pid, Cmd: redact.Command(p.Cmd), Since: p.Since}
	}
	for _, g := range f.Groups(procinfo.Default.Alive) {
		sg := snapshot.Group{Followers: []snapshot.Party{}}
		if g.HasLeader {
			l := party(g.Leader)
			sg.Leader = &l
		}
		for _, p := range g.Followers {
			sg.Followers = append(sg.Followers, party(p))
		}
		out = append(out, sg)
	}
	return out
}
