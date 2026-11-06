package cli

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/control"
	"github.com/LysCongThanh/heavyq/internal/metrics"
	"github.com/LysCongThanh/heavyq/internal/pools"
	"github.com/LysCongThanh/heavyq/internal/queue"
	"github.com/LysCongThanh/heavyq/internal/remote"
	"github.com/LysCongThanh/heavyq/internal/schedule"
	"github.com/LysCongThanh/heavyq/internal/slots"
)

// claim is a slot obtained (or, in dry mode, obtainable) on a machine.
type claim struct {
	machine string
	slot    string
	lock    *slots.Lock  // local slots only; nil in dry mode
	lease   *pools.Lease // pool values held with the slot; nil without needs and in dry mode
	score   int
	forced  bool // the load check would have refused without the bypass
	// reading is the machine as it was when this claim was made (ledger).
	reading admissionReading
}

// releaseAll gives back the slot and the pool values of the claim. Safe on nil.
func (c *claim) releaseAll() {
	if c == nil {
		return
	}
	c.lease.Release()
	c.lock.Release()
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
	dry      bool       // status: report only, never keep a lock
	poolSet  *pools.Set // pools the ticket's needs refer to; nil without needs
}

// evaluate gates every opportunity, obtains a slot where possible and
// returns the best-scoring claim (ties go to the earlier candidate, i.e.
// local first). A ticket whose target is a list of machines takes the first
// claim in the list's order instead: the operator stated a preference, and
// the gate of each machine already refused those without room. Every machine
// not chosen gets a reason.
func evaluate(in evalInput) (*claim, map[string]string) {
	reasons := map[string]string{}
	ordered := config.IsMachineList(in.ticket.Target)
	passedOver := "lower placement score"
	if ordered {
		passedOver = "later in the --remote order"
	}
	var best *claim
	for _, op := range in.opps {
		if controlBox().Paused(op.Machine) {
			reasons[op.Machine] = pausedReason
			continue
		}
		v := buildView(in.cfg, op.Machine, in.snap, in.probes, in.cfg.Queue.HoldFor.Duration)
		forced := op.Head && schedule.BypassApplies(in.ticket) && in.headWait >= in.cfg.Queue.ForceAfter.Duration
		gate := schedule.GateInput{Enabled: in.cfg.GateEnabled(), Forced: forced, Light: in.ticket.Light}
		why := schedule.Gate(v.state, gate)
		if why != "" {
			reasons[op.Machine] = why
			continue
		}
		c, why := pickSlot(in.cfg, v, op, in.dry)
		if c == nil {
			reasons[op.Machine] = why
			continue
		}
		// The slot and every need are taken together or not at all.
		if op.Machine == config.LocalName {
			if why := claimPools(in, c); why != "" {
				c.releaseAll()
				reasons[op.Machine] = why
				continue
			}
		}
		c.reading = readingOf(v.state, in.ticket.Light)
		if forced {
			gate.Forced = false
			c.forced = schedule.Gate(v.state, gate) != ""
		}
		c.score = metrics.Score(v.state.Metrics, in.cfg.Queue.LoadWeightMB, in.needsRAM)
		if best == nil || (!ordered && c.score > best.score) {
			if best != nil {
				best.releaseAll()
				reasons[best.machine] = passedOver
			}
			best = c
		} else {
			c.releaseAll()
			reasons[op.Machine] = passedOver
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

// pausedReason is the wait reason on a machine paused by an operator.
const pausedReason = "paused (heavyq resume to admit again)"

// controlBox is the operator request mailbox of the queue directory.
func controlBox() control.Mailbox { return control.Mailbox{Dir: filepath.Join(QueueDir(), "ctl")} }
