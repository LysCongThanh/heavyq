package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/procinfo"
	"github.com/LysCongThanh/heavyq/internal/queue"
	"github.com/LysCongThanh/heavyq/internal/schedule"
)

const (
	cpuSampleInterval = 500 * time.Millisecond
	outsideQueue      = "outside the queue"
	minShownCPU       = 1.0
	maxShownGroups    = 5
)

// loadHolders attributes current CPU use to the lanes of running tickets by
// walking each process's ancestry up to a ticket's pid.
func loadHolders(snap queue.Snapshot) string {
	if !procinfo.Default.Available() {
		return "unavailable on this platform (no /proc)"
	}
	roots := map[int]string{}
	for _, t := range snap.Running {
		roots[t.Pid] = "lane " + t.Lane
	}
	groups := procinfo.Default.Attribute(roots, outsideQueue, cpuSampleInterval)
	var parts []string
	for _, g := range groups {
		if g.Percent < minShownCPU || len(parts) == maxShownGroups {
			break
		}
		parts = append(parts, fmt.Sprintf("%s %.0f%%", g.Group, g.Percent))
	}
	if len(parts) == 0 {
		return "idle (no group above 1% CPU)"
	}
	return strings.Join(parts, ", ")
}

// printHeadReasons explains, per candidate machine, why the first waiting
// ticket has not started.
func printHeadReasons(w io.Writer, cfg *config.Config, snap queue.Snapshot, probes *probeCache) {
	head := snap.Waiting[0]
	opps, blocked := schedule.Opportunities(head, nil, slotCounts(cfg))
	waited := time.Since(head.Since)
	c, reasons := evaluate(evalInput{cfg: cfg, ticket: head, snap: snap, probes: probes,
		opps: opps, headWait: waited, dry: true})
	for m, why := range blocked {
		reasons[m] = why
	}
	fmt.Fprintf(w, "HEAD TICKET %s (p%d, %s) waits because:\n", head.Lane, head.Prio, head.Target)
	if c != nil {
		reasons[c.machine] = fmt.Sprintf("nothing blocks it now; it claims %s on its next poll", c.slot)
	}
	names := make([]string, 0, len(reasons))
	for m := range reasons {
		names = append(names, m)
	}
	sort.Strings(names)
	for _, m := range names {
		fmt.Fprintf(w, "  %-10s %s\n", m+":", reasons[m])
	}
	if left := cfg.Queue.ForceAfter.Duration - waited; left > 0 && cfg.GateEnabled() {
		fmt.Fprintf(w, "  (resource gate is bypassed in %s: anti-starvation)\n", fmtDur(left))
	}
}
