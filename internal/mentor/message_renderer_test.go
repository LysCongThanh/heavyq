package mentor

import (
	"strings"
	"testing"
)

func TestRenderMessageGolden(t *testing.T) {
	base := Signal{Rule: RuleRepeatFailure, Exit: 1, Cmd: "go test ./...", Tree: "0123456789abcdef0123"}
	cases := []struct {
		name string
		sig  Signal
		want string
	}{
		{"2nd failure is a fact only", withLevel(base, LevelFact, 2),
			"[heavyq · tool data] `go test ./...` exited 1 twice in a row on the same tree (0123456789ab)."},
		{"3rd failure is a directive with Next", withLevel(base, LevelDirective, 3),
			"[heavyq · tool data] `go test ./...` exited 1 3 times in a row on the same tree (0123456789ab). " +
				"Next: change the code or its inputs before rerunning, or report BLOCKED with needs:."},
	}
	for _, c := range cases {
		got := RenderMessage(c.sig)
		if got != c.want {
			t.Errorf("%s:\n got  %s\n want %s", c.name, got, c.want)
		}
		if strings.Count(got, "\n") != 0 || !strings.HasPrefix(got, Prefix) {
			t.Errorf("%s: must be one line starting with %q", c.name, Prefix)
		}
	}
	if strings.Contains(RenderMessage(withLevel(base, LevelFact, 2)), "Next:") {
		t.Error("the fact-only note must not carry a Next")
	}
}

func withLevel(s Signal, level, streak int) Signal {
	s.Level, s.Streak = level, streak
	return s
}
