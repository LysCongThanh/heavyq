package snapshot

import (
	"fmt"
	"strings"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/redact"
)

// LaneLines says what one lane is doing right now (`heavyq why <lane>`):
// its running and waiting tickets, the reason per candidate machine for each
// waiting one, and its last recorded run. last may be nil. Lines are plain
// text; command lines are redacted (again: a snapshot is already redacted,
// and redaction is idempotent).
func LaneLines(s Snapshot, lane string, last *history.Record, now time.Time) []string {
	elapsed := elapsedSince(s, now)
	var running []Running
	for _, t := range s.Running {
		if t.Lane == lane {
			running = append(running, t)
		}
	}
	var waiting []Waiting
	for _, t := range s.Waiting {
		if t.Lane == lane {
			waiting = append(waiting, t)
		}
	}
	var out []string
	switch {
	case len(running)+len(waiting) == 0:
		out = append(out, fmt.Sprintf("lane %s: not running and not waiting.", lane))
	default:
		var parts []string
		if len(running) > 0 {
			parts = append(parts, fmt.Sprintf("%d running", len(running)))
		}
		if len(waiting) > 0 {
			parts = append(parts, fmt.Sprintf("%d waiting", len(waiting)))
		}
		out = append(out, fmt.Sprintf("lane %s: %s.", lane, strings.Join(parts, ", ")))
	}
	for _, t := range running {
		out = append(out, runningLine(t, elapsed))
	}
	for _, t := range waiting {
		out = append(out, waitingLines(s, t, elapsed)...)
	}
	if last != nil {
		out = append(out, describeRun(*last, now))
	} else {
		out = append(out, "no run of this lane in the history yet.")
	}
	return out
}

func runningLine(t Running, elapsed float64) string {
	line := fmt.Sprintf("  running on %s/%s for %s (p%d, pid %d): %s",
		t.Machine, t.Slot, dur(t.RunS+elapsed), t.Prio, t.Pid, redact.OneLine(redact.Command(t.Cmd)))
	if t.Forced {
		line += " [admitted by the anti-starvation bypass]"
	}
	if t.CancelRequested {
		line += " [cancel requested]"
	}
	return line
}

// waitingLines is the ticket, then one indented line per candidate machine
// with the reason it cannot start there, then the bypass countdown if any.
func waitingLines(s Snapshot, t Waiting, elapsed float64) []string {
	line := fmt.Sprintf("  waiting #%d for %s (p%d, pid %d): %s", t.Position, dur(t.WaitS+elapsed), t.Prio, t.Pid, redact.OneLine(redact.Command(t.Cmd)))
	if t.CancelRequested {
		line += " [cancel requested]"
	}
	out := []string{line}
	for _, mr := range sortedReasons(t.Reasons) {
		why := mr.reason
		if why == "" {
			why = "starts on the next poll"
		}
		out = append(out, fmt.Sprintf("    %s: %s", mr.machine, why))
	}
	if b := bypassSentence(s, &t, elapsed); b != nil {
		out = append(out, "    "+b.Text)
	}
	return out
}

// describeRun is one line about a finished run.
func describeRun(r history.Record, now time.Time) string {
	var what string
	switch {
	case r.Outcome() == history.OutcomeGaveUp:
		what = "gave up after waiting " + dur(r.WaitSecs)
	case r.Outcome() == history.OutcomeCancelled:
		what = "was cancelled by an operator"
	case r.Outcome() == history.OutcomeInterrupted:
		what = "was interrupted"
	case r.Cache == "hit":
		what = "reused a cached result"
	case r.Cache == "coalesced":
		what = "shared the result of an identical run"
	case r.Exit == 0:
		what = "passed in " + dur(r.RunSecs)
	default:
		what = fmt.Sprintf("failed with exit %d after %s", r.Exit, dur(r.RunSecs))
	}
	where := ""
	if r.Machine != "" {
		where = " on " + r.Machine
	}
	return fmt.Sprintf("last run %s%s, %s ago: %s", what, where, dur(now.Sub(r.End).Seconds()), redact.OneLine(redact.Command(r.Cmd)))
}
