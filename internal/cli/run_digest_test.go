package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/agentmode"
	"github.com/LysCongThanh/heavyq/internal/gitutil"
	"github.com/LysCongThanh/heavyq/internal/queue"
)

func TestShortTreeID(t *testing.T) {
	const full = "0123456789abcdef0123456789abcdef01234567"
	for _, c := range []struct {
		tree  string
		dirty bool
		want  string
	}{
		{"", false, ""},
		{full, false, "0123456789ab"},
		{full + "+fedcba9876543210", true, "0123456789ab+fedcba98"},
		{full, true, "0123456789ab+edits"},
		{"abc", false, "abc"},
	} {
		if got := shortTreeID(c.tree, c.dirty); got != c.want {
			t.Errorf("shortTreeID(%q, %v) = %q, want %q", c.tree, c.dirty, got, c.want)
		}
	}
}

// digestRun is a run that ends in mode, with stderr captured.
func digestRun(t *testing.T, mode agentmode.Mode, argv ...string) (*run, *bytes.Buffer) {
	t.Helper()
	t.Setenv("HEAVYQ_HISTORY", filepath.Join(t.TempDir(), "history.jsonl")) // logs go next to it
	var buf bytes.Buffer
	r := &run{opts: runOptions{Argv: argv}, stderr: &buf, t0: time.Now().Add(-3 * time.Second),
		me: &queue.Ticket{Cmd: queue.TruncateCmd(argv), Lane: "wt-a", Prio: 3}}
	r.out = newRunOutput(mode, false, &buf, r.t0, "wt-a")
	return r, &buf
}

func TestDigestRanClassification(t *testing.T) {
	tree := gitutil.Checkout{Tree: "0123456789abcdef0123456789abcdef01234567"}
	for _, c := range []struct {
		name      string
		rc        int
		machine   string
		gate      string
		cancelled bool
		interrupt bool
		distrust  string
		setupFail bool     // heavyq itself could not prepare the remote
		want      []string // substrings, in order
		next      string   // the final line, "" = none
	}{
		{name: "pass", rc: 0, machine: "local", want: []string{"heavyq: PASS go test ./... · tree 0123456789ab · 2s"}},
		{name: "fail", rc: 1, machine: "local", want: []string{"heavyq: FAIL go test ./... · exit 1 · tree 0123456789ab"},
			next: "next: fix the failure above, then re-run this command"},
		{name: "fail in a gate", rc: 1, machine: "local", gate: "ready", want: []string{"FAIL"},
			next: "next: fix the failure above, then run: heavyq gate run ready"},
		{name: "not found", rc: 127, machine: "local", want: []string{"exit 127"},
			next: "next: command not found; check its name and PATH, then re-run"},
		{name: "not executable", rc: 126, machine: "local", want: []string{"exit 126"},
			next: "next: command is not executable; check its permissions, then re-run"},
		{name: "connection lost", rc: ExitConnLost, machine: "box", want: []string{"FAIL"},
			next: "next: the connection to box was lost; retry, or add --remote local"},
		{name: "255 locally is just a failure", rc: ExitConnLost, machine: "local", want: []string{"FAIL"},
			next: "next: fix the failure above, then re-run this command"},
		{name: "remote setup", rc: ExitRemoteSetup, machine: "box", setupFail: true, want: []string{"heavyq: NOT RUN", "remote setup on box failed"},
			next: "next: " + nextRemoteDown},
		{name: "refused on a remote that loses exit codes", rc: ExitRemoteSetup, machine: "box", setupFail: true, distrust: "exit codes not trustworthy on box: x",
			want: []string{"heavyq: NOT RUN", "exit codes not trustworthy on box"}, next: "next: " + nextUntrusted},
		{name: "ran on a remote that loses exit codes", rc: 0, machine: "box", distrust: "exit codes not trustworthy on box: x",
			want: []string{"PASS?", "proves nothing"}, next: "next: " + nextUntrusted},
		{name: "a command's own 77 is its failure", rc: ExitRemoteSetup, machine: "box", want: []string{"heavyq: FAIL", "exit 77"},
			next: "next: fix the failure above, then re-run this command"},
		{name: "cancelled", rc: ExitCancelled, machine: "local", cancelled: true, want: []string{"heavyq: CANCELLED", "an operator cancelled it"},
			next: "next: cancelled by operator; ask before re-running"},
		{name: "interrupted", rc: 143, machine: "local", interrupt: true, want: []string{"heavyq: INTERRUPTED", "stopped by signal 15"},
			next: "next: run it again if you still need the result"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r, buf := digestRun(t, agentmode.Agent, "go", "test", "./...")
			r.opts.Gate, r.cancelled, r.interrupted, r.distrust, r.remoteSetupFailed = c.gate, c.cancelled, c.interrupt, c.distrust, c.setupFail
			r.digestRan(c.rc, c.machine, tree, 2*time.Second, 0)
			got := buf.String()
			pos := 0
			for _, w := range c.want {
				i := strings.Index(got[pos:], w)
				if i < 0 {
					t.Fatalf("missing %q (in order) in:\n%s", w, got)
				}
				pos += i
			}
			lines := strings.Split(strings.TrimSpace(got), "\n")
			if last := lines[len(lines)-1]; c.next != "" && last != c.next {
				t.Fatalf("last line %q, want %q", last, c.next)
			}
			if n := nextLines(got); (c.next != "") != (n == 1) || n > 1 {
				t.Fatalf("%d next: lines:\n%s", n, got)
			}
		})
	}
}

