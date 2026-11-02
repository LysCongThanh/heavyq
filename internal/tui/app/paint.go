package app

import (
	"strconv"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/tui/term"
)

// painter writes a frame as a synchronized update that rewrites only the
// lines that changed since the last one, in one buffer (one write call).
type painter struct {
	prev []string
}

// paint returns the bytes that bring the screen from the previous frame to
// lines; nil when nothing changed. full repaints everything (after a resize,
// a resume or Ctrl-L, when the screen's content cannot be trusted).
func (p *painter) paint(lines []string, full bool) []byte {
	if !full && equal(p.prev, lines) {
		return nil
	}
	var b strings.Builder
	b.WriteString(term.BSU)
	if full {
		b.WriteString("\x1b[2J")
	}
	for i, l := range lines {
		if !full && i < len(p.prev) && p.prev[i] == l {
			continue
		}
		b.WriteString("\x1b[" + strconv.Itoa(i+1) + ";1H")
		b.WriteString(l)
		b.WriteString("\x1b[0m\x1b[K") // reset, then clear what an older, longer line left
	}
	if len(lines) < len(p.prev) { // the frame got shorter
		b.WriteString("\x1b[" + strconv.Itoa(len(lines)+1) + ";1H\x1b[J")
	}
	b.WriteString(term.ESU)
	p.prev = append(p.prev[:0], lines...)
	return []byte(b.String())
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
