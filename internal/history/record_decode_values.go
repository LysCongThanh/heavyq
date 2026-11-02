package history

import (
	"bytes"
	"strconv"
	"time"
	"unicode/utf8"
)

// Scalar readers of the fast decoder (see fastDecode): each parses one JSON
// value at b[i:] and returns it with the index after it, or ok == false when
// the value is not exactly what encoding/json would read into that type.

func skipSpace(b []byte, i int) int {
	for i < len(b) && (b[i] == ' ' || b[i] == '\t' || b[i] == '\n' || b[i] == '\r') {
		i++
	}
	return i
}

// scanString scans the JSON string at b[i:] and returns the bytes between the
// quotes, whether they hold an escape, and the index after the closing quote.
// ok is false for a control byte, invalid UTF-8 or a missing quote.
func scanString(b []byte, i int) (raw []byte, escaped bool, end int, ok bool) {
	if i >= len(b) || b[i] != '"' {
		return nil, false, i, false
	}
	start, high := i+1, false // high: a byte >= 0x80, so UTF-8 needs checking
	for j := start; j < len(b); j++ {
		if ordinary[b[j]] { // the common byte: no branches on the special cases
			continue
		}
		switch c := b[j]; {
		case c == '"':
			raw = b[start:j]
			return raw, escaped, j + 1, !high || utf8.Valid(raw)
		case c == '\\':
			escaped = true
			j++ // the escaped byte cannot end the string
		case c < 0x20:
			return nil, false, i, false
		default: // c >= 0x80
			high = true
		}
	}
	return nil, false, i, false
}

// ordinary marks the bytes that need no attention inside a JSON string: ASCII
// from space up, except the quote and the backslash.
var ordinary = func() (t [256]bool) {
	for c := 0x20; c < 0x80; c++ {
		t[c] = c != '"' && c != '\\'
	}
	return t
}()

// plainString is scanString for a string that must have no escape (a key, a
// timestamp).
func plainString(b []byte, i int) ([]byte, int, bool) {
	raw, escaped, end, ok := scanString(b, i)
	return raw, end, ok && !escaped
}

// lineBuf is a line as bytes to scan and as a string to take fields from: the
// strings of a decoded Record share the one copy of the line instead of each
// being allocated (a third of the cost of decoding a ledger line).
type lineBuf struct {
	b []byte
	s string
}

// str reads the JSON string at i.
func (ln lineBuf) str(i int) (string, int, bool) {
	raw, escaped, end, ok := scanString(ln.b, i)
	switch {
	case !ok:
		return "", i, false
	case !escaped:
		return ln.s[i+1 : i+1+len(raw)], end, true
	}
	s, ok := unescape(raw)
	return s, end, ok
}

// unescape decodes the escapes of a JSON string body. It refuses surrogate
// escapes (\ud800-\udfff, which encoding/json pairs up or replaces) and
// anything unknown, so the caller falls back to encoding/json for them.
func unescape(raw []byte) (string, bool) {
	out := make([]byte, 0, len(raw))
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\\' {
			out = append(out, raw[i])
			continue
		}
		i++
		if i >= len(raw) {
			return "", false
		}
		switch c := raw[i]; c {
		case '"', '\\', '/':
			out = append(out, c)
		case 'b':
			out = append(out, '\b')
		case 'f':
			out = append(out, '\f')
		case 'n':
			out = append(out, '\n')
		case 'r':
			out = append(out, '\r')
		case 't':
			out = append(out, '\t')
		case 'u':
			if i+4 >= len(raw) {
				return "", false
			}
			n, err := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
			if err != nil || (n >= 0xD800 && n <= 0xDFFF) {
				return "", false
			}
			out = utf8.AppendRune(out, rune(n))
			i += 4
		default:
			return "", false
		}
	}
	return string(out), true
}

func timeValue(t *time.Time, b []byte, i int) (int, bool) {
	_, ni, ok := plainString(b, i)
	if !ok {
		return i, false
	}
	return ni, t.UnmarshalJSON(b[i:ni]) == nil
}

func boolValue(b []byte, i int) (bool, int, bool) {
	switch {
	case bytes.HasPrefix(b[i:], []byte("true")):
		return true, i + 4, true
	case bytes.HasPrefix(b[i:], []byte("false")):
		return false, i + 5, true
	}
	return false, i, false
}

// numberToken returns the JSON number at b[i:] (strict grammar) and whether it
// is an integer.
func numberToken(b []byte, i int) (tok []byte, isInt bool, end int, ok bool) {
	j := i
	if j < len(b) && b[j] == '-' {
		j++
	}
	digits := func() int {
		n := 0
		for j < len(b) && b[j] >= '0' && b[j] <= '9' {
			j, n = j+1, n+1
		}
		return n
	}
	intStart := j
	if n := digits(); n == 0 || (n > 1 && b[intStart] == '0') {
		return nil, false, i, false
	}
	isInt = true
	if j < len(b) && b[j] == '.' {
		j++
		if digits() == 0 {
			return nil, false, i, false
		}
		isInt = false
	}
	if j < len(b) && (b[j] == 'e' || b[j] == 'E') {
		j++
		if j < len(b) && (b[j] == '+' || b[j] == '-') {
			j++
		}
		if digits() == 0 {
			return nil, false, i, false
		}
		isInt = false
	}
	return b[i:j], isInt, j, true
}

func intValue(b []byte, i int) (int, int, bool) {
	tok, isInt, end, ok := numberToken(b, i)
	if !ok || !isInt {
		return 0, i, false
	}
	n, err := strconv.ParseInt(string(tok), 10, strconv.IntSize)
	return int(n), end, err == nil
}

func floatValue(b []byte, i int) (float64, int, bool) {
	tok, _, end, ok := numberToken(b, i)
	if !ok {
		return 0, i, false
	}
	f, err := strconv.ParseFloat(string(tok), 64)
	return f, end, err == nil
}
