package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/agentmode"
)

// Agent mode, end to end, with the real binary and CLAUDECODE=1 in its
// environment. The budgets here are the ones the roadmap promises: run
// chatter of at most three lines, a digest under 4 KB, a `why` under 600
// bytes, a help card under 900.

// newAgentRig is a rig whose heavyq runs under Claude Code.
func newAgentRig(t *testing.T) *rig {
	t.Helper()
	h := newRig(t, baseConfig)
	h.env = append(h.env, "CLAUDECODE=1")
	return h
}

// human is the same rig (same queue, ledger and directory) with agent mode off.
func (h *rig) human() *rig {
	c := *h
	c.env = nil
	for _, e := range h.env {
		if name, _, _ := strings.Cut(e, "="); !agentmode.IsAgentVar(name) {
			c.env = append(c.env, e)
		}
	}
	return &c
}

// heavyqLines are the lines heavyq itself wrote with its "heavyq:" prefix.
func heavyqLines(out string) []string {
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "heavyq:") || strings.HasPrefix(l, "heavyq-job:") { // the remote job's banner counts too
			lines = append(lines, l)
		}
	}
	return lines
}

func lastLine(out string) string {
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	return lines[len(lines)-1]
}

func noANSI(t *testing.T, out string) {
	t.Helper()
	if strings.Contains(out, "\x1b") {
		t.Fatalf("ANSI escape in agent output:\n%q", out)
	}
}

// digestOf is the digest at the end of out: from the last "heavyq: <VERDICT>" line.
var verdictLine = regexp.MustCompile(`(?m)^heavyq: (PASS|FAIL|REUSED|NOT RUN|CANCELLED|INTERRUPTED) `)

func digestOf(t *testing.T, out string) string {
	t.Helper()
	locs := verdictLine.FindAllStringIndex(out, -1)
	if len(locs) == 0 {
		t.Fatalf("no digest in:\n%s", out)
	}
	return out[locs[len(locs)-1][0]:]
}

