package anchor

import (
	"regexp"
	"strings"
)

const bom = "\uFEFF"

// lineSpan is one line of the input: data[start:end] without "\n". The
// trailing "\r" of a CRLF line is part of the span (and of the file) but is
// dropped from the text handed to the parser.
type lineSpan struct{ start, end int }

// splitLines splits data on "\n" without copying. A final line without a
// newline counts; an empty trailing segment does not.
func splitLines(data []byte) []lineSpan {
	var out []lineSpan
	start := 0
	for i, b := range data {
		if b == '\n' {
			out = append(out, lineSpan{start, i})
			start = i + 1
		}
	}
	if start < len(data) {
		out = append(out, lineSpan{start, len(data)})
	}
	return out
}

// lineText returns the line as the parser sees it: no CR, and no BOM on
// line 1. The second result is how many leading bytes were dropped (the BOM).
func lineText(data []byte, l lineSpan, first bool) (string, int) {
	s := strings.TrimSuffix(string(data[l.start:l.end]), "\r")
	if first && strings.HasPrefix(s, bom) {
		return s[len(bom):], len(bom)
	}
	return s, 0
}

// itemLine matches "- [ ] text", "* [x] text", "+ [X] text" with any
// indentation (spaces or tabs). Groups: 1 indent and marker, 2 box, 3 text.
var itemLine = regexp.MustCompile(`^([ \t]*[-*+][ \t]+)\[([ xX])\][ \t]+(.*)$`)

// matchItemLine parses a checkbox line. boxCol is the byte offset of the box
// character inside line; text is everything after "] ".
func matchItemLine(line string) (boxCol int, checked bool, text string, ok bool) {
	m := itemLine.FindStringSubmatchIndex(line)
	if m == nil {
		return 0, false, "", false
	}
	return m[4], line[m[4]] != ' ', line[m[6]:m[7]], true
}

// fenceTracker follows fenced code blocks (``` or ~~~) so checkboxes inside
// them are ignored. A fence closes with the same character, at least as long,
// and nothing else on the line. An unclosed fence runs to the end of file.
type fenceTracker struct {
	char byte
	size int
}

// inCode reports whether line belongs to a fence (opening and closing fence
// lines included) and updates the state.
func (f *fenceTracker) inCode(line string) bool {
	t := strings.TrimLeft(line, " \t")
	c, n := fenceRun(t)
	if f.size == 0 {
		if n >= 3 && !(c == '`' && strings.Contains(t[n:], "`")) {
			f.char, f.size = c, n
			return true
		}
		return false
	}
	if c == f.char && n >= f.size && strings.TrimSpace(t[n:]) == "" {
		f.char, f.size = 0, 0
	}
	return true
}

// fenceRun returns the fence character and the length of the run at the start
// of s (0 when s does not start with a backtick or tilde).
func fenceRun(s string) (byte, int) {
	if s == "" || (s[0] != '`' && s[0] != '~') {
		return 0, 0
	}
	n := 1
	for n < len(s) && s[n] == s[0] {
		n++
	}
	return s[0], n
}

// codeSpan is an inline code span: s[start:end] including the backticks.
type codeSpan struct {
	start, end int
	content    string // trimmed
}

// codeSpans finds inline code spans per CommonMark: a run of N backticks
// closes at the next run of exactly N. Unclosed runs are literal text.
func codeSpans(s string) []codeSpan {
	var out []codeSpan
	for i := 0; i < len(s); {
		if s[i] != '`' {
			i++
			continue
		}
		n := runLen(s, i)
		found := -1
		for j := i + n; j < len(s); {
			if s[j] != '`' {
				j++
				continue
			}
			m := runLen(s, j)
			if m == n {
				found = j
				break
			}
			j += m
		}
		if found < 0 {
			i += n
			continue
		}
		out = append(out, codeSpan{i, found + n, strings.TrimSpace(s[i+n : found])})
		i = found + n
	}
	return out
}

func runLen(s string, i int) int {
	n := 0
	for i+n < len(s) && s[i+n] == '`' {
		n++
	}
	return n
}
