// Package input turns the raw bytes of a terminal in raw mode into keys and
// commands. It has no I/O and no timers: the caller feeds it bytes, and calls
// Flush when it has waited long enough after a lone Escape.
package input

import "unicode/utf8"

// KeyKind classifies a key.
type KeyKind int

// Key kinds. KeyRune carries a printable character in Key.Rune.
const (
	KeyRune KeyKind = iota
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
	KeyHome
	KeyEnd
	KeyPageUp
	KeyPageDown
	KeyDelete
	KeyEnter
	KeyEsc
	KeyTab
	KeyShiftTab
	KeyBackspace
	KeyCtrlC
	KeyCtrlZ
	KeyCtrlL
	KeyUnknown // a control byte or sequence with no meaning here
)

// Key is one key press.
type Key struct {
	Kind KeyKind
	Rune rune // KeyRune only
}

// maxSequence bounds an unfinished escape sequence: a longer one is garbage
// (or a flood), and is dropped instead of growing the buffer.
const maxSequence = 32

// Parser is a byte-stream key decoder. The zero value is ready to use.
type Parser struct{ buf []byte }

// Feed adds bytes and returns the keys that are complete. An escape sequence
// cut in half stays buffered for the next Feed; Pending says so.
func (p *Parser) Feed(b []byte) []Key {
	p.buf = append(p.buf, b...)
	var keys []Key
	for len(p.buf) > 0 {
		k, n, ok := parseOne(p.buf)
		if !ok { // incomplete: wait for more bytes
			if len(p.buf) > maxSequence {
				p.buf = nil
			}
			break
		}
		p.buf = p.buf[n:]
		if k != nil {
			keys = append(keys, *k)
		}
	}
	if len(p.buf) == 0 {
		p.buf = nil
	}
	return keys
}

// Pending reports whether bytes are waiting for the rest of a sequence. A
// lone Escape is such a case: it is Esc only if nothing follows soon.
func (p *Parser) Pending() bool { return len(p.buf) > 0 }

// Flush resolves what is buffered after a timeout: a lone Escape is the Esc
// key; the start of any other sequence is dropped.
func (p *Parser) Flush() []Key {
	defer func() { p.buf = nil }()
	if len(p.buf) == 1 && p.buf[0] == 0x1b {
		return []Key{{Kind: KeyEsc}}
	}
	return nil
}

// parseOne decodes the first key of b. It returns the key (nil for bytes
// that are consumed without meaning), the bytes used, and false when b is an
// incomplete prefix of a sequence.
func parseOne(b []byte) (*Key, int, bool) {
	c := b[0]
	switch {
	case c == 0x1b:
		return parseEscape(b)
	case c == '\r' || c == '\n':
		return &Key{Kind: KeyEnter}, 1, true
	case c == '\t':
		return &Key{Kind: KeyTab}, 1, true
	case c == 0x7f || c == 0x08:
		return &Key{Kind: KeyBackspace}, 1, true
	case c == 0x03:
		return &Key{Kind: KeyCtrlC}, 1, true
	case c == 0x1a:
		return &Key{Kind: KeyCtrlZ}, 1, true
	case c == 0x0c:
		return &Key{Kind: KeyCtrlL}, 1, true
	case c < 0x20:
		return &Key{Kind: KeyUnknown}, 1, true
	case c < utf8.RuneSelf:
		return &Key{Kind: KeyRune, Rune: rune(c)}, 1, true
	}
	if !utf8.FullRune(b) {
		return nil, 0, false
	}
	r, n := utf8.DecodeRune(b)
	if r == utf8.RuneError {
		return nil, 1, true // a stray byte: skip it
	}
	return &Key{Kind: KeyRune, Rune: r}, n, true
}

// parseEscape decodes ESC-led input: CSI (ESC [ ...), SS3 (ESC O x), or an
// Escape followed by something else (Esc, then that something).
func parseEscape(b []byte) (*Key, int, bool) {
	if len(b) == 1 {
		return nil, 0, false
	}
	switch b[1] {
	case '[':
		return parseCSI(b)
	case 'O':
		if len(b) < 3 {
			return nil, 0, false
		}
		return ss3Key(b[2]), 3, true
	}
	return &Key{Kind: KeyEsc}, 1, true
}

// parseCSI: ESC [ parameter bytes (0x30-0x3f) intermediate bytes (0x20-0x2f)
// final byte (0x40-0x7e). Unknown sequences are consumed and ignored, so a
// terminal's replies or a mouse report never turn into keystrokes.
func parseCSI(b []byte) (*Key, int, bool) {
	for i := 2; i < len(b); i++ {
		c := b[i]
		switch {
		case c >= 0x40 && c <= 0x7e:
			return csiKey(b[2:i], c), i + 1, true
		case c < 0x20 || c > 0x7e:
			return nil, i, true // malformed: drop what we read, keep this byte
		}
		// 0x20-0x3f: parameter and intermediate bytes, part of the sequence.
	}
	return nil, 0, false
}

func ss3Key(final byte) *Key {
	switch final {
	case 'A':
		return &Key{Kind: KeyUp}
	case 'B':
		return &Key{Kind: KeyDown}
	case 'C':
		return &Key{Kind: KeyRight}
	case 'D':
		return &Key{Kind: KeyLeft}
	case 'H':
		return &Key{Kind: KeyHome}
	case 'F':
		return &Key{Kind: KeyEnd}
	}
	return nil
}

// csiKey maps a CSI sequence to a key. Modifier parameters ("1;5A" is Ctrl-Up)
// are ignored: the arrow is the arrow.
func csiKey(params []byte, final byte) *Key {
	switch final {
	case 'A':
		return &Key{Kind: KeyUp}
	case 'B':
		return &Key{Kind: KeyDown}
	case 'C':
		return &Key{Kind: KeyRight}
	case 'D':
		return &Key{Kind: KeyLeft}
	case 'H':
		return &Key{Kind: KeyHome}
	case 'F':
		return &Key{Kind: KeyEnd}
	case 'Z':
		return &Key{Kind: KeyShiftTab}
	case '~':
		n := 0
		for _, c := range params {
			if c < '0' || c > '9' {
				break
			}
			n = n*10 + int(c-'0')
		}
		switch n {
		case 1, 7:
			return &Key{Kind: KeyHome}
		case 4, 8:
			return &Key{Kind: KeyEnd}
		case 5:
			return &Key{Kind: KeyPageUp}
		case 6:
			return &Key{Kind: KeyPageDown}
		case 3:
			return &Key{Kind: KeyDelete}
		}
	}
	return nil
}
