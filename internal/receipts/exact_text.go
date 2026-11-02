package receipts

import (
	"fmt"
	"strings"
	"time"
)

// Text is the exact summary for your own eyes: real counts, no rounding. It
// is still free of names, but it is not what to post: use Share for that.
func (r Receipts) Text(window string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "heavyq receipts · %s (%s to %s)\n", WindowLabel(window),
		r.From.Local().Format("2006-01-02 15:04"), r.To.Local().Format("2006-01-02 15:04"))
	row := func(label string, v any, note string) {
		fmt.Fprintf(&b, "  %-26s %v%s\n", label, v, note)
	}
	row("heavy runs", r.Runs, "")
	row("duplicate runs avoided", r.DuplicatesAvoided, fmt.Sprintf("  (%d cache hits, %d coalesced)", r.CacheHits, r.Coalesced))
	row("runs held back", r.HeldBack, fmt.Sprintf("  (waited %gs or more in the queue)", HeldBackMinWaitS))
	row("flaky", r.Flaky, "  (run ids that passed and failed, failure not explained by load)")
	row("failed under load", r.UnderLoad, "  (started over a limit of the resource gate)")
	row("remote runs", r.RemoteRuns, "  (on "+plural(r.RemoteMachines, "machine")+")")
	row("pool values handed out", r.PoolValues, "")
	row("wait p50 / p95", fmtSecs(r.WaitP50S)+" / "+fmtSecs(r.WaitP95S), "")
	row("agents (lanes) served", r.Lanes, "")
	return b.String()
}

// fmtSecs writes seconds compactly: 4s, 2m10s, 1h05m.
func fmtSecs(s float64) string {
	d := time.Duration(s * float64(time.Second)).Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}
