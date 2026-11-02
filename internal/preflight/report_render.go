package preflight

import (
	"fmt"
	"io"
	"strings"
	"unicode"
)

// maxRenderedFindings keeps the text inside an agent's output budget; the rest
// are counted and left to --json.
const maxRenderedFindings = 20

// Render writes the human form: the verdict, the plan in one line, then the
// numbered findings. Each ends with the command that fixes it, after "->".
func (r Report) Render(w io.Writer) {
	blocks := r.Blocks()
	warns := len(r.Findings) - blocks
	switch {
	case len(r.Findings) == 0:
		fmt.Fprintf(w, "preflight: %s\n", r.Verdict)
	default:
		fmt.Fprintf(w, "preflight: %s (%d to handle now, %d to watch)\n", r.Verdict, blocks, warns)
	}
	goal := oneLine(r.Goal, 100)
	if goal == "" {
		goal = "(none)"
	}
	fmt.Fprintf(w, "goal: %s\n", goal)
	plan := fmt.Sprintf("%d items, %d lanes", len(r.Items), r.Parallel)
	if r.Hours > 0 {
		plan += ", " + trimFloat(r.Hours) + " h"
	}
	fmt.Fprintln(w, "plan: "+plan)
	shown := r.Findings
	if len(shown) > maxRenderedFindings {
		shown = shown[:maxRenderedFindings]
	}
	for i, f := range shown {
		mark := ""
		if f.Severity == Warn {
			mark = "(watch) "
		}
		fmt.Fprintf(w, "%d. %s[%s] %s -> %s\n", i+1, mark, f.Check, oneLine(f.Text, 220), oneLine(f.Fix, 160))
	}
	if n := len(r.Findings) - len(shown); n > 0 {
		fmt.Fprintf(w, "... %d more (heavyq preflight --json)\n", n)
	}
	for _, s := range r.Skipped {
		fmt.Fprintf(w, "skipped %s\n", oneLine(s, 160))
	}
}

// oneLine makes text that came from a file safe to print as one line: control
// characters dropped, clipped to n bytes.
func oneLine(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	return clipText(strings.TrimSpace(s), n)
}
