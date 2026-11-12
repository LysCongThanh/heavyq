package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/pools"
	"github.com/LysCongThanh/heavyq/internal/queue"
	"github.com/LysCongThanh/heavyq/internal/remote"
	"github.com/LysCongThanh/heavyq/internal/schedule"
	"github.com/LysCongThanh/heavyq/internal/slots"
	"github.com/LysCongThanh/heavyq/internal/snapshot"
)

// machineSnapshot describes machine m: resources, slots, and whether a heavy
// job could start there now.
func machineSnapshot(cfg *config.Config, m config.MachineConfig, qs queue.Snapshot, probes *probeCache) snapshot.Machine {
	v := buildView(cfg, m.Name, qs, probes, cfg.Queue.HoldFor.Duration)
	met := v.state.Metrics
	out := snapshot.Machine{
		Name: m.Name, Reachable: v.state.Reachable, Paused: controlBox().Paused(m.Name),
		MetricsKnown: met.Known, Cores: met.Cores, Load1: met.Load1, FreeMB: met.FreeMB, TotalMB: met.TotalMB,
		ReservedMB: v.state.ReservedMB, JobMB: m.JobMB, LightMB: m.LightMB, ReserveMB: m.ReserveMB,
		Slots: []snapshot.Slot{},
	}
	if met.Known {
		out.MaxLoad = m.MaxLoadRatio * float64(met.Cores)
	}
	if !v.state.Reachable {
		out.Admits = "unreachable"
		return out
	}
	held := map[string]*queue.Ticket{}
	for _, t := range qs.Running {
		if t.Machine == m.Name && t.Slot != "" {
			held[t.Slot] = t
		}
	}
	freeHeavy := false
	for _, light := range []bool{false, true} {
		kind := schedule.Heavy
		if light {
			kind = schedule.Light
		}
		for _, name := range slotNamesOf(cfg, m, light) {
			sl := snapshot.Slot{Name: name, Kind: kind.String(), State: snapshot.SlotFree}
			switch t := held[name]; {
			case t != nil:
				sl.State, sl.Ticket, sl.Lane = snapshot.SlotHeld, t.Name, t.Lane
			case slotBusyOutside(cfg, m, name, v):
				sl.State = snapshot.SlotOutside
			case !light:
				freeHeavy = true
			}
			out.Slots = append(out.Slots, sl)
		}
	}
	switch {
	case out.Paused:
		out.Admits = pausedReason
	case !freeHeavy:
		out.Admits = "all heavy slots busy"
	default:
		out.Admits = schedule.Gate(v.state, schedule.GateInput{Enabled: cfg.GateEnabled()})
	}
	return out
}

// slotNamesOf lists the heavy or light slot names of machine m.
func slotNamesOf(cfg *config.Config, m config.MachineConfig, light bool) []string {
	if !m.IsLocal() {
		return remote.SlotNames(m, light)
	}
	var out []string
	for _, p := range localSlotPaths(cfg, light) {
		out = append(out, slotName(p))
	}
	return out
}

// slotBusyOutside reports a slot locked by something that is not one of our
// running tickets (a legacy `flock` user, or a remote job we don't track).
func slotBusyOutside(cfg *config.Config, m config.MachineConfig, name string, v machineView) bool {
	if !m.IsLocal() {
		return v.busy[name]
	}
	for _, light := range []bool{false, true} {
		for _, p := range localSlotPaths(cfg, light) {
			if slotName(p) == name {
				return slots.Busy(p)
			}
		}
	}
	return false
}

// waitingSnapshots explains, for every waiting ticket and each of its
// candidate machines, why it has not started.
func waitingSnapshots(cfg *config.Config, qs queue.Snapshot, probes *probeCache, now time.Time, set *pools.Set) []snapshot.Waiting {
	out := []snapshot.Waiting{}
	box := controlBox()
	for i, t := range qs.Waiting {
		ahead := qs.Waiting[:i]
		opps, blocked, head := admissionOpportunities(cfg, t, ahead, set)
		w := snapshot.Waiting{Ticket: ticketSnapshot(t, now, box.CancelRequested(t.Name)), Position: i + 1,
			Reasons: map[string]string{}, BypassInS: -1}
		for m, why := range blocked {
			if why != notInConfigReason {
				why = behindReason(ahead, m)
			}
			w.Reasons[m] = why
		}
		// The bypass clock starts when the ticket became head (its owner
		// publishes that), not when it was enqueued.
		var waited time.Duration
		if head && !t.HeadSince.IsZero() {
			waited = now.Sub(t.HeadSince)
		}
		if len(opps) > 0 {
			c, reasons := evaluate(evalInput{cfg: cfg, ticket: t, snap: qs, probes: probes,
				opps: opps, headWait: waited, dry: true, poolSet: set})
			for m, why := range reasons {
				w.Reasons[m] = why
			}
			if c != nil {
				w.Reasons[c.machine] = startReason(cfg, t)
			}
		}
		if head && cfg.GateEnabled() && bypassCanHelp(t, w.Reasons) { // the bypass skips the load check only
			w.BypassInS = max(0, (cfg.Queue.ForceAfter.Duration - waited).Seconds())
		}
		out = append(out, w)
	}
	return out
}

// behindReason names the earlier tickets that compete for machine m.
func behindReason(ahead []*queue.Ticket, m string) string {
	const shown = 3
	var names []string
	n := 0
	for _, t := range ahead {
		if !t.Uses(m) {
			continue
		}
		if n++; n <= shown {
			names = append(names, fmt.Sprintf("%s (p%d)", t.Lane, t.Prio))
		}
	}
	if n > shown {
		names = append(names, fmt.Sprintf("+%d more", n-shown))
	}
	return "behind " + strings.Join(names, ", ")
}
