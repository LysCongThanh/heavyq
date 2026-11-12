package redact

import (
	"regexp"
	"strings"
	"unicode"
)

// escapeSeq matches terminal escape sequences: CSI (colours, cursor moves) and
// OSC (titles, hyperlinks), as a command line could carry them in its argv.
var escapeSeq = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)

// OneLine is s as one line of plain text, for a command line heavyq quotes
// (the running line, the digest, why): escape sequences are dropped and every
// other control character (newline, carriage return, tab, a lone ESC) becomes
// a space, so a multi-line `sh -c` script or a byte from argv can neither
// colour the output nor split a line. It does not redact: callers pass the
// already redacted command.
func OneLine(s string) string {
	s = escapeSeq.ReplaceAllString(s, "")
	if strings.IndexFunc(s, unicode.IsControl) < 0 {
		return s
	}
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}
