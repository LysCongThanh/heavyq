package width

import (
	"strings"
	"testing"
)

// Text that skipped JSON (an anchor file's `needs:` span saved in Latin-1, a
// repo directory name) can carry invalid bytes. Each one is shown as one
// replacement cell and is one byte long: a cut must count it that way, or it
// keeps bytes past the budget, splits the next rune, or slices past the end.
var invalidUTF8Inputs = []string{
	"abcd\xffg",            // invalid byte one before the end
	"abcd\xff",             // invalid byte last
	"ab\xffcd",             // invalid byte in the middle
	"ab\xff\xe4\xb8\x96cd", // invalid byte before a CJK rune
	"caf\xe9x",             // Latin-1 é
	"caf\xe9.",             // the `needs: caf\xe9.` exception text tail
	"\xff\xfe\xfd",         // nothing but invalid bytes
	"e\u0301\xffe\u0301",   // marks around an invalid byte
}

func TestTruncateInvalidUTF8StaysInBudget(t *testing.T) {
	for _, in := range invalidUTF8Inputs {
		for _, ell := range []string{"", "…"} {
			for max := 0; max <= String(in)+2; max++ {
				got := mustNotPanic(t, in, func() string { return Truncate(in, max, ell) })
				if w := String(got); w > max {
					t.Errorf("Truncate(%q, %d, %q) = %q is %d cells wide", in, max, ell, got, w)
				}
				if !strings.HasPrefix(in, strings.TrimSuffix(got, ell)) {
					t.Errorf("Truncate(%q, %d, %q) = %q does not keep a prefix", in, max, ell, got)
				}
			}
		}
	}
}

func TestTruncateInvalidUTF8Table(t *testing.T) {
	tests := []struct {
		in       string
		max      int
		ellipsis string
		want     string
	}{
		{"abcd\xffg", 5, "", "abcd\xff"},
		{"ab\xffcd", 3, "", "ab\xff"},
		{"ab\xffcd", 4, "…", "ab\xff…"},
		{"ab\xff\xe4\xb8\x96cd", 4, "", "ab\xff"}, // never half of 世
		{"caf\xe9.", 4, "", "caf\xe9"},
	}
	for _, tc := range tests {
		got := mustNotPanic(t, tc.in, func() string { return Truncate(tc.in, tc.max, tc.ellipsis) })
		if got != tc.want {
			t.Errorf("Truncate(%q, %d, %q) = %q, want %q", tc.in, tc.max, tc.ellipsis, got, tc.want)
		}
	}
}

func TestWrapInvalidUTF8KeepsEveryByteOnce(t *testing.T) {
	for _, in := range invalidUTF8Inputs {
		for w := 1; w <= String(in)+1; w++ {
			var lines []string
			mustNotPanic(t, in, func() string { lines = Wrap(in, w); return "" })
			for _, l := range lines {
				if String(l) > w {
					t.Errorf("Wrap(%q, %d) line %q is %d cells", in, w, l, String(l))
				}
			}
			// Single words wider than w are split, never dropped or duplicated,
			// unless one character alone is wider than w (none here are at w>=2).
			if w >= 2 {
				if got := strings.Join(lines, ""); got != strings.Join(strings.Fields(in), "") {
					t.Errorf("Wrap(%q, %d) = %q lost or split bytes", in, w, lines)
				}
			}
		}
	}
	got := Wrap("ab\xff\xe4\xb8\x96cd", 3)
	want := []string{"ab\xff", "\xe4\xb8\x96c", "d"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("Wrap split the CJK rune: %q, want %q", got, want)
	}
}

// mustNotPanic runs f and turns a panic into a test failure naming the input,
// so one bad case does not hide the rest of the table.
func mustNotPanic(t *testing.T, in string, f func() string) (out string) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("panic on %q: %v", in, r)
		}
	}()
	return f()
}
