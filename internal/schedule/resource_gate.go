package schedule

import (
	"fmt"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/metrics"
	"github.com/LysCongThanh/heavyq/internal/queue"
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
	Forced  bool // head ticket waited past force_after: the load check is skipped, never RAM
	Light   bool // the ticket is --light (smaller RAM budget)
}

// ramReasonPrefix starts every reason the gate gives for missing RAM.
const ramReasonPrefix = "RAM "

// IsRAMReason reports whether a wait reason is the gate's RAM check, the
// one the anti-starvation bypass never skips.
func IsRAMReason(why string) bool { return strings.HasPrefix(why, ramReasonPrefix) }

// BypassApplies reports whether the anti-starvation bypass may apply to t:
// only a ticket with one candidate machine has nowhere else to go. A ticket
// that may run on several machines waits for whichever has room, so the
// bypass never pushes it onto a machine that is over its load limit.
func BypassApplies(t *queue.Ticket) bool { return len(t.Candidates) <= 1 }

// Gate decides whether machine st may admit one more job. It returns "" when
// admitted, else the reason it waits.
//
// An idle machine (nothing of ours running, no slot busy) always admits: the
// load is then caused by things outside the queue, and refusing would starve
// the queue forever. The anti-starvation bypass (Forced) skips the load check
// only: admitting a job without the RAM for it can take down the jobs that
// already run there, and the wait it saves is bounded anyway (the head ticket
// keeps the machine for itself, our jobs there end, an idle machine admits).
func Gate(st MachineState, in GateInput) string {
	if !st.Reachable {
		return "unreachable"
	}
	if !in.Enabled || !st.Metrics.Known {
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
			return fmt.Sprintf(ramReasonPrefix+"%d MB (minus %d MB held for just-started jobs) < %d MB needed", st.Metrics.FreeMB, st.ReservedMB, need)
		}
		return fmt.Sprintf(ramReasonPrefix+"%d MB < %d MB needed (job %d + reserve %d)", st.Metrics.FreeMB, need, budget, st.Cfg.ReserveMB)
	}
	if in.Forced {
		return ""
	}
	limit := st.Cfg.MaxLoadRatio * float64(st.Metrics.Cores)
	if st.Metrics.Load1 >= limit {
		return fmt.Sprintf("load %.2f >= %.1f (%.2f x %d cores)", st.Metrics.Load1, limit, st.Cfg.MaxLoadRatio, st.Metrics.Cores)
	}
	return ""
}
