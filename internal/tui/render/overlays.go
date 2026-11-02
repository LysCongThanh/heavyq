package render

import (
	"strings"

	"github.com/LysCongThanh/heavyq/internal/tui/width"
)

// storySection is the story sentences, wrapped, at most maxLines lines.
func (c *ctx) storySection(w, maxLines int) []bodyLine {
	var lines []string
	for _, s := range c.s.Story {
		lines = append(lines, width.Wrap(c.th.txt(s.Text), w-2)...)
	}
	var out []bodyLine
	for i, t := range lines {
		if i == maxLines-1 && len(lines) > maxLines {
			t = width.Truncate(t+" "+c.th.g(gEll), w-2, c.th.g(gEll))
			out = append(out, c.plainLine(Seg{t, 0}))
			break
		}
		out = append(out, c.plainLine(Seg{t, 0}))
	}
	return out
}

// helpLines is the key reference.
func (c *ctx) helpLines(w int) []bodyLine {
	rows := [][2]string{
		{"j k  arrows", "move the selection"},
		{"g G", "first and last row"},
		{"Tab", "jump to the next section"},
		{"Enter", "why: the full explanation for the selected lane"},
		{"/", "filter lanes (Enter keeps it, Esc clears it)"},
		{"m", "next display mode: " + strings.Join(modeNames[:], ", ")},
		{"r", "refresh now"},
		{"?", "this help"},
		{"q  Ctrl-C", "quit"},
		{"y", "copy the selected command to the clipboard (OSC 52)"},
	}
	if c.v.Control {
		rows = append(rows,
			[2]string{"c", "cancel the selected job (asks y/n first)"},
			[2]string{"+ -", "raise or lower a waiting job's priority"},
			[2]string{"p", "pause or resume the selected machine"})
	} else {
		rows = append(rows, [2]string{"c + - p", "need --control: the same requests as heavyq dash"})
	}
	out := []bodyLine{{Line: c.heading("KEYS", 0, w)}}
	for _, r := range rows {
		out = append(out, c.plainLine(c.seg(Bold, width.Pad(r[0], 14)), c.seg(0, r[1])))
	}
	return out
}

// whyLines is the explanation of one lane or exception, wrapped.
func (c *ctx) whyLines(w int) []bodyLine {
	out := []bodyLine{{Line: c.heading("WHY "+c.v.WhyTitle, 0, w)}}
	for _, raw := range c.v.WhyLines {
		raw = c.th.txt(raw)
		indent := len(raw) - len(strings.TrimLeft(raw, " "))
		for _, t := range width.Wrap(strings.TrimLeft(raw, " "), w-2-indent) {
			out = append(out, c.plainLine(blanks(indent), Seg{t, 0}))
		}
	}
	return out
}

// hint is a key and what it does.
type hint struct{ key, label string }

// footer is the last line: a prompt, the filter being typed, a flash, or the keys.
func (c *ctx) footer(mode Mode, w int) Line {
	switch {
	case c.v.Prompt != "":
		return Line{c.seg(Warn|Bold, c.v.Prompt), blanks(2), c.seg(Bold, "y"), c.seg(0, "/"), c.seg(Bold, "n")}
	case c.v.Typing:
		return Line{c.seg(Bold, "/ "), c.seg(0, c.v.Filter), c.seg(Bold, "_"), blanks(2),
			c.seg(Dim, "Enter keeps  Esc clears")}
	case c.v.Overlay != OverlayNone:
		return Line{c.seg(Bold, "Esc"), c.seg(Dim, " back  "), c.seg(Bold, "q"), c.seg(Dim, " quit")}
	}
	var lead Line
	if c.v.Flash != "" {
		lead = Line{c.seg(Good, c.v.Flash), blanks(2)}
	} else if c.v.Filter != "" {
		lead = Line{c.seg(0, "filter "), c.seg(Bold, c.v.Filter), c.seg(Dim, " (Esc clears)"), blanks(2)}
	}
	enter := c.th.g(gEnter)
	hints := []hint{{enter, "why"}, {"c", "cancel"}, {"+/-", "prio"}, {"p", "pause"}, {"/", "filter"},
		{"m", "mode " + mode.String()}, {"?", "help"}, {"q", "quit"}}
	if !c.v.Control {
		hints = []hint{{enter, "why"}, {"/", "filter"}, {"m", "mode " + mode.String()}, {"?", "help"}, {"q", "quit"}}
	}
	// Drop hints from the left of the list first (cancel, prio, pause go before help and quit).
	for {
		l := append(Line{}, lead...)
		for i, h := range hints {
			if i > 0 {
				l = append(l, blanks(2))
			}
			l = append(l, c.seg(Bold, h.key), c.seg(Dim, " "+h.label))
		}
		if l.Width() <= w || len(hints) <= 2 {
			if tail := "  read-only: --control to act"; !c.v.Control && l.Width()+cellWidth(tail) <= w {
				l = append(l, c.seg(Dim, tail))
			}
			return l
		}
		hints = append(hints[:dropIndex(hints)], hints[dropIndex(hints)+1:]...)
	}
}

// dropIndex picks the least important hint: the first of these that is present.
func dropIndex(hs []hint) int {
	for _, key := range []string{"m", "/", "p", "+/-", "c"} {
		for i, h := range hs {
			if h.key == key {
				return i
			}
		}
	}
	return 0
}
