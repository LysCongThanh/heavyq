package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/agentmode"
	"github.com/LysCongThanh/heavyq/internal/gitutil"
	"github.com/LysCongThanh/heavyq/internal/queue"
)

// The narrator is driven with a fake clock: no test here sleeps.

var t0 = time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)

// simulate polls a waiting ticket every poll for total, as the wait loop does.
// The position changes as tickets ahead finish: from pos0 down to 1.
func simulate(n *waitNarrator, total, poll time.Duration, pos0 int) {
	steps := int(total / poll)
	for i := 0; i < steps; i++ {
		pos := max(1, pos0-i*pos0/steps) // moves up the queue as time passes
		n.observe(t0.Add(time.Duration(i)*poll), waitView{pos: pos, total: pos0, target: "local", prio: 3, lane: "wt-a",
			ahead: []*queue.Ticket{{Lane: "wt-b", Prio: 3}, {Lane: "wt-c", Prio: 3}, {Lane: "wt-d", Prio: 4}, {Lane: "wt-e", Prio: 5}}})
	}
}

func TestAgentTwoMinuteWaitSaysQueuedOnce(t *testing.T) {
	var buf bytes.Buffer
	n := waitNarrator{w: &buf, terse: true, t0: t0}
	simulate(&n, 2*time.Minute, 50*time.Millisecond, 6)
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("a 2-minute wait said %d lines, want 1 (the admission line and the digest make 3):\n%s", len(lines), buf.String())
	}
	if !strings.HasPrefix(lines[0], "heavyq: queued #") || !strings.Contains(lines[0], "lane wt-a") ||
		!strings.Contains(lines[0], "ahead: wt-b(p3) wt-c(p3) wt-d(p4) +1") {
		t.Fatalf("queued line = %q", lines[0])
	}
	if strings.Contains(buf.String(), "\x1b") {
		t.Fatal("ANSI in agent output")
	}
}

func TestAgentSaysNothingWhenAdmittedQuickly(t *testing.T) {
	var buf bytes.Buffer
	n := waitNarrator{w: &buf, terse: true, t0: t0}
	simulate(&n, queuedAfter-time.Millisecond, 50*time.Millisecond, 3)
	if buf.Len() != 0 {
		t.Fatalf("a wait under %s must be silent, got %q", queuedAfter, buf.String())
	}
}

func TestAgentQueuedLineWaitsForTwoSeconds(t *testing.T) {
	var buf bytes.Buffer
	n := waitNarrator{w: &buf, terse: true, t0: t0}
	v := waitView{pos: 2, total: 2, target: "local", prio: 3, lane: "x"}
	n.observe(t0.Add(queuedAfter-time.Nanosecond), v)
	if buf.Len() != 0 {
		t.Fatalf("spoke too early: %q", buf.String())
	}
	n.observe(t0.Add(queuedAfter), v)
	if !strings.HasPrefix(buf.String(), "heavyq: queued #2 of 2 on local (p3, lane x)\n") {
		t.Fatalf("got %q", buf.String())
	}
}

func TestAgentHeartbeatsAreFiveMinutesApart(t *testing.T) {
	var buf bytes.Buffer
	n := waitNarrator{w: &buf, terse: true, t0: t0}
	simulate(&n, 21*time.Minute, time.Second, 4)
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	// queued at 2 s, then beats at 5, 10, 15 and 20 minutes.
	if len(lines) != 5 {
		t.Fatalf("21 minutes said %d lines, want 5:\n%s", len(lines), buf.String())
	}
	for _, l := range lines[1:] {
		if !strings.HasPrefix(l, "heavyq: still queued #") {
			t.Fatalf("heartbeat = %q", l)
		}
	}
	if !strings.Contains(lines[1], "after 5m0s") || !strings.Contains(lines[4], "after 20m0s") {
		t.Fatalf("heartbeats at the wrong times:\n%s", buf.String())
	}
}

func TestHumanNarratorKeepsItsOldRhythm(t *testing.T) {
	var buf bytes.Buffer
	n := waitNarrator{w: &buf, t0: t0}
	v := waitView{pos: 2, total: 2, target: "local", prio: 3, lane: "x", ahead: []*queue.Ticket{{Lane: "y", Prio: 3}}}
	n.observe(t0, v)
	n.observe(t0.Add(10*time.Second), v) // same position, under 30 s: silent
	v.pos = 1
	n.observe(t0.Add(11*time.Second), v) // moved up: says so
	n.observe(t0.Add(42*time.Second), v) // 31 s after the last line
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	want := []string{
		"heavyq: waiting 2/2 (prio 3, lane x, local) for 0s; ahead: y(p3)",
		"heavyq: waiting 1/2 (prio 3, lane x, local) for 11s; ahead: y(p3)",
		"heavyq: waiting 1/2 (prio 3, lane x, local) for 42s; ahead: y(p3)",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got:\n%s\nwant:\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
}

// The issue's acceptance test on a fake clock: a run that waits two minutes
// and then passes says three lines in all (queued, running, digest).
func TestAgentRunAfterATwoMinuteSimulatedWaitSaysThreeLines(t *testing.T) {
	r, buf := digestRun(t, agentmode.Agent, "go", "test", "./...")
	r.t0 = t0
	r.out.nar = waitNarrator{w: buf, terse: true, t0: t0}
	simulate(&r.out.nar, 2*time.Minute, 50*time.Millisecond, 5)
	co := gitutil.Checkout{Tree: "0123456789abcdef0123456789abcdef01234567"}
	r.announceAdmitted(&claim{machine: "local", slot: "heavy-1"}, 2*time.Minute, co)
	r.digestRan(0, "local", co, 41*time.Second, 2*time.Minute)

	lines := heavyqLines(buf.String())
	if len(lines) != 3 {
		t.Fatalf("%d heavyq lines, want 3:\n%s", len(lines), buf)
	}
	if !strings.HasPrefix(lines[0], "heavyq: queued #") ||
		lines[1] != "heavyq: running go test ./... (p3, lane wt-a, local/heavy-1, tree 0123456789ab, waited 2m0s)" ||
		lines[2] != "heavyq: PASS go test ./... · tree 0123456789ab · 41s (waited 2m0s)" {
		t.Fatalf("lines:\n%s", strings.Join(lines, "\n"))
	}
}
