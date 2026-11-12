package recap

import (
	"fmt"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/statusline"
	"github.com/LysCongThanh/heavyq/internal/tui/width"
)

// RenderOptions of the text form.
type RenderOptions struct {
	ASCII   bool // ASCII glyphs and text only
	Verbose bool // lanes, and when each line happened
}

// maxTextCols is how wide the text column of a bucket may grow before the
// command that acts on the line.
const maxTextCols = 62

// Render is the recap as text: the header (progress, forecast, constraint), the
// next action, then the buckets that are not empty, each line ending with its
// command.
func Render(r Recap, o RenderOptions) string {
	th := statusline.Theme{ASCII: o.ASCII}
	var b strings.Builder
	b.WriteString(txt(header(r, th), o) + "\n")
	if r.Best != nil {
		fmt.Fprintf(&b, "next: %s   %s\n", r.Best.Command, txt("("+r.Best.Why+")", o))
	} else {
		b.WriteString("next: nothing needs you\n")
	}
	title := "proven"
	if r.MergeRef != "" {
		title = "proven, not on " + r.MergeRef
		bucket(&b, "merged into "+r.MergeRef, r.Merged, o)
	}
	bucket(&b, title, r.ProvenNotMerged, o)
	bucket(&b, "claimed, unproven", r.ClaimedUnproven, o)
	bucket(&b, "done · stale", r.DoneStale, o)
	bucket(&b, "blocked", r.Blocked, o)
	bucket(&b, "wasted", r.Wasted, o)
	bucket(&b, "in flight", r.InFlight, o)
	if o.Verbose && len(r.Lanes) > 0 {
		b.WriteString("\nlanes\n")
		for _, l := range r.Lanes {
			since := ""
			if l.Since != nil {
				since = "  since " + l.Since.Local().Format("15:04") + " (" + human(r.Generated.Sub(*l.Since).Seconds()) + ")"
			}
			fmt.Fprintf(&b, "  %s\n", txt(strings.TrimRight(fmt.Sprintf("%-14s %-10s %s%s", l.Lane, l.State, l.Item, since), " "), o))
		}
	}
	if r.Progress.Total == 0 && len(r.Wasted) == 0 && len(r.Blocked) == 0 {
		b.WriteString("\nnothing to recap: no work items in the anchor (docs/anchor.md) and no repeated failures since " +
			r.Since.Local().Format("15:04") + "\n")
	}
	return b.String()
}

// header is the one line: progress, forecast, constraint, what needs you.
func header(r Recap, th statusline.Theme) string {
	var parts []string
	if r.Progress.Total > 0 {
		parts = append(parts, fmt.Sprintf("%s %d/%d proven", statusline.Pick(th, statusline.GlyphProgress), r.Progress.Proven, r.Progress.Total),
			forecastText(r, th))
	}
	if r.Constraint != "" {
		parts = append(parts, statusline.Pick(th, statusline.GlyphConstraint)+" "+statusline.ConstraintLabel(r.Constraint))
	}
	if r.NeedsYou > 0 {
		parts = append(parts, fmt.Sprintf("%s %d needs you", statusline.Pick(th, statusline.GlyphNeeds), r.NeedsYou))
	}
	if len(parts) == 0 {
		return "heavyq recap"
	}
	return strings.Join(parts, "  ")
}

func forecastText(r Recap, th statusline.Theme) string {
	f, dot := r.Forecast, " "+statusline.Pick(th, statusline.GlyphDot)+" "
	conf := ""
	if f.Confidence != "none" {
		conf = fmt.Sprintf(" (%s confidence, %d finishes)", f.Confidence, f.Samples)
	}
	switch {
	case f.Done:
		return statusline.Pick(th, statusline.GlyphGood) + " all proven"
	case f.Status == "unknown":
		return "forecast unknown" + dot + f.Reason
	}
	eta := "~?"
	if f.ETA != nil {
		eta = "~" + statusline.ETAClock(*f.ETA, r.Generated)
	}
	if f.Status == "at_risk" {
		return statusline.Pick(th, statusline.GlyphRisk) + " at risk" + dot + eta + conf + dot + f.Reason
	}
	return statusline.Pick(th, statusline.GlyphGood) + " on track" + dot + eta + conf
}

// bucket prints one section; an empty one prints nothing.
func bucket(b *strings.Builder, title string, lines []Line, o RenderOptions) {
	if len(lines) == 0 {
		return
	}
	fmt.Fprintf(b, "\n%s (%d)\n", txt(title, o), len(lines))
	texts := make([]string, len(lines))
	pad := 0
	for i, l := range lines {
		texts[i] = txt(l.Text+when(l, o), o)
		pad = max(pad, min(width.String(texts[i]), maxTextCols))
	}
	for i, l := range lines {
		t := width.Truncate(texts[i], maxTextCols, "...")
		fmt.Fprintf(b, "  %s%s   %s\n", t, strings.Repeat(" ", pad-width.String(t)), l.Action)
	}
}

// when is the verbose time note of a line.
func when(l Line, o RenderOptions) string {
	if !o.Verbose || l.At == nil {
		return ""
	}
	return "  [" + l.At.Local().Format("15:04") + "]"
}

// txt makes text safe for the output: ASCII only when asked.
func txt(s string, o RenderOptions) string {
	if !o.ASCII {
		return s
	}
	s = strings.NewReplacer("·", ".", "→", "->", "…", "...", "–", "-", "—", "-").Replace(s)
	return strings.Map(func(r rune) rune {
		if r > 0x7e {
			return '?'
		}
		return r
	}, s)
}
