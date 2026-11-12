package fleet

import (
	"fmt"
	"strings"
	"time"
)

// Formatting shared by the text, markdown and agent renderers.

// fmtDur is a short duration: 45s, 43m, 1h28m, 2d3h.
func fmtDur(s float64) string {
	d := time.Duration(s) * time.Second
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd%dh", int(d.Hours())/24, int(d.Hours())%24)
	}
}

// fmtWindow is the window length as a flag would spell it: 24h, 90m, 7d.
func fmtWindow(s float64) string {
	d := time.Duration(s) * time.Second
	switch {
	case d%(24*time.Hour) == 0 && d >= 48*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours())/24)
	case d%time.Hour == 0:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
}

// fmtGiB renders MiB as GiB with one decimal.
func fmtGiB(mb int) string { return fmt.Sprintf("%.1fG", float64(mb)/1024) }

// fmtPct renders a share (0..1) as a whole percentage.
func fmtPct(v float64) string { return fmt.Sprintf("%.0f%%", v*100) }

// slotsText is "heavyq 1/3 bashq 4/6" ("?" for an unknown total).
func slotsText(us []SlotUse) string {
	var parts []string
	for _, u := range us {
		total := "?"
		if u.Total > 0 {
			total = fmt.Sprint(u.Total)
		}
		parts = append(parts, fmt.Sprintf("%s %d/%s", u.Owner, u.Busy, total))
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, " ")
}

// groupText is "node bashq/api 1650% 40thr 9.0G".
func groupText(g Group, laneWidth int) string {
	who := g.Owner
	if g.Lane != "" {
		who += "/" + clip(g.Lane, laneWidth)
	}
	thr := ""
	if g.Threads > 0 {
		thr = fmt.Sprintf(" %dthr", g.Threads)
	}
	n := ""
	if g.Procs > 1 {
		n = fmt.Sprintf("x%d", g.Procs)
	}
	return fmt.Sprintf("%s%s %s %.0f%%%s %s", clip(g.Comm, 16), n, who, g.CPUPct, thr, fmtGiB(g.RSSMB))
}

// pressureText is the memory "full avg10" share, or "-".
func pressureText(p *Pressure) string {
	if p == nil {
		return "-"
	}
	return fmt.Sprintf("%.1f%%", p.MemFullAvg10)
}

// mdCell makes s safe inside a markdown table cell.
func mdCell(s string) string {
	s = strings.NewReplacer("|", `\|`, "\n", " ", "\r", " ").Replace(s)
	if s == "" {
		return "-"
	}
	return s
}

// overloadRuleText documents the rule where the numbers are shown.
func overloadRuleText() string {
	return fmt.Sprintf("load1/cores >= %.1f, or free RAM < %.0f%% of total (< %d MB when the total is unknown), or memory pressure full avg10 >= %.0f%%",
		OverloadLoadPerCore, OverloadFreeShare*100, OverloadFreeFloorMB, OverloadMemFullAvg10)
}

// longestWaits is "lane 59m · lane 58m" for the first n waiting tickets.
func longestWaits(ws []LiveTicket, n, laneWidth int) string {
	var parts []string
	for i, w := range ws {
		if i == n {
			parts = append(parts, fmt.Sprintf("+%d more", len(ws)-n))
			break
		}
		parts = append(parts, clip(w.Lane, laneWidth)+" "+fmtDur(w.WaitS))
	}
	return strings.Join(parts, " · ")
}
