package width

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// The same text composed (NFC) and decomposed (NFD, what macOS file names
// give). Both take 12 cells; the byte lengths differ.
const (
	nfcText = "caf\u00e9 au lait"
	nfdText = "cafe\u0301 au lait"
)

func TestStringTable(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int
	}{
		{"empty", "", 0},
		{"ascii", "heavyq top", 10},
		{"composed", nfcText, 12},
		{"decomposed", nfdText, 12},
		{"combining acute", "e\u0301", 1},
		{"cjk", "日本語", 6},
		{"hangul", "한글", 4},
		{"fullwidth latin", "ＡＢ", 4},
		{"emoji", "🔥", 2},
		{"zwj is zero", "a\u200db", 2},
		{"variation selector", "☁\ufe0f", 1},
		{"control chars are zero", "a\x1b[31mb", 6}, // the ESC is 0 but "[31m" are printable: callers must not pass SGR
		{"tab is zero", "a\tb", 2},
		{"glyphs used by the screen", "●▲▮▯─┃⏎…", 8},
		{"invalid byte", "a\xffb", 3},
	}
	for _, tc := range tests {
		if got := String(tc.in); got != tc.want {
			t.Errorf("%s: String(%q) = %d, want %d", tc.name, tc.in, got, tc.want)
		}
	}
}

func TestNFCAndNFDMeasureTheSame(t *testing.T) {
	if len(nfcText) == len(nfdText) {
		t.Fatal("fixture error: NFC and NFD must differ in bytes")
	}
	if a, b := String(nfcText), String(nfdText); a != 12 || b != 12 {
		t.Fatalf("widths NFC=%d NFD=%d, want 12 and 12", a, b)
	}
}

func TestTruncateNeverSplitsAGrapheme(t *testing.T) {
	for _, in := range []string{nfcText, nfdText, "日本語のテキスト", "e\u0301e\u0301e\u0301e\u0301", "a\u200db\u200dc"} {
		for max := 0; max <= String(in)+2; max++ {
			got := Truncate(in, max, "")
			if w := String(got); w > max {
				t.Fatalf("Truncate(%q, %d) = %q is %d cells wide", in, max, got, w)
			}
			if !utf8.ValidString(got) {
				t.Fatalf("Truncate(%q, %d) = %q is not valid UTF-8", in, max, got)
			}
			if rest := strings.TrimPrefix(in, got); rest != "" {
				if r, _ := utf8.DecodeRuneInString(rest); Rune(r) == 0 {
					t.Fatalf("Truncate(%q, %d) = %q left a combining mark behind its base", in, max, got)
				}
			}
			if !strings.HasPrefix(in, got) {
				t.Fatalf("Truncate(%q, %d) = %q is not a prefix", in, max, got)
			}
		}
	}
}

func TestTruncateTable(t *testing.T) {
	tests := []struct {
		in       string
		max      int
		ellipsis string
		want     string
	}{
		{"hello", 10, "…", "hello"},
		{"hello", 5, "…", "hello"},
		{"hello world", 8, "…", "hello w…"},
		{"hello world", 8, "", "hello wo"},
		{"hello", 0, "…", ""},
		{"hello", 1, "…", "…"},
		{"日本語", 5, "", "日本"},
		{"日本語", 4, "…", "日…"},
		{nfcText, 6, "…", "caf\u00e9 …"},
		{nfdText, 6, "…", "cafe\u0301 …"},
		{"ab", 3, "...", "ab"},
		{"abcdef", 2, "...", "ab"}, // the ellipsis does not fit: plain cut
	}
	for _, tc := range tests {
		if got := Truncate(tc.in, tc.max, tc.ellipsis); got != tc.want {
			t.Errorf("Truncate(%q, %d, %q) = %q, want %q", tc.in, tc.max, tc.ellipsis, got, tc.want)
		}
	}
}

func TestPad(t *testing.T) {
	if got := Pad(nfdText, 14); String(got) != 14 || !strings.HasPrefix(got, nfdText) {
		t.Errorf("Pad NFD = %q (%d cells)", got, String(got))
	}
	if got := PadLeft("日本", 6); got != "  日本" {
		t.Errorf("PadLeft = %q", got)
	}
	if got := Pad("longer than w", 4); got != "longer than w" {
		t.Errorf("Pad shortened: %q", got)
	}
}

func TestWrap(t *testing.T) {
	got := Wrap("1 agent waits because linuxi is over its load limit.", 20)
	want := []string{"1 agent waits", "because linuxi is", "over its load limit."}
	if len(got) != len(want) {
		t.Fatalf("Wrap = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
	for _, l := range Wrap("supercalifragilisticexpialidocious and 日本語日本語日本語", 8) {
		if String(l) > 8 {
			t.Errorf("wrapped line %q is %d cells", l, String(l))
		}
	}
	if got := Wrap("日 x", 1); len(got) == 0 {
		t.Errorf("a character wider than the line must not loop or vanish silently: %q", got)
	}
	if Wrap("x", 0) != nil {
		t.Error("width 0 wraps to nothing")
	}
}

func TestWideTableIsSortedAndDisjoint(t *testing.T) {
	for i, r := range wide {
		if r[0] > r[1] {
			t.Fatalf("range %d is inverted: %x", i, r)
		}
		if i > 0 && r[0] <= wide[i-1][1] {
			t.Fatalf("range %d overlaps or is out of order: %x after %x", i, r, wide[i-1])
		}
	}
}
