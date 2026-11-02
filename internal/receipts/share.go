package receipts

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Share is the privacy-rounded form of Receipts: only ranges and words, no
// exact count, and no lane, repository, command, path or machine name. It is
// what `heavyq receipts --share` prints and the dashboard copies.
type Share struct {
	Version string   `json:"version"`
	Window  string   `json:"window"` // "last 7 days"
	Lines   []string `json:"lines"`
	// Text is the card: Lines in a box, at most 12 lines.
	Text string `json:"text"`
}

// NewShare rounds r. version is heavyq's version, window the name given to
// ParseWindow. Metrics that are zero are left out.
func NewShare(r Receipts, version, window string) Share {
	s := Share{Version: version, Window: WindowLabel(window)}
	s.Lines = shareLines(r)
	s.Text = card("heavyq receipts · "+s.Window, "heavyq "+version, s.Lines)
	return s
}

func shareLines(r Receipts) []string {
	if r.Runs == 0 && r.DuplicatesAvoided == 0 {
		return []string{"no heavy runs in this window"}
	}
	var l []string
	add := func(format string, a ...any) { l = append(l, fmt.Sprintf(format, a...)) }
	if perDay := float64(r.Runs) / max(r.Days(), 1e-9); perDay >= 1 {
		add("%s runs/day through the queue", approxRate(perDay))
	} else {
		add("%s runs in total through the queue", approxCount(r.Runs))
	}
	if r.DuplicatesAvoided > 0 {
		add("%s duplicate runs avoided", approxCount(r.DuplicatesAvoided))
	}
	if q := ratioRuns(r.HeldBack, r.Runs); q != "" {
		add("%s waited their turn", q)
	}
	if r.Runs > 0 {
		add("wait p50 %s · p95 %s", approxDuration(r.WaitP50S), approxDuration(r.WaitP95S))
	}
	if r.Flaky+r.UnderLoad > 0 {
		add("flagged: %s flaky, %s under load", approxCount(r.Flaky), approxCount(r.UnderLoad))
	}
	if r.RemoteRuns > 0 {
		add("%s runs offloaded to %s", approxCount(r.RemoteRuns), plural(r.RemoteMachines, "machine"))
	}
	if r.PoolValues > 0 {
		add("%s pool values handed out", approxCount(r.PoolValues))
	}
	if r.Lanes > 0 {
		add("%s served", plural(r.Lanes, "agent"))
	}
	return l
}

// card draws lines in a box with title in the top border and footer in the
// bottom one: len(lines)+2 rows.
func card(title, footer string, lines []string) string {
	inner := max(utf8.RuneCountInString(title), utf8.RuneCountInString(footer)) + 4
	for _, ln := range lines {
		inner = max(inner, utf8.RuneCountInString(ln)+2)
	}
	border := func(left, right, label string) string {
		return left + "─ " + label + " " + strings.Repeat("─", inner-utf8.RuneCountInString(label)-3) + right
	}
	var b strings.Builder
	b.WriteString(border("┌", "┐", title) + "\n")
	for _, ln := range lines {
		b.WriteString("│ " + ln + strings.Repeat(" ", inner-utf8.RuneCountInString(ln)-1) + "│\n")
	}
	b.WriteString(border("└", "┘", footer) + "\n")
	return b.String()
}

// plural writes "1 machine", "3 machines" (counts of 10 or more are rounded).
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return approxCount(n) + " " + noun + "s"
}
