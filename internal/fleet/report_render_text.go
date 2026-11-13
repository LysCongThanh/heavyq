package fleet

import (
	"fmt"
	"io"
	"strings"
)

// RenderText writes the human report: short, one block per machine and per queue, lines under 120 columns.
func RenderText(w io.Writer, r Report) {
	fmt.Fprintf(w, "heavyq fleet · %s · window %s · machines %d · queues %d\n",
		r.GeneratedAt.Format("2006-01-02 15:04 MST"), fmtWindow(r.WindowS), len(r.Machines), len(r.Queues))
	fmt.Fprintf(w, "\n%-14s %-12s %5s %6s %5s %13s %6s  %s\n", "MACHINE", "ROLE", "CORES", "LOAD", "L/C", "FREE/TOTAL", "PSI", "SLOTS busy/total")
	for _, m := range r.Machines {
		textMachine(w, m)
	}
	fmt.Fprintf(w, "\n%-14s %5s %6s %-17s %7s %6s %-11s %4s %4s %5s\n",
		"QUEUE", "RUNS", "RUN-H", "WAIT p50/p90/p95", "RUN p50", "RUNS/H", "RED inf/cod", "RUN", "WAIT", "STALE")
	for _, q := range r.Queues {
		textQueue(w, q)
	}
	for _, n := range r.Notes {
		fmt.Fprintf(w, "note: %s\n", clip(n, 110))
	}
}

func textMachine(w io.Writer, m Machine) {
	name := clip(m.Name, 14)
	if !m.Reachable {
		fmt.Fprintf(w, "%-14s %-12s unreachable: %s\n", name, m.Role, clip(m.Error, 80))
		return
	}
	state := ""
	if m.Overload {
		state = "  OVERLOAD"
	}
	fmt.Fprintf(w, "%-14s %-12s %5d %6.1f %5.2f %13s %6s  %s%s\n", name, m.Role, m.Cores, m.Load1, m.LoadPerCore,
		fmtGiB(m.FreeMB)+"/"+fmtGiB(m.TotalMB), pressureText(m.Pressure), clip(slotsText(m.Slots), 36), state)
	if m.Overload {
		fmt.Fprintf(w, "  ! %s\n", clip(strings.Join(m.OverloadWhy, "; "), 112))
	}
	if m.Partial != "" {
		fmt.Fprintf(w, "  partial: %s\n", clip(m.Partial, 108))
	}
	var top []string
	for i, g := range m.Top {
		if i == 3 {
			break
		}
		top = append(top, groupText(g, 24))
	}
	if len(top) > 0 {
		fmt.Fprintf(w, "  top: %s\n", clip(strings.Join(top, " · "), 110))
	}
	if m.OutsideRSS > 0 {
		fmt.Fprintf(w, "  outside every queue: %s RSS\n", fmtGiB(m.OutsideRSS))
	}
	if m.Cap != nil {
		// The number and the division first, then one line per queue term: the terms grow with the queues and
		// a clipped line must never lose the worker count.
		fmt.Fprintf(w, "  cap: %d workers per run = %s\n", m.Cap.Workers, clip(m.Cap.division(), 80))
		for _, t := range m.Cap.terms() {
			fmt.Fprintf(w, "    %s\n", clip(t, 110))
		}
	}
	if m.CapUnknown != "" {
		fmt.Fprintf(w, "  cap: unknown, %s\n", clip(m.CapUnknown, 100))
	}
}

func textQueue(w io.Writer, q Queue) {
	s := q.Window
	fmt.Fprintf(w, "%-14s %5d %6.1f %-17s %7s %6.1f %-11s %4d %4d %5d\n", clip(q.Name, 14), s.Runs, s.RunHours,
		fmtDur(s.WaitP50S)+"/"+fmtDur(s.WaitP90S)+"/"+fmtDur(s.WaitP95S), fmtDur(s.RunP50S), s.PerHour,
		fmtPct(s.InfraRate)+"/"+fmtPct(s.CodeRate), len(q.Running), len(q.Waiting), q.Stale)
	var parts []string
	for i, m := range q.Machines {
		if i == 4 {
			parts = append(parts, fmt.Sprintf("+%d more", len(q.Machines)-4))
			break
		}
		parts = append(parts, fmt.Sprintf("%s %d (p50 wait %s, red %s/%s)", clip(m.Machine, 14), m.Window.Runs,
			fmtDur(m.Window.WaitP50S), fmtPct(m.Window.InfraRate), fmtPct(m.Window.CodeRate)))
	}
	if len(parts) > 0 {
		fmt.Fprintf(w, "  by machine: %s\n", clip(strings.Join(parts, " · "), 104))
	}
	if len(q.Waiting) > 0 {
		fmt.Fprintf(w, "  longest waits: %s\n", clip(longestWaits(q.Waiting, 3, 28), 104))
	}
	if q.Truncated {
		fmt.Fprintln(w, "  history read up to its byte bound: the window is partial")
	}
}
