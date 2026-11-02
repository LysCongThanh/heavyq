package statusline

import (
	"fmt"
	"strings"
	"time"

	"github.com/LysCongThanh/heavyq/internal/flow"
	"github.com/LysCongThanh/heavyq/internal/flowview"
	"github.com/LysCongThanh/heavyq/internal/forecast"
	"github.com/LysCongThanh/heavyq/internal/tui/render"
)

// MaxCols is the widest a segment may be, so it fits beside another status line.
const MaxCols = 60

// Theme is how the segment is drawn; it is the TUI's (NO_COLOR, TERM=dumb,
// non-UTF-8 locale, --ascii).
type Theme = render.Theme

// glyphs: the unicode form and the ASCII stand-in. Every glyph is followed by a
// space (a terminal that draws ambiguous-width characters double must not make
// text touch), and there is no emoji.
type Glyph struct{ uni, asc string }

var (
	GlyphProgress   = Glyph{"◆", "#"}
	GlyphNeeds      = Glyph{"⚑", "!"}
	GlyphConstraint = Glyph{"▲", "^"}
	GlyphGood       = Glyph{"✓", "+"}
	GlyphRisk       = Glyph{"⚠", "!"}
	GlyphDot        = Glyph{"·", "."}
	GlyphBar        = Glyph{"│", "|"}
	GlyphEllipsis   = Glyph{"…", "..."}
)

func Pick(th Theme, g Glyph) string {
	if th.ASCII {
		return g.asc
	}
	return g.uni
}

// Segment builds heavyq's segment from the flow state; nil when there is
// nothing to say (no state, or no items and nothing needing a person).
// It is nearly silent when nothing is notable:
//
//	◆ 5/8 ✓ ~16:40
//
// and says more only when there is something to act on or look at:
//
//	◆ 5/8 proven  ⚑ 1 needs you  ▲ CI wait  ✓ on track · ~16:40
func Segment(st *flow.Snapshot, now time.Time, th Theme) render.Line {
	if st == nil || st.Empty() {
		return nil
	}
	fc := flowview.Forecast(st, now)
	needs := st.NeedCount()
	constraint := ""
	if c := st.Constraint; c.Significant && c.State != "" && c.State != "none" {
		constraint = ConstraintLabel(string(c.State))
	}
	notable := needs > 0 || constraint != "" || fc.Status == forecast.AtRisk

	// The most detailed form that fits MaxCols wins; each next one drops the least important part.
	for _, form := range []struct {
		word, constraint bool
	}{{notable, true}, {notable, false}, {false, false}} {
		c := constraint
		if !form.constraint {
			c = ""
		}
		l := compose(st, fc, now, th, form.word, needs, c)
		if l.Width() <= MaxCols {
			return l
		}
	}
	return compose(st, fc, now, th, false, needs, "").Fit(MaxCols, Pick(th, GlyphEllipsis))
}

// compose joins the parts with two spaces.
func compose(st *flow.Snapshot, fc forecast.Result, now time.Time, th Theme, word bool, needs int, constraint string) render.Line {
	var parts []render.Line
	loud := word || needs > 0 || constraint != "" // quiet segments are tighter: "◆ 5/8 ✓ ~16:40"
	one := func(style render.Style, text string) render.Line { return render.Line{{Text: text, Style: style}} }
	if st.Items.Total > 0 {
		p := fmt.Sprintf("%s %d/%d", Pick(th, GlyphProgress), st.Items.Proven, st.Items.Total)
		if word {
			p += " proven"
		}
		parts = append(parts, one(0, p))
	}
	if needs > 0 {
		parts = append(parts, one(render.Bad, fmt.Sprintf("%s %d needs you", Pick(th, GlyphNeeds), needs)))
	}
	if constraint != "" {
		parts = append(parts, one(render.Warn, Pick(th, GlyphConstraint)+" "+constraint))
	}
	if st.Items.Total > 0 { // no items, nothing to forecast
		if f := forecastPart(fc, now, th, loud); f != nil {
			parts = append(parts, f)
		}
	}
	sep := " "
	if loud {
		sep = "  "
	}
	var out render.Line
	for i, p := range parts {
		if i > 0 {
			out = append(out, render.Seg{Text: sep})
		}
		out = append(out, p...)
	}
	return out
}

// forecastPart is the reassurance: "✓ on track · ~16:40", "⚠ at risk · ~16:40",
// "✓ done"; nothing while the forecast is unknown. Without the words it is
// "✓ ~16:40".
func forecastPart(fc forecast.Result, now time.Time, th Theme, words bool) render.Line {
	switch {
	case fc.Done:
		return render.Line{{Text: Pick(th, GlyphGood) + " done", Style: render.Good}}
	case fc.Status == forecast.Unknown:
		return nil
	}
	eta := "~" + ETAClock(fc.ETA, now)
	dot := " " + Pick(th, GlyphDot) + " "
	if fc.Status == forecast.AtRisk {
		return render.Line{{Text: Pick(th, GlyphRisk) + " at risk" + dot + eta, Style: render.Warn}}
	}
	if !words {
		return render.Line{{Text: Pick(th, GlyphGood) + " " + eta, Style: render.Good}}
	}
	return render.Line{{Text: Pick(th, GlyphGood) + " on track" + dot + eta, Style: render.Good}}
}

// ETAClock is the local time of day, with the weekday when it is not today.
func ETAClock(eta, now time.Time) string {
	eta, now = eta.Local(), now.Local()
	if eta.YearDay() == now.YearDay() && eta.Year() == now.Year() {
		return eta.Format("15:04")
	}
	return eta.Format("Mon 15:04")
}

// ConstraintLabel turns a constraint state ("ci_wait") into words ("CI wait").
func ConstraintLabel(state string) string {
	words := strings.Split(state, "_")
	for i, w := range words {
		if w == "ci" {
			words[i] = "CI"
		}
	}
	return strings.Join(words, " ")
}

// RenderLine is the segment as terminal text. Under NO_COLOR colour goes but
// bold stays (needs-you), and a segment with nothing bold carries no escape
// sequence at all.
func RenderLine(l render.Line, lv render.Level) string {
	if lv == render.LevelMono {
		styled := false
		for _, s := range l {
			styled = styled || s.Style&(render.Bad|render.Bold|render.Dim|render.Rev) != 0
		}
		if !styled {
			return l.Plain()
		}
	}
	return l.Render(lv)
}

// Join puts the segment on the last line of existing status-line text, behind
// a separator. When that text uses colour, its style is reset first so it
// cannot leak into the separator.
func Join(existing, segment string, th Theme) string {
	if segment == "" {
		return existing
	}
	if existing == "" {
		return segment
	}
	reset := ""
	if th.Level != render.LevelNone && strings.Contains(existing[strings.LastIndex(existing, "\n")+1:], "\x1b[") {
		reset = "\x1b[0m"
	}
	return existing + reset + " " + Pick(th, GlyphBar) + " " + segment
}