func TestAgentModeRunSaysRunningThenTheDigest(t *testing.T) {
	h := newAgentRig(t)
	repo := gitRepo(t, h)
	rc, out := h.run("3", "--lane", "api", "--", "sh", "-c", "echo hello")
	noANSI(t, out)
	tree := gitOutput(t, h, repo, "rev-parse", "HEAD^{tree}")[:12]
	lines := heavyqLines(out)
	if rc != 0 || len(lines) != 2 {
		t.Fatalf("rc=%d, want exactly the running line and the digest:\n%s", rc, out)
	}
	if want := "heavyq: running sh -c echo hello (p3, lane api, local/heavy-1, tree " + tree + ")"; lines[0] != want {
		t.Fatalf("first line %q, want %q", lines[0], want)
	}
	if !strings.HasPrefix(lines[1], "heavyq: PASS sh -c echo hello · tree "+tree+" · ") {
		t.Fatalf("digest %q", lines[1])
	}
	if !(strings.Index(out, "running") < strings.Index(out, "hello\n") && strings.Index(out, "hello\n") < strings.Index(out, "PASS")) {
		t.Fatalf("order: the verdict starts the output and the digest ends it:\n%s", out)
	}
	if strings.Contains(out, "our turn") || strings.Contains(out, "next:") || strings.Contains(out, "log:") {
		t.Fatalf("a pass has no human chatter, no next: and no log:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(h.root, "logs")); !os.IsNotExist(err) {
		t.Fatalf("a passing run must leave no log behind: %v", err)
	}
}

// The acceptance test of the issue: a wait ends with at most three heavyq
// lines (queued, running, the digest). The 2-minute version of the policy runs
// on a fake clock in TestAgentTwoMinuteWaitSaysQueuedOnce; here a real wait
// crosses the real 2-second threshold.
func TestAgentModeWaitPrintsAtMostThreeHeavyqLines(t *testing.T) {
	h := newAgentRig(t)
	hold := h.start("4", "--lane", "holder", "--", "sh", "-c", h.gateFile("go"))
	h.running(1)
	w := h.start("4", "--lane", "waiter", "--", "sh", "-c", "echo waited-ok")
	h.waiting(1)
	h.until("the queued line", func() bool { return strings.Contains(w.out.String(), "heavyq: queued") })
	h.touch("go")
	if rc := hold.wait(t, 30*time.Second); rc != 0 {
		t.Fatalf("holder rc=%d\n%s", rc, hold.out)
	}
	if rc := w.wait(t, 30*time.Second); rc != 0 {
		t.Fatalf("waiter rc=%d\n%s", rc, w.out)
	}
	out := w.out.String()
	noANSI(t, out)
	lines := heavyqLines(out)
	if len(lines) != 3 {
		t.Fatalf("%d heavyq lines, want 3 (queued, running, digest):\n%s", len(lines), out)
	}
	if !strings.HasPrefix(lines[0], "heavyq: queued #1 of 1 on local (p4, lane waiter)") ||
		!strings.HasPrefix(lines[1], "heavyq: running sh -c echo waited-ok (p4, lane waiter, local/heavy-1") ||
		!strings.Contains(lines[1], ", waited ") || !strings.HasPrefix(lines[2], "heavyq: PASS ") {
		t.Fatalf("lines:\n%s", strings.Join(lines, "\n"))
	}
	if n := len(heavyqLines(hold.out.String())); n != 2 {
		t.Fatalf("an immediate admission is running + digest, got %d lines:\n%s", n, hold.out)
	}
}

func TestAgentModeFailureDigestIsBoundedAndKeepsTheFullLog(t *testing.T) {
	h := newAgentRig(t)
	const lines = 60000 // ~6 MB: past the 4 MiB log cap and far past the 64 KiB tail
	script := fmt.Sprintf(`seq 1 %d | sed 's|.*|pkg/f&_test.go:&: assertion failed with a deliberately long message to blow up the digest|'; echo '--- FAIL: TestLast'; exit 3`, lines)
	rc, out := h.run("3", "--lane", "big", "--", "sh", "-c", script)
	noANSI(t, out)
	if rc != 3 {
		t.Fatalf("rc=%d", rc)
	}
	// The raw stream is untouched: every line, then the digest.
	if n := len(regexp.MustCompile(`(?m)^pkg/f\d+_test\.go:\d+: assertion`).FindAllString(out, -1)); n != lines {
		t.Fatalf("%d raw lines reached the caller, want %d", n, lines)
	}
	if first := strings.SplitN(out, "\n", 2)[0]; !strings.HasPrefix(first, "heavyq: running sh -c ") {
		t.Fatalf("the verdict must open the output, got %q", first)
	}
	digest := digestOf(t, out)
	if len(digest) >= agentmode.MaxDigestBytes {
		t.Fatalf("digest is %d bytes, budget under %d:\n%s", len(digest), agentmode.MaxDigestBytes, digest)
	}
	dl := strings.Split(strings.TrimRight(digest, "\n"), "\n")
	if !strings.HasPrefix(dl[0], "heavyq: FAIL sh -c ") || !strings.Contains(dl[0], " · exit 3 · ") {
		t.Fatalf("digest header %q", dl[0])
	}
	if last := dl[len(dl)-1]; last != "next: fix the failure above, then re-run this command" {
		t.Fatalf("the digest must end with its one next: line, got %q", last)
	}
	quoted := 0
	for _, l := range dl {
		if strings.HasPrefix(l, "  pkg/f") {
			quoted++
		}
	}
	if quoted != agentmode.MaxFailureLines || !strings.Contains(digest, "more failure-like lines") {
		t.Fatalf("%d failure lines quoted:\n%s", quoted, digest)
	}
	// The log: full output (first 4 MiB), private, named in the digest.
	m := regexp.MustCompile(`(?m)^log: (\S+) \(first 4 MiB of the output\)$`).FindStringSubmatch(digest)
	if m == nil {
		t.Fatalf("no log line:\n%s", digest)
	}
	st, err := os.Stat(m[1])
	if err != nil || st.Size() != agentmode.LogMaxBytes || st.Mode().Perm() != 0o600 {
		t.Fatalf("log %s: %v %v", m[1], st, err)
	}
	if filepath.Dir(m[1]) != filepath.Join(h.root, "logs") {
		t.Fatalf("log dir %s: it belongs next to the ledger", filepath.Dir(m[1]))
	}
	if b, _ := os.ReadFile(m[1]); !strings.HasPrefix(string(b), "pkg/f1_test.go:1: assertion failed") {
		t.Fatal("the log must start with the command's first byte")
	}
}

func TestAgentModeFailureDigestQuotesTheFailuresOfASmallRun(t *testing.T) {
	h := newAgentRig(t)
	rc, out := h.run("3", "--lane", "t", "--gate", "ready", "--", "sh", "-c",
		"echo 'ok  pkg/a 0.1s'; echo '--- FAIL: TestB (0.00s)'; echo '    b_test.go:9: want 1 got 2'; echo FAIL; exit 1")
	if rc != 1 {
		t.Fatalf("rc=%d", rc)
	}
	d := digestOf(t, out)
	for _, want := range []string{"  --- FAIL: TestB (0.00s)\n", "  b_test.go:9: want 1 got 2\n", "  FAIL\n", "\nlog: ", "(full output, 0 KiB)\n",
		"\nnext: fix the failure above, then run: heavyq gate run ready\n"} {
		if !strings.Contains(d, want) {
			t.Fatalf("digest lacks %q:\n%s", want, d)
		}
	}
	if strings.Contains(d, "\n  ok  pkg/a") {
		t.Fatalf("a passing line was quoted:\n%s", d)
	}
}

func TestAgentModeMaxWaitSaysNotRunAndWhatToDo(t *testing.T) {
	h := newAgentRig(t)
	hold := h.start("4", "--lane", "holder", "--", "sh", "-c", h.gateFile("go"))
	h.running(1)
	rc, out := h.run("4", "--lane", "late", "--max-wait", "1", "--", "true")
	noANSI(t, out)
	if rc != ExitMaxWait {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
	// A slow machine may add the queued line and "(waited ...)"; nothing else.
	d := digestOf(t, out)
	lines := strings.Split(strings.TrimRight(d, "\n"), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "heavyq: NOT RUN true · exit 75") ||
		lines[1] != "  not our turn after 1s (--max-wait)" || lines[2] != "next: retry later; don't bypass heavyq" {
		t.Fatalf("digest:\n%s\nall output:\n%s", d, out)
	}
	if n := len(heavyqLines(out)); n > 2 {
		t.Fatalf("%d heavyq lines:\n%s", n, out)
	}
	h.touch("go")
	hold.wait(t, 30*time.Second)
}

func TestAgentModeCancelledRunsEndWithTheCancelledNext(t *testing.T) {
	h := newAgentRig(t)
	hold := h.start("5", "--lane", "holder", "--", "sh", "-c", "sleep 60")
	h.running(1)
	waiter := h.start("5", "--lane", "waiter", "--", "true")
	h.waiting(1)
	const next = "next: cancelled by operator; ask before re-running"
	if rc, out := h.run("cancel", strconv.Itoa(waiter.cmd.Process.Pid)); rc != 0 {
		t.Fatalf("cancel rc=%d: %s", rc, out)
	}
	if rc := waiter.wait(t, 20*time.Second); rc != ExitCancelled {
		t.Fatalf("waiter rc=%d\n%s", rc, waiter.out)
	}
	// The digest carries the reason (it never ran); there is no separate human line.
	if out := waiter.out.String(); !strings.Contains(out, "heavyq: CANCELLED true · exit 78") || lastLine(out) != next ||
		!strings.Contains(out, "\n  cancelled by an operator while waiting\n") || strings.Contains(out, "heavyq: cancelled by") {
		t.Fatalf("waiting cancel:\n%s", out)
	}
	if rc, _ := h.run("cancel", strconv.Itoa(hold.cmd.Process.Pid)); rc != 0 {
		t.Fatal("cancel running failed")
	}
	if rc := hold.wait(t, 20*time.Second); rc != ExitCancelled {
		t.Fatalf("holder rc=%d\n%s", rc, hold.out)
	}
	out := hold.out.String()
	if n := len(heavyqLines(out)); n != 2 || !strings.Contains(out, "heavyq: CANCELLED sh -c sleep 60 · exit 78") || lastLine(out) != next {
		t.Fatalf("running cancel, %d heavyq lines:\n%s", n, out)
	}
}

func TestAgentModeReusedResultSaysDoNotRerun(t *testing.T) {
	h := newAgentRig(t)
	gitRepo(t, h)
	run := []string{"3", "--cache", "--lane", "api", "--", "sh", "-c", "echo built"}
	if rc, out := h.run(run...); rc != 0 || !strings.Contains(digestOf(t, out), "heavyq: PASS ") {
		t.Fatalf("first run rc=%d\n%s", rc, out)
	}
	rc, out := h.run(run...)
	noANSI(t, out)
	lines := heavyqLines(out)
	if rc != 0 || len(lines) != 1 || !strings.HasPrefix(lines[0], "heavyq: REUSED sh -c echo built · tree ") {
		t.Fatalf("rc=%d, want built + one REUSED line:\n%s", rc, out)
	}
	if !regexp.MustCompile(`(?m)^  already passed on this exact tree at \d{4}-\d\d-\d\d \d\d:\d\d \(lane api\); do not re-run$`).MatchString(out) {
		t.Fatalf("the replay must say not to re-run:\n%s", out)
	}
	if !strings.Contains(out, "built\n") || strings.Contains(out, "cached result from") || strings.Contains(out, "next:") {
		t.Fatalf("output:\n%s", out)
	}
}

func TestAgentModeReusedFailureIsNotPassedOff(t *testing.T) {
	h := newRig(t, baseConfig+"[cache]\ncache_failures = true\n")
	h.env = append(h.env, "CLAUDECODE=1")
	gitRepo(t, h)
	run := []string{"3", "--cache", "--lane", "api", "--", "sh", "-c", "echo 'a_test.go:4: boom'; exit 2"}
	if rc, _ := h.run(run...); rc != 2 {
		t.Fatalf("first rc=%d", rc)
	}
	rc, out := h.run(run...)
	d := digestOf(t, out)
	if rc != 2 || !strings.HasPrefix(d, "heavyq: REUSED sh -c ") || !strings.Contains(d, " · exit 2 · ") ||
		!strings.Contains(d, "  already failed (exit 2) on this exact tree at ") || !strings.Contains(d, "\n  a_test.go:4: boom\n") ||
		lastLine(d) != "next: change the code, then re-run this command" {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
}
