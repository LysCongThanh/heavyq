// Package width measures and cuts text by what a terminal shows, in cells.
//
// Terminals place one grapheme per one or two cells. len(s) counts bytes and
// utf8.RuneCountInString counts code points: both are wrong for decomposed
// text (NFD "ậ" is a base letter plus two combining marks and takes one
// cell) and for East Asian text (two cells per character). The rule here:
//
//	Mn, Me, Cf (combining marks, format characters such as ZWJ)  0 cells
//	East Asian Wide and Fullwidth, and emoji presentation          2 cells
//	control characters                                             0 cells
//	everything else                                                1 cell
//
// Truncate never cuts between a base character and the marks after it.
package width

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Rune is the number of cells r takes: 0, 1 or 2.
func Rune(r rune) int {
	switch {
	case r == utf8.RuneError:
		return 1 // an invalid byte is shown as a replacement character
	case r < 0x20 || (r >= 0x7F && r < 0xA0):
		return 0 // control characters are never printed by the renderer
	case r < 0x300:
		return 1 // fast path: ASCII and Latin-1
	case unicode.In(r, unicode.Mn, unicode.Me, unicode.Cf):
		return 0
	case isWide(r):
		return 2
	}
	return 1
}

// String is the number of cells s takes.
func String(s string) int {
	n := 0
	for _, r := range s {
		n += Rune(r)
	}
	return n
}

// Truncate cuts s to at most max cells. When it has to cut, it ends with
// ellipsis (counted in the budget); with an empty ellipsis it just cuts. It
// never splits a character from the combining marks that follow it, and
// never leaves half of a two-cell character.
func Truncate(s string, max int, ellipsis string) string {
	if max <= 0 {
		return ""
	}
	if String(s) <= max {
		return s
	}
	budget := max - String(ellipsis)
	if budget < 0 {
		ellipsis, budget = "", max
	}
	return cut(s, budget) + ellipsis
}

// cut is the longest prefix of s within max cells, ending at a cluster edge.
func cut(s string, max int) string {
	used, end := 0, 0
	for i, r := range s {
		w := Rune(r)
		if w == 0 {
			continue // a mark: kept only with the character before it (below)
		}
		if used+w > max {
			break
		}
		used += w
		end = i + utf8.RuneLen(r)
	}
	// Marks right after the last kept character belong to it.
	for end < len(s) {
		r, size := utf8.DecodeRuneInString(s[end:])
		if Rune(r) != 0 || r < 0x300 {
			break
		}
		end += size
	}
	return s[:end]
}

// Pad appends spaces to s up to w cells; a longer s is returned unchanged.
func Pad(s string, w int) string {
	if n := String(s); n < w {
		return s + strings.Repeat(" ", w-n)
	}
	return s
}

// PadLeft prepends spaces to s up to w cells.
func PadLeft(s string, w int) string {
	if n := String(s); n < w {
		return strings.Repeat(" ", w-n) + s
	}
	return s
}

// Wrap breaks s at spaces into lines of at most w cells. A word longer than
// w is cut. Runs of white space collapse to one space.
func Wrap(s string, w int) []string {
	if w <= 0 {
		return nil
	}
	var lines []string
	cur := ""
	for _, word := range strings.Fields(s) {
		for String(word) > w {
			if cur != "" {
				lines, cur = append(lines, cur), ""
			}
			head := cut(word, w)
			if head == "" { // one character wider than w: drop it rather than loop
				_, size := utf8.DecodeRuneInString(word)
				word = word[size:]
				continue
			}
			lines, word = append(lines, head), word[len(head):]
		}
		switch {
		case word == "":
		case cur == "":
			cur = word
		case String(cur)+1+String(word) <= w:
			cur += " " + word
		default:
			lines, cur = append(lines, cur), word
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}
