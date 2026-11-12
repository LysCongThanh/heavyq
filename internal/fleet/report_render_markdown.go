package fleet

import (
	"bytes"
	"fmt"
	"strings"
	"time"
)

// mdLiveRows bounds the live ticket tables of the markdown report.
const mdLiveRows = 15

// RenderMarkdown is the report for other agents and people to read: a header with the generation time and
// the data window, then machines and queues as tables. Every command line in it is redacted.
func RenderMarkdown(r Report) []byte {
	var b bytes.Buffer
	b.WriteString("# Fleet report\n\n")
	fmt.Fprintf(&b, "Generated at: %s · Window: %s (since %s) · Schema %d\n\n", r.GeneratedAt.UTC().Format(time.RFC3339),
		fmtWindow(r.WindowS), r.Since.UTC().Format(time.RFC3339), r.Schema)
	fmt.Fprintf(&b, "Overload rule: %s. Worker cap: cores / heavy slots of every queue on the machine.\n\n", overloadRuleText())

	b.WriteString("## Machines\n\n")
	b.WriteString("| machine | role | cores | load1 | load/cores | free / total RAM | mem pressure full | slots busy/total | worker cap | state |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|\n")
	for _, m := range r.Machines {
		if !m.Reachable {
			fmt.Fprintf(&b, "| %s | %s | - | - | - | - | - | - | - | unreachable: %s |\n", mdCell(m.Name), m.Role, mdCell(m.Error))
			continue
		}
		state, capText := "ok", "-"
		if m.Overload {
			state = "**OVERLOAD**: " + strings.Join(m.OverloadWhy, "; ")
		}
		if m.Partial != "" {
			state += "; partial: " + m.Partial
		}
		if m.Cap != nil {
			capText = fmt.Sprint(m.Cap.Workers)
		}
		fmt.Fprintf(&b, "| %s | %s | %d | %.1f | %.2f | %s / %s | %s | %s | %s | %s |\n", mdCell(m.Name), m.Role, m.Cores, m.Load1,
			m.LoadPerCore, fmtGiB(m.FreeMB), fmtGiB(m.TotalMB), pressureText(m.Pressure), mdCell(slotsText(m.Slots)), capText, mdCell(state))
	}
	for _, m := range r.Machines {
		mdMachineDetail(&b, m)
	}

	b.WriteString("\n## Queues\n\n")
	b.WriteString("| queue | kind | runs | run-hours | wait p50 | wait p90 | wait p95 | run p50 | runs/h | red infra | red code | running | waiting | stale |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, q := range r.Queues {
		s := q.Window
		fmt.Fprintf(&b, "| %s | %s | %d | %.1f | %s | %s | %s | %s | %.1f | %s | %s | %d | %d | %d |\n", mdCell(q.Name), q.Kind, s.Runs,
			s.RunHours, fmtDur(s.WaitP50S), fmtDur(s.WaitP90S), fmtDur(s.WaitP95S), fmtDur(s.RunP50S), s.PerHour,
			fmtPct(s.InfraRate), fmtPct(s.CodeRate), len(q.Running), len(q.Waiting), q.Stale)
	}
	for _, q := range r.Queues {
		mdQueueDetail(&b, q)
	}
	if len(r.Notes) > 0 {
		b.WriteString("\n## Notes\n\n")
		for _, n := range r.Notes {
			fmt.Fprintf(&b, "- %s\n", strings.ReplaceAll(n, "\n", " "))
		}
	}
	return b.Bytes()
}

func mdMachineDetail(b *bytes.Buffer, m Machine) {
	if !m.Reachable || (len(m.Top) == 0 && m.Cap == nil) {
		return
	}
	fmt.Fprintf(b, "\n### %s\n\n", mdCell(m.Name))
	if m.Cap != nil {
		fmt.Fprintf(b, "Suggested worker cap: **%d** per run (%s).\n\n", m.Cap.Workers, m.Cap.Why)
	}
	if len(m.Top) == 0 {
		return // not measured (a partial probe): no RSS figure either
	}
	fmt.Fprintf(b, "Outside every queue: %s RSS.\n\n", fmtGiB(m.OutsideRSS))
	b.WriteString("| owner | lane | comm | procs | threads | %CPU | RSS | command |\n|---|---|---|---|---|---|---|---|\n")
	for _, g := range m.Top {
		fmt.Fprintf(b, "| %s | %s | %s | %d | %d | %.0f | %s | %s |\n", mdCell(g.Owner), mdCell(g.Lane), mdCell(g.Comm),
			g.Procs, g.Threads, g.CPUPct, fmtGiB(g.RSSMB), mdCell(g.Cmd))
	}
}

func mdQueueDetail(b *bytes.Buffer, q Queue) {
	if len(q.Machines) == 0 && len(q.Running) == 0 && len(q.Waiting) == 0 {
		return
	}
	fmt.Fprintf(b, "\n### %s\n\n", mdCell(q.Name))
	if q.Truncated {
		b.WriteString("The history was read up to its byte bound: this window is partial.\n\n")
	}
	if len(q.Machines) > 0 {
		b.WriteString("| machine | runs | run-hours | wait p50 | wait p90 | run p50 | red infra | red code |\n|---|---|---|---|---|---|---|---|\n")
		for _, m := range q.Machines {
			s := m.Window
			fmt.Fprintf(b, "| %s | %d | %.1f | %s | %s | %s | %s | %s |\n", mdCell(m.Machine), s.Runs, s.RunHours,
				fmtDur(s.WaitP50S), fmtDur(s.WaitP90S), fmtDur(s.RunP50S), fmtPct(s.InfraRate), fmtPct(s.CodeRate))
		}
		b.WriteString("\n")
	}
	mdLive(b, "Longest waits now", q.Waiting)
	mdLive(b, "Running now", q.Running)
}

func mdLive(b *bytes.Buffer, title string, ts []LiveTicket) {
	if len(ts) == 0 {
		return
	}
	fmt.Fprintf(b, "%s (%d):\n\n| lane | prio | machine | slot | waited | ran | command |\n|---|---|---|---|---|---|---|\n", title, len(ts))
	for i, t := range ts {
		if i == mdLiveRows {
			fmt.Fprintf(b, "| +%d more | | | | | | |\n", len(ts)-mdLiveRows)
			break
		}
		ran := "-"
		if t.RunS > 0 {
			ran = fmtDur(t.RunS)
		}
		fmt.Fprintf(b, "| %s | %d | %s | %s | %s | %s | %s |\n", mdCell(t.Lane), t.Prio, mdCell(t.Machine), mdCell(t.Slot),
			fmtDur(t.WaitS), ran, mdCell(clip(t.Cmd, 100)))
	}
	b.WriteString("\n")
}
