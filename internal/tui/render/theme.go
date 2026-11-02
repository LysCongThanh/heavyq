package render

import (
	"strings"

	"github.com/LysCongThanh/heavyq/internal/tui/width"
)

// Theme is how the screen is drawn: how much SGR, and whether non-ASCII
// glyphs are allowed.
type Theme struct {
	Level Level
	ASCII bool
}

// ThemeFromEnv decides the theme from the environment, by the usual rules:
// NO_COLOR (any non-empty value) drops colour but keeps bold and reverse;
// TERM=dumb gets no escape sequences at all; a locale that is not UTF-8 gets
// the ASCII glyph set. asciiFlag forces ASCII (--ascii).
func ThemeFromEnv(getenv func(string) string, asciiFlag bool) Theme {
	t := Theme{Level: LevelColor, ASCII: asciiFlag}
	if getenv("NO_COLOR") != "" {
		t.Level = LevelMono
	}
	if getenv("TERM") == "dumb" {
		t.Level, t.ASCII = LevelNone, true
	}
	if !localeIsUTF8(getenv) {
		t.ASCII = true
	}
	return t
}

// localeIsUTF8 follows the precedence of the C library: LC_ALL, then
// LC_CTYPE, then LANG; the first one that is set decides.
func localeIsUTF8(getenv func(string) string) bool {
	for _, k := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v := strings.ToLower(getenv(k)); v != "" {
			return strings.Contains(v, "utf-8") || strings.Contains(v, "utf8")
		}
	}
	return false
}

// glyph is a symbol with its ASCII stand-in.
type glyph struct{ uni, asc string }

// The fixed glyph set. No emoji. Status glyphs (dot, triangle) may be drawn
// two cells wide by terminals that treat ambiguous-width characters as wide,
// so each one is always followed by a space (see ctx.mark), and everything
// that has to line up (the RAM bar, rules, separators, the selection marker)
// is made of characters that are one cell wide everywhere.
var (
	gCrit  = glyph{"●", "*"}
	gWarn  = glyph{"▲", "!"}
	gRun   = glyph{"●", "*"}
	gWait  = glyph{"○", "o"}
	gFull  = glyph{"▪", "#"} // U+25AA, neutral width
	gEmpty = glyph{"▫", "."} // U+25AB, neutral width
	gNeed  = glyph{"|", "|"}
	gRule  = glyph{"-", "-"}
	gSel   = glyph{">", ">"}
	gEll   = glyph{"…", "..."}
	gEnter = glyph{"⏎", "Enter"}
	gMore  = glyph{"↓", "v"}
	gLess  = glyph{"↑", "^"}
)

func (t Theme) g(x glyph) string {
	if t.ASCII {
		return x.asc
	}
	return x.uni
}

// asciiReplacer turns the symbols snapshot text uses into ASCII.
var asciiReplacer = strings.NewReplacer(
	"≥", ">=", "≤", "<=", "…", "...", "·", ".", "—", "-", "–", "-", "→", "->", "←", "<-",
	"●", "*", "○", "o", "▲", "!", "▪", "#", "▫", ".", "─", "-", "│", "|", "↓", "v", "↑", "^", "⏎", "Enter",
	"‘", "'", "’", "'", "“", `"`, "”", `"`, "×", "x", " ", " ",
)

// txt makes s safe for the theme. In ASCII mode every non-ASCII character
// becomes its ASCII form or '?', and combining marks vanish, so no stray
// byte reaches a terminal that cannot show it.
func (t Theme) txt(s string) string {
	if !t.ASCII {
		return s
	}
	s = asciiReplacer.Replace(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r < 0x80:
			b.WriteRune(r)
		case width.Rune(r) == 0:
			// a combining mark: dropped
		default:
			b.WriteByte('?')
		}
	}
	return b.String()
}
