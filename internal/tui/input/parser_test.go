package input

import (
	"reflect"
	"testing"
)

func r(c rune) Key       { return Key{Kind: KeyRune, Rune: c} }
func k(kind KeyKind) Key { return Key{Kind: kind} }

func TestParserTable(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []Key
	}{
		{"letters", "jkgG", []Key{r('j'), r('k'), r('g'), r('G')}},
		{"punctuation keys", "/r?q+-", []Key{r('/'), r('r'), r('?'), r('q'), r('+'), r('-')}},
		{"c p y n", "cpyn", []Key{r('c'), r('p'), r('y'), r('n')}},
		{"arrows CSI", "\x1b[A\x1b[B\x1b[C\x1b[D", []Key{k(KeyUp), k(KeyDown), k(KeyRight), k(KeyLeft)}},
		{"arrows SS3 (application cursor keys)", "\x1bOA\x1bOB\x1bOC\x1bOD", []Key{k(KeyUp), k(KeyDown), k(KeyRight), k(KeyLeft)}},
		{"arrow with modifiers", "\x1b[1;5A\x1b[1;2B", []Key{k(KeyUp), k(KeyDown)}},
		{"home end", "\x1b[H\x1b[F\x1bOH\x1bOF\x1b[1~\x1b[4~\x1b[7~\x1b[8~", []Key{k(KeyHome), k(KeyEnd), k(KeyHome), k(KeyEnd), k(KeyHome), k(KeyEnd), k(KeyHome), k(KeyEnd)}},
		{"page keys and delete", "\x1b[5~\x1b[6~\x1b[3~", []Key{k(KeyPageUp), k(KeyPageDown), k(KeyDelete)}},
		{"tab shift-tab", "\t\x1b[Z", []Key{k(KeyTab), k(KeyShiftTab)}},
		{"enter CR and LF", "\r\n", []Key{k(KeyEnter), k(KeyEnter)}},
		{"backspace DEL and BS", "\x7f\x08", []Key{k(KeyBackspace), k(KeyBackspace)}},
		{"ctrl keys", "\x03\x1a\x0c", []Key{k(KeyCtrlC), k(KeyCtrlZ), k(KeyCtrlL)}},
		{"other control byte", "\x01", []Key{k(KeyUnknown)}},
		{"utf8 runes", "ế日", []Key{r('ế'), r('日')}},
		{"unknown CSI is swallowed", "a\x1b[200~b", []Key{r('a'), r('b')}},
		{"mouse report is swallowed", "\x1b[<0;10;5Mx", []Key{r('x')}},
		{"terminal reply is swallowed", "\x1b[?1;2cx", []Key{r('x')}},
		{"esc then other is esc plus key", "\x1bx", []Key{k(KeyEsc), r('x')}},
		{"stray high byte is skipped", "a\xffb", []Key{r('a'), r('b')}},
		{"malformed CSI keeps the next byte", "\x1b[\x01x", []Key{k(KeyUnknown), r('x')}},
	}
	for _, tc := range tests {
		var p Parser
		got := p.Feed([]byte(tc.in))
		if p.Pending() {
			t.Errorf("%s: parser left %q pending", tc.name, p.buf)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: Feed(%q) = %v, want %v", tc.name, tc.in, got, tc.want)
		}
	}
}

func TestLoneEscapeNeedsATimeout(t *testing.T) {
	var p Parser
	if got := p.Feed([]byte{0x1b}); len(got) != 0 || !p.Pending() {
		t.Fatalf("lone ESC must wait: keys %v, pending %v", got, p.Pending())
	}
	if got := p.Flush(); !reflect.DeepEqual(got, []Key{k(KeyEsc)}) {
		t.Fatalf("Flush = %v, want Esc", got)
	}
	if p.Pending() {
		t.Fatal("Flush left bytes pending")
	}
}

