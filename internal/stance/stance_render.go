package stance

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	ellipsis       = "…"
	coreOpen       = "<!-- core -->"
	coreClose      = "<!-- /core -->"
	maxInjectLines = 3
)

// Render produces the compact injection text for s, facts first:
//
//	[heavyq · stance <name>] why now: <evidence>
//	next: <one command or check>          (only when the stance has one)
//	<at most 3 short core lines>
//	full: heavyq stance show <name>
//
// The result never exceeds s.InjectTokens x BytesPerToken bytes
// (DefaultInjectTokens when unset, at most MaxInjectTokens) and is always
// valid UTF-8. Core lines come from the body's <!-- core --> ... <!-- /core -->
// blocks; without one, from its first list items, else its first prose lines.
// A line that does not fit is skipped, never cut, unless nothing fits at all
// (one long paragraph): then the first line is cut with an ellipsis.
// why is evidence, not trusted text: it is flattened to one line and limited
// to a third of the budget; next gets a quarter.
func Render(s Stance, why string) string {
	budget := renderBudget(s.InjectTokens)
	footer := "full: heavyq stance show " + s.Name
	prefix := "[heavyq · stance " + s.Name + "] why now:"
	head := []string{prefix}
	if why = oneLine(why); why != "" {
		head[0] = prefix + " " + cutRunes(why, min(budget/3, budget-len(prefix)-len(footer)-1))
	}
	if next := oneLine(s.Next); next != "" {
		head = append(head, "next: "+cutRunes(next, min(budget/4, budget-len(head[0])-len(footer)-8)))
	}
	room := budget - len(strings.Join(head, "\n")) - len(footer) - 2
	parts := append(head, pickCore(s.Body, room)...)
	out := strings.Join(append(parts, footer), "\n")
	if len(out) > budget { // only reachable with absurd names; stay safe
		out = cutRunes(out, budget)
	}
	return out
}

func renderBudget(tokens int) int {
	if tokens <= 0 {
		tokens = DefaultInjectTokens
	}
	return min(tokens, MaxInjectTokens) * BytesPerToken
}

// pickCore chooses up to maxInjectLines body lines that fit in room bytes
// (each costs its length plus a newline), in document order.
func pickCore(body string, room int) []string {
	cands, _ := coreLines(body) // validated at parse time
	if len(cands) == 0 {
		cands = firstItems(body)
	}
	var out []string
	for _, l := range cands {
		if len(out) == maxInjectLines {
			break
		}
		if cost := len(l) + 1; cost <= room {
			out = append(out, l)
			room -= cost
		}
	}
	if len(out) == 0 && len(cands) > 0 && room > len(ellipsis)+1 {
		out = []string{cutRunes(cands[0], room-1)}
	}
	return out
}

// coreLines returns the non-blank lines inside <!-- core --> blocks, trimmed,
// and an error for markers that are nested, stray or unclosed.
func coreLines(body string) ([]string, error) {
	var out []string
	open := 0
	for i, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case line == coreOpen:
			if open != 0 {
				return nil, fmt.Errorf("body line %d: %s inside another core block", i+1, coreOpen)
			}
			open = i + 1
		case line == coreClose:
			if open == 0 {
				return nil, fmt.Errorf("body line %d: %s without %s", i+1, coreClose, coreOpen)
			}
			open = 0
		case open != 0 && line != "":
			out = append(out, line)
		}
	}
	if open != 0 {
		return nil, fmt.Errorf("body line %d: %s is never closed", open, coreOpen)
	}
	return out, nil
}

// firstItems is the fallback when a body has no core block: its list items,
// or, with none, its prose lines (headings, comments and code fences skipped).
func firstItems(body string) []string {
	var items, prose []string
	for _, raw := range strings.Split(body, "\n") {
		t := strings.TrimSpace(raw)
		switch {
		case t == "", strings.HasPrefix(t, "#"), strings.HasPrefix(t, "```"), strings.HasPrefix(t, "<!--"):
		case isListItem(t):
			items = append(items, t)
		default:
			prose = append(prose, t)
		}
	}
	if len(items) > 0 {
		return items
	}
	return prose
}

func isListItem(t string) bool {
	if strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ") || strings.HasPrefix(t, "+ ") {
		return true
	}
	i := 0
	for i < len(t) && t[i] >= '0' && t[i] <= '9' {
		i++
	}
	return i > 0 && i+1 < len(t) && (t[i] == '.' || t[i] == ')') && t[i+1] == ' '
}

// oneLine flattens all whitespace runs (newlines included) to single spaces.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// cutRunes returns s unchanged when it fits in limit bytes, else the longest
// prefix at a character boundary that fits together with an ellipsis (empty
// when even that does not fit).
func cutRunes(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	if limit < len(ellipsis) {
		return ""
	}
	cut := limit - len(ellipsis)
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return strings.TrimRightFunc(s[:cut], unicode.IsSpace) + ellipsis
}
