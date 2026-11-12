package cli

import (
	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/pools"
	"github.com/LysCongThanh/heavyq/internal/queue"
	"github.com/LysCongThanh/heavyq/internal/schedule"
	"github.com/LysCongThanh/heavyq/internal/snapshot"
)

// One admission decision, two callers: the waiting ticket's own round
// (run_wait_loop.go) claims what it finds, and status / why (dry) report it.
// Both reach evaluate through admissionOpportunities with the config they
// read, so status can only tell a different story when the waiter decides
// with a different config, which startReason says instead of a start.

// notInConfigReason is the wait reason on a candidate machine the config does
// not have: a remote removed by an edit while the ticket waits (it counts as
// unreachable, see checkRemoteDown), or one of another --config file.
const notInConfigReason = "not in this config"

// admissionOpportunities is where ticket t may try for a slot this round given
// the tickets ahead of it: tickets waiting for a pool it does not share are
// skipped (set: the pools they need; nil when none of them has needs), the
// other machines are blocked with the reason why (notInConfigReason for a
// candidate cfg does not have), and head says whether t is
// first in line for some machine (the anti-starvation clock runs).
func admissionOpportunities(cfg *config.Config, t *queue.Ticket, ahead []*queue.Ticket, set *pools.Set) (opps []schedule.Opportunity, blocked map[string]string, head bool) {
	opps, blocked = schedule.Opportunities(t, skipPoolBlocked(set, t, ahead), slotCounts(cfg))
	for _, m := range t.Candidates {
		if _, ok := cfg.Machine(m); !ok { // no slots in cfg: never an opportunity, and the reason is the config
			blocked[m] = notInConfigReason
		}
	}
	for _, op := range opps {
		head = head || op.Head
	}
	return opps, blocked, head
}

// startReason is what status says of the machine where evaluate found room for
// t: "" (it starts on its next poll) when t's owner decides with the config
// status read, else snapshot.OtherConfigReason, because that config may well
// refuse it (another --config file, or an edit its owner has not polled yet).
// A ticket from a heavyq that does not publish its config (ConfigSum "") keeps
// the old promise: there is nothing to compare.
func startReason(cfg *config.Config, t *queue.Ticket) string {
	if t.ConfigSum != "" && t.ConfigSum != cfg.Origin.Sum {
		return snapshot.OtherConfigReason
	}
	return ""
}
