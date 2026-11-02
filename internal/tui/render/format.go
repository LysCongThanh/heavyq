package render

import (
	"fmt"
	"math"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/snapshot"
)

// dur is a short duration: 38s, 1m10s, 14m, 2h05m.
func dur(sec float64) string {
	if sec < 0 || math.IsNaN(sec) {
		sec = 0
	}
	s := int(sec)
	switch {
	case s < 60:
		return fmt.Sprintf("%ds", s)
	case s < 600:
		return fmt.Sprintf("%dm%02ds", s/60, s%60)
	case s < 3600:
		return fmt.Sprintf("%dm", s/60)
	}
	return fmt.Sprintf("%dh%02dm", s/3600, s%3600/60)
}

// size is megabytes as 9.6G or 740M.
func size(mb int) string {
	if mb >= 1024 {
		return fmt.Sprintf("%.1fG", float64(mb)/1024)
	}
	return fmt.Sprintf("%dM", mb)
}

// shortTree is the first 7 characters of a tree or commit id.
func shortTree(t string) string {
	if len(t) > 7 {
		return t[:7]
	}
	return t
}

// shortSlot abbreviates a slot name: heavy-1 is h1, light-2 is l2.
func shortSlot(name string) string {
	kind, num, ok := strings.Cut(name, "-")
	if !ok || kind == "" {
		return name
	}
	return kind[:1] + num
}

// shortReason trims a scheduler reason to its first clause: the trailing
// parenthetical is detail ("load 16.00 >= 9.0 (1.50 x 6 cores)").
func shortReason(r string) string {
	r = strings.TrimSpace(r)
	if strings.HasSuffix(r, ")") {
		if i := strings.LastIndex(r, " ("); i > 0 {
			r = r[:i]
		}
	}
	return r
}

// waitReason is why a waiting ticket cannot start, in a few words: the first
// candidate machine that blocks it, or "next poll" when none does.
func waitReason(w snapshot.Waiting) string {
	for _, c := range w.Candidates {
		if r := w.Reasons[c]; r != "" {
			return shortReason(r)
		}
	}
	for _, r := range w.Reasons { // candidates missing: any reason will do
		if r != "" {
			return shortReason(r)
		}
	}
	return "starts next poll"
}

// bar draws free RAM as cells, with a marker at the free RAM one heavy job
// needs. A fill that stops short of the marker means no job fits.
func bar(th Theme, freeMB, scaleMB, needMB, cells int) Line {
	if scaleMB <= 0 || cells <= 0 {
		return nil
	}
	fill := clamp(int(math.Round(float64(freeMB)/float64(scaleMB)*float64(cells))), 0, cells)
	mark := -1
	if needMB > 0 {
		mark = clamp(int(float64(needMB)/float64(scaleMB)*float64(cells)), 0, cells-1)
	}
	fits := needMB <= 0 || freeMB >= needMB
	var out Line
	for i := 0; i < cells; i++ {
		switch {
		case i == mark:
			st := Warn
			if fits {
				st = Dim
			}
			out = append(out, Seg{th.g(gNeed), st})
		case i < fill:
			out = append(out, Seg{th.g(gFull), Good})
		default:
			out = append(out, Seg{th.g(gEmpty), Dim})
		}
	}
	return out
}

func clamp(v, lo, hi int) int { return max(lo, min(hi, v)) }
