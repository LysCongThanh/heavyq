package agentmode

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestDigestPass(t *testing.T) {
	got := Result{Verdict: Pass, Cmd: "go test ./...", Tree: "3f2a9c1d7b0e", Dur: 12400 * time.Millisecond}.Text()
	if want := "heavyq: PASS go test ./... · tree 3f2a9c1d7b0e · 12s\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	short := Result{Verdict: Pass, Cmd: "true", Dur: 400 * time.Millisecond, Waited: 3 * time.Second}.Text()
	if want := "heavyq: PASS true · 0.4s (waited 3s)\n"; short != want {
		t.Fatalf("got %q, want %q", short, want)
	}
}

func TestDigestFailWithEverything(t *testing.T) {
	got := Result{Verdict: Fail, Cmd: "go test ./...", Exit: 1, Tree: "3f2a9c1d7b0e", Dur: 65 * time.Second,
		Failures: []string{"--- FAIL: TestX (0.00s)", "x_test.go:12: got 1, want 2"}, More: 3,
		Log:  &Log{Path: "/state/heavyq/logs/r.log", Size: 2048},
		Next: "fix the failure above, then re-run this command"}.Text()
	want := `heavyq: FAIL go test ./... · exit 1 · tree 3f2a9c1d7b0e · 1m5s
  --- FAIL: TestX (0.00s)
  x_test.go:12: got 1, want 2
  ... +3 more failure-like lines in the output
log: /state/heavyq/logs/r.log (full output, 2 KiB)
next: fix the failure above, then re-run this command
`
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestDigestReusedAndNotRun(t *testing.T) {
	got := Result{Verdict: Reused, Cmd: "go vet ./...", Tree: "abc", Dur: 10 * time.Millisecond,
		Detail: "already passed on this exact tree at 2026-11-02 01:02 (lane wt-a); do not re-run"}.Text()
	want := "heavyq: REUSED go vet ./... · tree abc · 0.0s\n  already passed on this exact tree at 2026-11-02 01:02 (lane wt-a); do not re-run\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	got = Result{Verdict: NotRun, Cmd: "make test", Exit: 75, Dur: 0, Why: "not our turn after 5m0s (--max-wait)",
		Next: "retry later; don't bypass heavyq"}.Text()
	want = "heavyq: NOT RUN make test · exit 75\n  not our turn after 5m0s (--max-wait)\nnext: retry later; don't bypass heavyq\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// The budget: whatever a command prints, the digest stays under 4 KB and
// keeps its header and its final next: line.
func TestDigestStaysUnderTheBudgetWithHugeFailingOutput(t *testing.T) {
	var out strings.Builder
	for i := 0; i < 5000; i++ {
		fmt.Fprintf(&out, "pkg/very/long/path/to/some/file_%d_test.go:%d: %s\n", i, i, strings.Repeat("assertion failed with a long message ", 12))
	}
	c := NewCapture("", "")
	_, _ = c.Wrap(discard{}).Write([]byte(out.String()))
	tail, cut := c.Tail()
	lines, more := FailureLines(tail, cut, MaxFailureLines)
	r := Result{Verdict: Fail, Cmd: strings.Repeat("go test -run TestSomethingLong ", 20), Exit: 1, Tree: "3f2a9c1d7b0e+0123abcd",
		Dur: time.Hour, Waited: time.Minute, Failures: lines, More: more,
		Log:  &Log{Path: "/home/someone/.local/state/heavyq/logs/20261102T010203.123456Z-12345-a-lane.log", Size: LogMaxBytes, Truncated: true},
		Next: "fix the failure above, then re-run this command"}
	for _, s := range []string{r.Text(), r.JSON()} {
		if len(s) >= MaxDigestBytes {
			t.Fatalf("digest is %d bytes, budget is under %d:\n%s", len(s), MaxDigestBytes, s)
		}
		if !strings.Contains(s, "next:") && !strings.Contains(s, `"next"`) {
			t.Fatalf("lost the next step:\n%s", s)
		}
	}
	if len(lines) != MaxFailureLines {
		t.Fatalf("%d failure lines, want %d", len(lines), MaxFailureLines)
	}
}

func TestDigestDropsFailureLinesToFit(t *testing.T) {
	lines := make([]string, MaxFailureLines)
	for i := range lines {
		lines[i] = strings.Repeat("x", maxLineBytes)
	}
	r := Result{Verdict: Fail, Cmd: strings.Repeat("c", 500), Exit: 1, Failures: lines,
		Why: strings.Repeat("w", 900), Detail: strings.Repeat("d", 900), Next: strings.Repeat("n", 900),
		Log: &Log{Path: strings.Repeat("p", 500)}}
	s := r.Text()
	if len(s) >= MaxDigestBytes || !strings.HasSuffix(s, "...\n") || !strings.Contains(s, "\nnext: ") {
		t.Fatalf("%d bytes:\n%s", len(s), s)
	}
}

func TestDigestHasNoControlBytes(t *testing.T) {
	lines, _ := FailureLines([]byte("\x1b[31mFAIL\x1b[0m x\n"), false, MaxFailureLines)
	s := Result{Verdict: Fail, Cmd: "x", Exit: 1, Failures: lines}.Text()
	if strings.Contains(s, "\x1b") {
		t.Fatalf("ANSI in digest: %q", s)
	}
}

func TestDigestJSON(t *testing.T) {
	r := Result{Verdict: Fail, Cmd: "go test", Exit: 1, Tree: "abc", Dur: 1500 * time.Millisecond,
		Failures: []string{"a_test.go:1: boom"}, More: 2, Log: &Log{Path: "/l.log"}, Next: "fix it"}
	s := r.JSON()
	if strings.Count(s, "\n") != 1 || !strings.HasSuffix(s, "\n") {
		t.Fatalf("want one line, got %q", s)
	}
	var d map[string]any
	if err := json.Unmarshal([]byte(s), &d); err != nil {
		t.Fatalf("not JSON: %v: %q", err, s)
	}
	for k, want := range map[string]any{"heavyq": "digest", "verdict": "FAIL", "cmd": "go test", "exit": 1.0,
		"tree": "abc", "secs": 1.5, "more_failures": 2.0, "log": "/l.log", "next": "fix it"} {
		if d[k] != want {
			t.Errorf("%s = %v, want %v", k, d[k], want)
		}
	}
	if f, _ := d["failures"].([]any); len(f) != 1 || f[0] != "a_test.go:1: boom" {
		t.Errorf("failures = %v", d["failures"])
	}
}

func TestCardBudget(t *testing.T) {
	if len(Card) > CardMaxBytes {
		t.Fatalf("card is %d bytes, budget %d (about 200 tokens)", len(Card), CardMaxBytes)
	}
	for _, want := range []string{"75", "78", "76/77", "retry", "never bypass", "ask", "gate status", "gate run",
		"why", "next:", "REUSED", "do not re-run", "exit"} {
		if !strings.Contains(Card, want) {
			t.Errorf("card does not mention %q", want)
		}
	}
	if strings.ContainsRune(Card, '\x1b') {
		t.Error("ANSI in the card")
	}
}

func TestCapLines(t *testing.T) {
	short := []string{"a", "b"}
	if got := CapLines(short, 600, "more"); strings.Join(got, "\n") != "a\nb" {
		t.Fatalf("a short text is untouched, got %q", got)
	}
	var many []string
	for i := 0; i < 100; i++ {
		many = append(many, fmt.Sprintf("line %03d of a long story about the queue", i))
	}
	got := CapLines(many, WhyMaxBytes, "... (heavyq status has the rest)")
	if n := len(strings.Join(got, "\n")) + 1; n > WhyMaxBytes {
		t.Fatalf("%d bytes", n)
	}
	if got[len(got)-1] != "... (heavyq status has the rest)" || got[0] != many[0] {
		t.Fatalf("got %q", got)
	}
	huge := CapLines([]string{strings.Repeat("é", 2000)}, WhyMaxBytes, "...more")
	if n := len(strings.Join(huge, "\n")) + 1; n > WhyMaxBytes || len(huge) != 2 {
		t.Fatalf("%d bytes, %d lines", n, len(huge))
	}
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