func TestSequenceSplitAcrossReads(t *testing.T) {
	for _, in := range []string{"\x1b[A", "\x1bOA", "\x1b[1;5A", "\x1b[5~"} {
		for cut := 1; cut < len(in); cut++ {
			var p Parser
			first := p.Feed([]byte(in[:cut]))
			second := p.Feed([]byte(in[cut:]))
			got := append(first, second...)
			if len(got) != 1 || p.Pending() {
				t.Errorf("%q cut at %d: keys %v pending %v", in, cut, got, p.Pending())
			}
		}
	}
	var p Parser // a multi-byte rune split in two reads
	b := []byte("ế")
	if got := p.Feed(b[:1]); len(got) != 0 || !p.Pending() {
		t.Fatalf("half a rune: keys %v", got)
	}
	if got := p.Feed(b[1:]); !reflect.DeepEqual(got, []Key{r('ế')}) {
		t.Fatalf("rest of the rune: %v", got)
	}
}

func TestFlushDropsAnUnfinishedSequence(t *testing.T) {
	var p Parser
	p.Feed([]byte("\x1b[1;"))
	if got := p.Flush(); len(got) != 0 || p.Pending() {
		t.Fatalf("Flush of a partial CSI = %v, pending %v", got, p.Pending())
	}
}

func TestRunawaySequenceIsBounded(t *testing.T) {
	var p Parser
	junk := append([]byte("\x1b["), make([]byte, 0)...)
	for i := 0; i < 100; i++ {
		junk = append(junk, '1')
	}
	p.Feed(junk)
	if p.Pending() {
		t.Fatalf("a 100-byte unfinished sequence stayed buffered (%d bytes)", len(p.buf))
	}
	if got := p.Feed([]byte("q")); !reflect.DeepEqual(got, []Key{r('q')}) {
		t.Fatalf("parser did not recover: %v", got)
	}
}

func TestMapTable(t *testing.T) {
	tests := []struct {
		key  Key
		want Command
	}{
		{r('j'), CmdDown}, {k(KeyDown), CmdDown}, {r('k'), CmdUp}, {k(KeyUp), CmdUp},
		{r('g'), CmdTop}, {k(KeyHome), CmdTop}, {r('G'), CmdBottom}, {k(KeyEnd), CmdBottom},
		{k(KeyPageUp), CmdPageUp}, {k(KeyPageDown), CmdPageDown},
		{k(KeyTab), CmdNextSection}, {k(KeyShiftTab), CmdPrevSection},
		{k(KeyEnter), CmdWhy}, {k(KeyEsc), CmdBack},
		{r('/'), CmdFilter}, {r('r'), CmdRefresh}, {r('?'), CmdHelp},
		{r('q'), CmdQuit}, {k(KeyCtrlC), CmdQuit}, {k(KeyCtrlZ), CmdSuspend}, {k(KeyCtrlL), CmdRedraw},
		{r('c'), CmdCancel}, {r('+'), CmdRaise}, {r('='), CmdRaise}, {r('-'), CmdLower},
		{r('m'), CmdMode}, {r('p'), CmdPause}, {r('y'), CmdYes}, {r('Y'), CmdYes}, {r('n'), CmdNo}, {r('N'), CmdNo},
		{r('x'), CmdNone}, {k(KeyLeft), CmdNone}, {k(KeyUnknown), CmdNone}, {k(KeyBackspace), CmdNone},
	}
	for _, tc := range tests {
		if got := Map(tc.key); got != tc.want {
			t.Errorf("Map(%+v) = %v, want %v", tc.key, got, tc.want)
		}
	}
}

func FuzzParserNeverPanicsAndAlwaysDrains(f *testing.F) {
	for _, s := range []string{"\x1b[A", "\x1b", "\x1b[", "\xe1\xba", "\x1b[<0;1;1M", "jk\x1bOA", "\x1b[1;5~"} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		var p Parser
		p.Feed(b)
		p.Flush()
		if p.Pending() {
			t.Fatal("Flush must leave nothing pending")
		}
	})
}
