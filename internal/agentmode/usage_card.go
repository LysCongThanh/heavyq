package agentmode

import "strings"

// Budgets of the agent-facing text, enforced by tests. A token is about four
// bytes, so 900 bytes is the 200-token card the roadmap promises, and `why`
// stays within the 150 tokens (~600 bytes) it is allowed in agent mode.
const (
	CardMaxBytes = 900
	WhyMaxBytes  = 600
)

// Card is `heavyq help agents`: everything an agent needs to use heavyq
// without reading the manual.
const Card = `heavyq agent card
run     heavyq <1-9> [--cache] -- <cmd>   (lower = sooner; the hook adds it)
output  your command's output, then a digest:
          heavyq: PASS|FAIL|REUSED|NOT RUN <cmd> · tree <id> · <time>
          failure lines, "log: <file>" (full output), and on any
          non-success exactly one "next:" line. Do what it says.
REUSED  same command already passed on this exact tree: do not re-run.
exit    the command's own, else: 75 queue busy past --max-wait: retry
        later, never bypass heavyq; 78 cancelled by an operator: ask
        first; 76/77 remote down: retry or --remote local; 2 usage.
proof   heavyq gate status [name]  (exit 0 = proven on this exact code)
        heavyq gate run [name]     (runs what is unproven)
item    heavyq item claim --next | done <id> --run | block <id> "needs: x"
brief   heavyq brief  (goal, your items, next)
why     heavyq why [lane]
`

// CapLines keeps whole lines of text while they fit in max bytes (newlines
// included). When lines are dropped the last line says so with more, which
// also fits. A single first line longer than max is clipped.
func CapLines(lines []string, max int, more string) []string {
	total := 0
	for _, l := range lines {
		total += len(l) + 1
	}
	if total <= max {
		return lines
	}
	budget := max - len(more) - 1
	var out []string
	used := 0
	for _, l := range lines {
		if used+len(l)+1 > budget {
			break
		}
		out = append(out, l)
		used += len(l) + 1
	}
	if len(out) == 0 && len(lines) > 0 {
		out = append(out, clipTo(strings.TrimRight(lines[0], "\n"), max-len(more)-2))
	}
	return append(out, more)
}
