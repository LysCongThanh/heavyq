package schedule

import (
	"fmt"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/metrics"
)

// MachineState is everything the gate needs to know about one machine.
type MachineState struct {
	Cfg       config.MachineConfig
	Metrics   metrics.Metrics
	Reachable bool
	// Running is the number of queue tickets currently running there.
	Running int
	// BusySlots counts slots held outside our knowledge (remote probe).
	BusySlots int
	// ReservedMB is RAM promised to jobs claimed less than hold_for ago.
	ReservedMB int
}

// GateInput bundles the ticket-side inputs of the gate.
type GateInput struct {
	Enabled bool // queue.resource_gate
	Forced  bool // head ticket waited past force_after
	Light   bool // the ticket is --light (smaller RAM budget)
}

// Gate decides whether machine st may admit one more job. It returns "" when
// admitted, else the reason it waits.
//
// An idle machine (nothing of ours running, no slot busy) always admits: the
// load is then caused by things outside the queue, and refusing would starve
// the queue forever.
func Gate(st MachineState, in GateInput) string {
	if !st.Reachable {
		return "unreachable"
	}
	if !in.Enabled || in.Forced || !st.Metrics.Known {
		return ""
	}
	if st.Running == 0 && st.BusySlots == 0 {
		return ""
	}
	budget := st.Cfg.JobMB
	if in.Light {
		budget = st.Cfg.LightMB
	}
	need := budget + st.Cfg.ReserveMB
	if eff := st.Metrics.FreeMB - st.ReservedMB; eff < need {
		if st.ReservedMB > 0 {
			return fmt.Sprintf("RAM %d MB (minus %d MB held for just-started jobs) < %d MB needed", st.Metrics.FreeMB, st.ReservedMB, need)
		}
		return fmt.Sprintf("RAM %d MB < %d MB needed (job %d + reserve %d)", st.Metrics.FreeMB, need, budget, st.Cfg.ReserveMB)
	}
	limit := st.Cfg.MaxLoadRatio * float64(st.Metrics.Cores)
	if st.Metrics.Load1 >= limit {
		return fmt.Sprintf("load %.2f >= %.1f (%.2f x %d cores)", st.Metrics.Load1, limit, st.Cfg.MaxLoadRatio, st.Metrics.Cores)
	}
	return ""
}
