package stance

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const ellipsis = "…"

// Render produces the compact injection text for s: a fixed header naming
// the stance and why it applies now, the most important lines of the body,
// and a pointer to the full text. The result never exceeds
// s.InjectTokens x BytesPerToken bytes (DefaultInjectTokens when unset, at
// most MaxInjectTokens) and is always valid UTF-8.
//
// Line choice: list items first, then headings, then prose lines; within a
// tier, document order; a line that does not fit is skipped, never cut. The
// chosen lines are emitted in document order. When not even one line fits
// (one long paragraph), the first line is cut at a character boundary with
// an ellipsis. why is evidence, not trusted text: it is flattened to one
// line and limited to a third of the budget.
func Render(s Stance, why string) string {
	budget := renderBudget(s.InjectTokens)
	footer := "full text: heavyq stance show " + s.Name
	prefix := "heavyq stance: " + s.Name
	if why = oneLine(why); why != "" {
		prefix += " — why now: "
		why = cutRunes(why, min(budget/3, budget-len(prefix)-len(footer)-1))
	}
	header := strings.TrimRight(prefix+why, " ")

	room := budget - len(header) - len(footer) - 2 // the two newlines
	lines := pickLines(s.Body, room)

	parts := []string{header}
	parts = append(parts, lines...)
	parts = append(parts, footer)
	out := strings.Join(parts, "\n")
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

// pickLines chooses body lines that fit in room bytes (each costs its length
// plus a newline), in document order.
func pickLines(body string, room int) []string {
	type cand struct {
		idx, tier int
		text      string
	}
	var cands []cand
	for i, raw := range strings.Split(body, "\n") {
		text := strings.TrimRightFunc(raw, unicode.IsSpace)
		if strings.TrimSpace(text) == "" || strings.HasPrefix(strings.TrimSpace(text), "```") {
			continue
		}
		cands = append(cands, cand{i, lineTier(text), text})
	}
	byTier := make([]cand, len(cands))
	copy(byTier, cands)
	sort.SliceStable(byTier, func(i, j int) bool { return byTier[i].tier < byTier[j].tier })

	var chosen []cand
	for _, c := range byTier {
		if cost := len(c.text) + 1; cost <= room {
			chosen = append(chosen, c)
			room -= cost
		}
	}
	if len(chosen) == 0 {
		if len(cands) > 0 && room > len(ellipsis)+1 {
			return []string{cutRunes(strings.TrimSpace(cands[0].text), room-1)}
		}
		return nil
	}
	sort.Slice(chosen, func(i, j int) bool { return chosen[i].idx < chosen[j].idx })
	out := make([]string, len(chosen))
	for i, c := range chosen {
		out[i] = c.text
	}
	return out
}

// lineTier ranks a body line: 0 list item, 1 heading, 2 prose.
func lineTier(line string) int {
	t := strings.TrimSpace(line)
	switch {
	case strings.HasPrefix(t, "- "), strings.HasPrefix(t, "* "), strings.HasPrefix(t, "+ "), numberedItem(t):
		return 0
	case strings.HasPrefix(t, "#"):
		return 1
	}
	return 2
}

// numberedItem reports whether t starts like "3. " or "3) ".
func numberedItem(t string) bool {
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
