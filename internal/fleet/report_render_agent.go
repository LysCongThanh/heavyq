package fleet

import (
	"fmt"
	"strings"
)

// AgentBudget is the most bytes RenderAgent prints (docs/agent-mode.md: a digest stays under 4 KB).
const AgentBudget = 3800

// RenderAgent is the compact digest for agent mode: one line per machine and per queue, overloaded
// machines first, plain ASCII-safe text, never more than AgentBudget bytes.
func RenderAgent(r Report) string {
	lines := []string{fmt.Sprintf("heavyq fleet %s window %s", r.GeneratedAt.UTC().Format("2006-01-02T15:04Z"), fmtWindow(r.WindowS))}
	var hot, rest []string
	var next []string
	for _, m := range r.Machines {
		l := agentMachine(m)
		if m.Overload {
			hot = append(hot, l)
			if m.Cap != nil {
				next = append(next, fmt.Sprintf("%s: cap test runners at %d workers per run", m.Name, m.Cap.Workers))
			}
		} else {
			rest = append(rest, l)
		}
	}
	lines = append(append(lines, hot...), rest...)
	for _, q := range r.Queues {
		s := q.Window
		l := fmt.Sprintf("queue %s: %d runs, wait p50 %s p90 %s, run p50 %s, red infra %s code %s, running %d waiting %d",
			q.Name, s.Runs, fmtDur(s.WaitP50S), fmtDur(s.WaitP90S), fmtDur(s.RunP50S), fmtPct(s.InfraRate), fmtPct(s.CodeRate), len(q.Running), len(q.Waiting))
		if len(q.Waiting) > 0 {
			l += " (longest " + longestWaits(q.Waiting, 2, 32) + ")"
		}
		lines = append(lines, l)
	}
	if len(next) > 0 {
		lines = append(lines, "next: "+strings.Join(next, "; "))
	}
	for _, n := range r.Notes {
		lines = append(lines, "note: "+clip(n, 160))
	}
	return capLines(lines, AgentBudget)
}

func agentMachine(m Machine) string {
	if !m.Reachable {
		return fmt.Sprintf("%s: unreachable (%s)", m.Name, clip(m.Error, 60))
	}
	state := fmt.Sprintf("ok, load/cores %.2f", m.LoadPerCore)
	if m.Overload {
		state = "OVERLOAD (" + strings.Join(m.OverloadWhy, "; ") + ")"
	}
	l := fmt.Sprintf("%s: %s, free %s/%s, slots %s", m.Name, state, fmtGiB(m.FreeMB), fmtGiB(m.TotalMB), slotsText(m.Slots))
	if m.Cap != nil {
		l += fmt.Sprintf(", cap %d", m.Cap.Workers)
	}
	if len(m.Top) > 0 {
		l += ", top " + groupText(m.Top[0], 24)
	}
	return l
}

// capLines joins lines until the budget, then says how many were left out.
func capLines(lines []string, budget int) string {
	var b strings.Builder
	for i, l := range lines {
		l = clip(l, 400)
		if b.Len()+len(l)+40 > budget {
			fmt.Fprintf(&b, "... %d more lines: heavyq fleet --json\n", len(lines)-i)
			break
		}
		b.WriteString(l + "\n")
	}
	return b.String()
}
