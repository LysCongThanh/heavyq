// Package render draws the heavyq top screen. It is pure: a snapshot, a view
// state and a size go in, lines of text come out. No terminal, no clock, no
// I/O, so a frame is a value a test can compare.
//
// Text is built as Lines of styled Segs. Widths are measured on the text
// only (never on escape sequences), and SGR codes are added last, by
// Line.Render, with a reset at the end of every line.
package render

import (
	"strconv"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/tui/width"
)

// Style is a set of flags: attributes (Bold, Dim, Rev) plus at most one of
// three colour roles. Colour always comes with a glyph or a word, so the
// screen stays readable without it.
type Style uint8

// Style flags.
const (
	Bold Style = 1 << iota
	Dim
	Rev
	Good // green: fine
	Warn // yellow: look at it
	Bad  // red: needs you
)

const colours = Good | Warn | Bad

// Seg is a run of text in one style.
type Seg struct {
	Text  string
	Style Style
}

// Line is one screen line: segments left to right.
type Line []Seg

// Level is how much SGR the terminal gets.
type Level int

// Levels. NO_COLOR asks for LevelMono: attributes stay (bold, dim, reverse
// are not colour), colour roles are dropped. TERM=dumb gets LevelNone.
const (
	LevelNone Level = iota
	LevelMono
	LevelColor
)

// Width is the line's width in cells.
func (l Line) Width() int {
	n := 0
	for _, s := range l {
		n += width.String(s.Text)
	}
	return n
}

// Plain is the line's text without styling.
func (l Line) Plain() string {
	var b strings.Builder
	for _, s := range l {
		b.WriteString(s.Text)
	}
	return b.String()
}

// With adds style flags to every segment (the selection highlight).
func (l Line) With(st Style) Line {
	out := make(Line, len(l))
	for i, s := range l {
		out[i] = Seg{s.Text, s.Style | st}
	}
	return out
}

// Fit cuts the line to cols cells, ending a cut line with ellipsis, and drops
// trailing blanks of an unhighlighted end. It never splits a character from
// its combining marks (width.Truncate).
func (l Line) Fit(cols int, ellipsis string) Line {
	if cols <= 0 {
		return nil
	}
	if l.Width() <= cols {
		return trimBlank(l)
	}
	ew := width.String(ellipsis)
	if ew > cols {
		ellipsis, ew = "", 0
	}
	budget := cols - ew
	var out Line
	used := 0
	var last Style
	for _, s := range l {
		w := width.String(s.Text)
		if used+w <= budget {
			out = append(out, s)
			used += w
			last = s.Style
			continue
		}
		if cut := width.Truncate(s.Text, budget-used, ""); cut != "" {
			out = append(out, Seg{cut, s.Style})
			last = s.Style
		}
		break
	}
	out = trimBlank(out)
	if ellipsis != "" {
		out = append(out, Seg{ellipsis, last})
	}
	return out
}

// trimBlank removes trailing spaces from the last segments unless they are highlighted.
func trimBlank(l Line) Line {
	for len(l) > 0 {
		last := &l[len(l)-1]
		if last.Style&Rev != 0 {
			return l
		}
		last.Text = strings.TrimRight(last.Text, " ")
		if last.Text != "" {
			return l
		}
		l = l[:len(l)-1]
	}
	return l
}

// Render is the line as terminal text: SGR where the style changes, and a
// reset at the end so a style never leaks into the next line.
func (l Line) Render(lv Level) string {
	if lv == LevelNone {
		return l.Plain()
	}
	var b strings.Builder
	cur := Style(0)
	for _, s := range l {
		if s.Text == "" {
			continue
		}
		if st := effective(s.Style, lv); st != cur {
			b.WriteString(sgr(st))
			cur = st
		}
		b.WriteString(s.Text)
	}
	b.WriteString("\x1b[0m")
	return b.String()
}

// effective drops the colour roles at LevelMono, keeping a cue for the worst:
// a Bad segment turns bold.
func effective(st Style, lv Level) Style {
	if lv == LevelColor {
		return st
	}
	if st&Bad != 0 {
		st |= Bold
	}
	return st &^ colours
}

// sgr is the escape sequence that sets exactly st (it starts from a reset).
func sgr(st Style) string {
	codes := []string{"0"}
	if st&Bold != 0 {
		codes = append(codes, "1")
	}
	if st&Dim != 0 {
		codes = append(codes, "2")
	}
	if st&Rev != 0 {
		codes = append(codes, "7")
	}
	switch {
	case st&Bad != 0:
		codes = append(codes, "31")
	case st&Warn != 0:
		codes = append(codes, "33")
	case st&Good != 0:
		codes = append(codes, "32")
	}
	return "\x1b[" + strings.Join(codes, ";") + "m"
}

// StripSGR removes SGR (and any other CSI) sequences, leaving the text a
// person reads; golden tests compare frames this way.
func StripSGR(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func itoa(n int) string { return strconv.Itoa(n) }
