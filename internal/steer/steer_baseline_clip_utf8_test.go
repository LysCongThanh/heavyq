package steer

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// A baseline survives the trip through the session file: a failing command
// recorded at SessionStart must still read as pre-existing at Stop, even when
// the 300-byte clip lands inside a multi-byte rune or the text is not UTF-8.
func TestBaselineSurvivesSessionFileForNonASCIICommands(t *testing.T) {
	cases := []struct{ name, cmd string }{
		{"two-byte rune across the cut", strings.Repeat("a", 299) + "é tail"},
		{"three-byte rune, cut after its first byte", strings.Repeat("b", 299) + "— tail"},
		{"three-byte rune, cut after its second byte", strings.Repeat("c", 298) + "— tail"},
		{"vietnamese text near the cut", "sh -c 'echo " + strings.Repeat("x", 280) + " kiểm tra lỗi trước khi gộp'"},
		{"exact length ends on a full rune", strings.Repeat("d", 298) + "é"},
		{"short command with a stray non-UTF-8 byte", "make check MSG=caf\xe9"},
		{"long command with a stray byte past the cut", strings.Repeat("e", 310) + "\xff"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := Facts{Gate: &GateFact{Name: "fast", Failing: []FailedCmd{{Cmd: tc.cmd, Exit: 1}}}}
			base := f.AllFailing()
			if len(base) != 1 {
				t.Fatalf("AllFailing = %q", base)
			}
			if len(base[0]) > maxBaselineCmdLen || !utf8.ValidString(base[0]) {
				t.Fatalf("clipped baseline entry is %d bytes, valid UTF-8 %v", len(base[0]), utf8.ValidString(base[0]))
			}
			s := OpenSessions(t.TempDir())
			if err := s.Save("sess-1", SessionState{Started: time.Now(), HasBaseline: true, Baseline: base}); err != nil {
				t.Fatal(err)
			}
			got, ok := s.Load("sess-1")
			if !ok {
				t.Fatal("record not loaded back")
			}
			if !got.IsBaseline(tc.cmd) {
				t.Fatalf("pre-existing failure lost after the session file round trip: saved %q, reloaded %q", base[0], got.Baseline)
			}
			if got.IsBaseline("a different command") {
				t.Fatal("unrelated command matched the baseline")
			}
		})
	}
}