func TestDigestNotRunClassification(t *testing.T) {
	for _, c := range []struct {
		name      string
		rc        int
		why       string
		cancelled bool
		want      string // header words
		next      string
	}{
		{"max wait", ExitMaxWait, "not our turn after 5m0s (--max-wait)", false, "NOT RUN",
			"next: retry later; don't bypass heavyq"},
		{"remote down", ExitRemoteDown, "box unreachable for 10m0s; giving up", false, "NOT RUN", "next: " + nextRemoteDown},
		{"cancelled", ExitCancelled, "", true, "CANCELLED", "next: cancelled by operator; ask before re-running"},
		{"signal", 130, "", false, "INTERRUPTED", "next: run it again if you still need the result"},
		{"code changed", ExitUsage, "code changed while waiting (HEAD moved); giving up", false, "NOT RUN",
			"next: run it again; if that repeats, `heavyq why` explains the queue"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r, buf := digestRun(t, agentmode.Agent, "make", "test")
			r.out.gaveUp, r.cancelled = c.why, c.cancelled
			r.digestNotRun(c.rc)
			lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
			if !strings.HasPrefix(lines[0], "heavyq: "+c.want+" make test · exit ") || lines[len(lines)-1] != c.next {
				t.Fatalf("got:\n%s", buf)
			}
			if c.why != "" && !strings.Contains(buf.String(), "  "+c.why+"\n") {
				t.Fatalf("the reason is missing:\n%s", buf)
			}
			if nextLines(buf.String()) != 1 {
				t.Fatalf("want exactly one next: line:\n%s", buf)
			}
		})
	}
}

func TestDigestIsOffForHumansUnlessAsked(t *testing.T) {
	r, buf := digestRun(t, agentmode.Human, "true")
	r.digestRan(1, "local", gitutil.Checkout{}, time.Second, 0)
	r.digestNotRun(ExitMaxWait)
	if buf.Len() != 0 || r.out.capture != nil {
		t.Fatalf("human mode printed %q (capture=%v): it must not touch the streams at all", buf, r.out.capture)
	}
	if r.out.stdout == nil || r.out.stderr == nil {
		t.Fatal("no streams")
	}
	asked := newRunOutput(agentmode.Human, true, io.Discard, time.Now(), "x")
	if !asked.digest || asked.capture == nil || asked.mode.Terse() {
		t.Fatal("--digest turns the digest on and leaves the chatter human")
	}
}

// The human path must keep handing the command the process's own streams:
// they stay a terminal (colours, line buffering) and nothing copies the bytes.
func TestHumanStreamsAreTheRealOnesWithoutADigest(t *testing.T) {
	o := newRunOutput(agentmode.Human, false, io.Discard, time.Now(), "x")
	if o.capture != nil || o.stdout != io.Writer(os.Stdout) || o.stderr != io.Writer(os.Stderr) {
		t.Fatalf("human mode wraps the command's streams: %+v", o)
	}
	if a := newRunOutput(agentmode.Agent, false, io.Discard, time.Now(), "x"); a.capture == nil || a.stdout == io.Writer(os.Stdout) {
		t.Fatal("agent mode must capture")
	}
}

// nextLines counts the lines that start with "next:".
func nextLines(s string) int {
	n := 0
	for _, l := range strings.Split(s, "\n") {
		if strings.HasPrefix(l, "next:") {
			n++
		}
	}
	return n
}

func TestDigestFailureLinesAndLog(t *testing.T) {
	r, buf := digestRun(t, agentmode.Agent, "go", "test")
	// The command's output goes through the capture to the real stdout; here
	// the real one is discarded.
	tee := r.out.capture.Wrap(io.Discard)
	_, _ = tee.Write([]byte("ok  \tpkg/a\t0.1s\n--- FAIL: TestB (0.00s)\n    b_test.go:9: want 1 got 2\nFAIL\n"))
	r.digestRan(1, "local", gitutil.Checkout{}, time.Second, 0)
	got := buf.String()
	for _, want := range []string{"  --- FAIL: TestB (0.00s)\n", "  b_test.go:9: want 1 got 2\n", "  FAIL\n", "\nlog: ", "(full output, 0 KiB)\n"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "ok  ") {
		t.Fatalf("a passing line was quoted:\n%s", got)
	}
}

func TestPassingRunLeavesNoLog(t *testing.T) {
	r, buf := digestRun(t, agentmode.Agent, "true")
	_, _ = r.out.capture.Wrap(io.Discard).Write([]byte("fine\n"))
	r.digestRan(0, "local", gitutil.Checkout{}, time.Second, 0)
	if strings.Contains(buf.String(), "log:") {
		t.Fatalf("a pass names a log:\n%s", buf)
	}
	if entries, _ := filepath.Glob(filepath.Join(logDir(), "*.log")); len(entries) != 0 {
		t.Fatalf("a passing run left %v", entries)
	}
}
