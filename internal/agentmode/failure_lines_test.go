package agentmode

import (
	"fmt"
	"strings"
	"testing"
)

func TestFailureLinesRecognisesWhatFailuresPrint(t *testing.T) {
	for _, c := range []struct{ name, line string }{
		{"go test", "--- FAIL: TestRun (0.01s)"},
		{"go package", "FAIL\tgithub.com/x/y\t0.012s"},
		{"go location", "    run_test.go:42: got 1, want 2"},
		{"go compile", "./main.go:10:2: undefined: foo"},
		{"go panic", "panic: runtime error: index out of range [3] with length 3"},
		{"rust", "error[E0425]: cannot find value `x` in this scope"},
		{"cc", "src/a.c:7:3: error: expected ';' before '}' token"},
		{"jest", "  ✕ adds numbers (3 ms)"},
		{"pytest", "FAILED tests/test_x.py::test_a - assert 1 == 2"},
		{"python", "Traceback (most recent call last):"},
		{"python assert", "AssertionError: 1 != 2"},
		{"node", "TypeError: Cannot read properties of undefined"},
		{"plain", "Error: connect ECONNREFUSED"},
		{"ts location", "src/app.ts:41"},
	} {
		got, more := FailureLines([]byte("noise before\n"+c.line+"\nnoise after\n"), false, MaxFailureLines)
		if len(got) != 1 || got[0] != strings.TrimSpace(c.line) || more != 0 {
			t.Errorf("%s: got %q more=%d, want only %q", c.name, got, more, strings.TrimSpace(c.line))
		}
	}
}

func TestFailureLinesIgnoresNoise(t *testing.T) {
	out := "ok  \tpkg\t0.1s\n--- PASS: TestFailHandling\n0 failures\nListening on http://127.0.0.1:3000\n"
	got, _ := FailureLines([]byte(out), false, MaxFailureLines)
	// Nothing looks like a failure (an IP and port is not a source location,
	// and neither is a test named *Fail*), so the last lines stand in.
	want := []string{"--- PASS: TestFailHandling", "0 failures", "Listening on http://127.0.0.1:3000"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q, want the last %d lines %q", got, fallbackLines, want)
	}
}

func TestFailureLinesCapDedupeAndCount(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&b, "x_test.go:%d: boom\n", i)
		b.WriteString("FAIL\n") // the same line again and again counts once
	}
	got, more := FailureLines([]byte(b.String()), false, MaxFailureLines)
	if len(got) != MaxFailureLines || more != 30+1-MaxFailureLines {
		t.Fatalf("len=%d more=%d", len(got), more)
	}
	if got[0] != "x_test.go:0: boom" || got[1] != "FAIL" || got[2] != "x_test.go:1: boom" {
		t.Fatalf("order not kept: %q", got[:3])
	}
}

func TestFailureLinesStripsAnsiRedrawsAndControlBytes(t *testing.T) {
	out := "\x1b[31mFAIL\x1b[0m pkg/a\n" +
		"progress 10%\rprogress 100%\n" +
		"\x1b]0;title\x07\x1b[1;31merror: bad\x1b[0m\r\n" +
		"x_test.go:3: \x00weird\x07\n"
	got, _ := FailureLines([]byte(out), false, MaxFailureLines)
	want := []string{"FAIL pkg/a", "error: bad", "x_test.go:3: weird"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q, want %q", got, want)
	}
	for _, l := range got {
		if strings.ContainsAny(l, "\x1b\r\x00\x07") {
			t.Fatalf("control bytes survived in %q", l)
		}
	}
}

func TestFailureLinesDropsTheCutFirstLine(t *testing.T) {
	out := "ail: partial FAIL fragment\nx_test.go:1: real\n"
	got, _ := FailureLines([]byte(out), true, MaxFailureLines)
	if len(got) != 1 || got[0] != "x_test.go:1: real" {
		t.Fatalf("got %q", got)
	}
	got, _ = FailureLines([]byte(out), false, MaxFailureLines)
	if len(got) != 2 {
		t.Fatalf("an uncut output keeps its first line: %q", got)
	}
}

func TestFailureLinesClipsLongLines(t *testing.T) {
	got, _ := FailureLines([]byte("FAIL "+strings.Repeat("é", 500)+"\n"), false, MaxFailureLines)
	if len(got) != 1 || len(got[0]) > maxLineBytes || !strings.HasSuffix(got[0], "...") {
		t.Fatalf("len=%d %q", len(got[0]), got[0])
	}
}

func TestFailureLinesOfNothing(t *testing.T) {
	if got, more := FailureLines(nil, false, MaxFailureLines); len(got) != 0 || more != 0 {
		t.Fatalf("got %q %d", got, more)
	}
}
