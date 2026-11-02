package render

import (
	"strconv"

	"github.com/LysCongThanh/heavyq/internal/snapshot"
)

// kindLabel is the short name of an exception kind.
var kindLabel = map[string]string{
	snapshot.KindRedGate: "red gate", snapshot.KindLoop: "loop", snapshot.KindStarved: "starved",
	snapshot.KindMachine: "machine", snapshot.KindBlocked: "blocked",
}

// minFlex is the least width worth giving a truncated command.
const minFlex = 12

// exceptionSection is NEEDS YOU: one row per exception, or one calm line.
func (c *ctx) exceptionSection(rows []Row, w int, compact bool) []bodyLine {
	var ex []Row
	for _, r := range rows {
		if r.Kind == RowException {
			ex = append(ex, r)
		}
	}
	if len(c.s.Exceptions) == 0 {
		return []bodyLine{{Line: Line{c.seg(Good|Bold, "NOTHING NEEDS YOU")}}}
	}
	if len(ex) == 0 {
		return []bodyLine{{Line: c.heading("NEEDS YOU (0 match the filter)", Dim, w)}}
	}
	out := []bodyLine{{Line: c.heading("NEEDS YOU ("+strconv.Itoa(len(ex))+")", Bad, w)}}
	for _, r := range ex {
		out = append(out, c.row(r.Key, c.exceptionContent(*r.Exception, w-2, compact), w))
	}
	return out
}

// part is one fact of an exception row. A flex part is cut to fit instead of dropped.
type part struct {
	text string
	st   Style
	flex bool
}

// exceptionContent composes the row from the richest form that fits down to
// the plain sentence: glyph, kind, subject, then tree, command, count, and
// on the right the action (a command to type, or with --control the key).
func (c *ctx) exceptionContent(e snapshot.Exception, w int, compact bool) Line {
	st, g := Bad, gCrit
	if e.Severity == snapshot.SeverityWarning {
		st, g = Warn, gWarn
	}
	head := Line{c.mark(g, st), c.seg(Bold, kindLabel[e.Kind])}
	subject := e.Lane
	if subject == "" {
		subject = e.Machine
	}
	parts := []part{{text: subject, st: Bold}}
	ev := e.Evidence
	switch e.Kind {
	case snapshot.KindRedGate, snapshot.KindLoop:
		if ev.Tree != "" {
			parts = append(parts, part{text: "tree " + shortTree(ev.Tree), st: Dim})
		}
		if ev.Cmd != "" {
			parts = append(parts, part{text: ev.Cmd, flex: true})
		}
		if ev.Count > 0 {
			parts = append(parts, part{text: "failed " + strconv.Itoa(ev.Count) + "x", st: Bad})
		}
	case snapshot.KindStarved:
		if ev.Cmd != "" {
			parts = append(parts, part{text: ev.Cmd, flex: true})
		}
		if ev.Detail != "" {
			parts = append(parts, part{text: ev.Detail, st: Warn})
		}
		if ev.Count > 0 {
			parts = append(parts, part{text: "gave up " + strconv.Itoa(ev.Count) + "x", st: Warn})
		}
	case snapshot.KindBlocked:
		parts = append(parts, part{text: "item " + ev.Item, st: Dim}, part{text: "needs " + ev.Detail, flex: true})
	case snapshot.KindMachine:
		parts = []part{{text: subject, st: Bold}, {text: ev.Detail, flex: true}}
	}
	kept := parts[:0]
	for _, p := range parts {
		if p.text != "" {
			kept = append(kept, p)
		}
	}
	parts = kept
	action := ""
	if !compact && e.Action != nil {
		action = c.actionText(e.Action)
	}
	if l, ok := c.compose(head, parts, action, w); ok {
		return l
	}
	// Nothing structured fits: the sentence, cut.
	return append(head, blanks(1), c.seg(0, e.Text))
}

// actionText is what to show for an exception's action.
func (c *ctx) actionText(a *snapshot.Action) string {
	if c.v.Control {
		switch a.Kind {
		case snapshot.ActionCancel:
			return "c cancel"
		case snapshot.ActionRaise:
			return "+ prio"
		case snapshot.ActionResume:
			return "p resume"
		}
	}
	return a.Command
}

// compose lays head and parts out on one line of w cells, with action right
// aligned. It tries the whole row with the action, then without it, then the same
// with one part fewer, and so on; a flex part is truncated to what is left when that is
// at least minFlex cells. ok is false when not even head plus the first part fits.
func (c *ctx) compose(head Line, parts []part, action string, w int) (Line, bool) {
	for n := len(parts); n >= 1; n-- {
		for _, withAction := range []bool{true, false} {
			if withAction && action == "" {
				continue
			}
			if l, ok := c.tryParts(head, parts[:n], action, withAction, w); ok {
				return l, true
			}
		}
	}
	return nil, false
}

func (c *ctx) tryParts(head Line, parts []part, action string, withAction bool, w int) (Line, bool) {
	used := head.Width()
	flexAt := -1
	for i, p := range parts {
		used += 2 // the gap before the part
		if p.flex {
			flexAt = i
			continue
		}
		used += cellWidth(c.th.txt(p.text))
	}
	right := 0
	if withAction {
		right = 2 + cellWidth(c.th.txt(action))
	}
	avail := w - used - right
	line := append(Line{}, head...)
	for i, p := range parts {
		text := p.text
		if i == flexAt {
			if avail < minFlex && !(avail >= cellWidth(c.th.txt(text))) {
				return nil, false
			}
			text = c.th.txt(text)
			if cellWidth(text) > avail {
				text = Line{{text, 0}}.Fit(avail, c.th.g(gEll)).Plain()
			}
			line = append(line, blanks(2), Seg{text, p.st})
			continue
		}
		line = append(line, blanks(2), c.seg(p.st, text))
	}
	if avail < 0 {
		return nil, false
	}
	if withAction {
		gap := w - line.Width() - cellWidth(c.th.txt(action))
		line = append(line, blanks(max(gap, 2)), c.seg(Dim, action))
	}
	return line, line.Width() <= w
}
