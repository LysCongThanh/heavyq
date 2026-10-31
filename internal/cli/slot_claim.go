package cli

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/metrics"
	"github.com/LysCongThanh/heavyq/internal/queue"
	"github.com/LysCongThanh/heavyq/internal/remote"
	"github.com/LysCongThanh/heavyq/internal/schedule"
	"github.com/LysCongThanh/heavyq/internal/slots"
)

// claim is a slot obtained (or, in dry mode, obtainable) on a machine.
type claim struct {
	machine string
	slot    string
	lock    *slots.Lock // local slots only; nil in dry mode
	score   int
}

// evalInput is one scheduling round for one ticket.
type evalInput struct {
	cfg      *config.Config
	ticket   *queue.Ticket
	snap     queue.Snapshot
	probes   *probeCache
	opps     []schedule.Opportunity
	headWait time.Duration
	needsRAM bool
	dry      bool // status: report only, never keep a lock
}

// evaluate gates every opportunity, obtains a slot where possible and
// returns the best-scoring claim (ties go to the earlier candidate, i.e.
// local first). Every machine not chosen gets a reason.
func evaluate(in evalInput) (*claim, map[string]string) {
	reasons := map[string]string{}
	var best *claim
	for _, op := range in.opps {
		v := buildView(in.cfg, op.Machine, in.snap, in.probes, in.cfg.Queue.HoldFor.Duration)
		forced := op.Head && in.headWait >= in.cfg.Queue.ForceAfter.Duration
		why := schedule.Gate(v.state, schedule.GateInput{Enabled: in.cfg.GateEnabled(), Forced: forced, Light: in.ticket.Light})
		if why != "" {
			reasons[op.Machine] = why
			continue
		}
		c, why := pickSlot(in.cfg, v, op, in.dry)
		if c == nil {
			reasons[op.Machine] = why
			continue
		}
		c.score = metrics.Score(v.state.Metrics, in.cfg.Queue.LoadWeightMB, in.needsRAM)
		if best == nil || c.score > best.score {
			if best != nil {
				best.lock.Release()
				reasons[best.machine] = "lower placement score"
			}
			best = c
		} else {
			c.lock.Release()
			reasons[op.Machine] = "lower placement score"
		}
	}
	return best, reasons
}

// pickSlot finds a free slot of the allowed kinds on the machine of v.
func pickSlot(cfg *config.Config, v machineView, op schedule.Opportunity, dry bool) (*claim, string) {
	var tried []string
	for _, k := range op.Kinds {
		light := k == schedule.Light
		if v.state.Cfg.IsLocal() {
			for _, p := range localSlotPaths(cfg, light) {
				name := slotName(p)
				if dry {
					if !slots.Busy(p) {
						return &claim{machine: op.Machine, slot: name}, ""
					}
					continue
				}
				l, err := slots.TryLock(p)
				if err != nil {
					return nil, err.Error()
				}
				if l != nil {
					return &claim{machine: op.Machine, slot: name, lock: l}, ""
				}
			}
		} else {
			for _, s := range remote.SlotNames(v.state.Cfg, light) {
				if !v.busy[s] {
					return &claim{machine: op.Machine, slot: s}, ""
				}
			}
		}
		tried = append(tried, k.String())
	}
	return nil, fmt.Sprintf("all %s slots busy", strings.Join(tried, "/"))
}

func slotName(path string) string { return strings.TrimSuffix(filepath.Base(path), ".lock") }
