package cli

import (
	"strings"
	"testing"
	"time"
)

// The story is the same sentence in `status --json`, `status` and `why`.
// "Outside the queue" sentences depend on what else runs on the machine
// running the tests, so these tests look at the first sentence and at
// substrings, never at the whole story.

func TestWhyAndStoryOnAnIdleQueue(t *testing.T) {
	h := newRig(t, baseConfig)
	s := h.statusJSON()
	if len(s.Story) == 0 || s.Story[0].Text != "All quiet. Nothing runs, nothing waits." || len(s.Story[0].Parts) == 0 {
		t.Fatalf("snapshot.story = %+v", s.Story)
	}
	rc, out := h.run("why")
	if rc != 0 || !strings.HasPrefix(out, "All quiet. Nothing runs, nothing waits.\n") {
		t.Fatalf("why rc=%d:\n%s", rc, out)
	}
	rc, out = h.run("status")
	if rc != 0 || !strings.HasPrefix(out, "STORY\n  All quiet. Nothing runs, nothing waits.\n") || !strings.Contains(out, "\nRUNNING\n") {
		t.Fatalf("status rc=%d: the story must open the text output:\n%s", rc, out)
	}
}

func TestWhyExplainsWhoWaitsAndWhy(t *testing.T) {
	h := newRig(t, baseConfig) // 1 heavy slot, gate off
	hold := h.start("4", "--lane", "holder", "--", "sh", "-c", h.gateFile("go"))
	h.running(1)
	h.start("3", "--lane", "first", "--", "true")
	h.waiting(1)
	h.start("3", "--lane", "second", "--", "env", "API_TOKEN=abc123", "true")
	h.waiting(2)

	const story = "2 agents wait because every heavy slot on local is taken."
	if s := h.statusJSON(); len(s.Story) == 0 || s.Story[0].Text != story {
		t.Fatalf("snapshot.story = %+v", s.Story)
	}
	rc, out := h.run("why")
	if rc != 0 || !strings.HasPrefix(out, story+"\n") {
		t.Fatalf("why rc=%d:\n%s", rc, out)
	}
	if rc, out := h.run("status"); rc != 0 || !strings.HasPrefix(out, "STORY\n  "+story+"\n") {
		t.Fatalf("status rc=%d:\n%s", rc, out)
	}

	rc, out = h.run("why", "second")
	for _, want := range []string{"lane second: 1 waiting.", "waiting #2", "local: behind first (p3)",
		"API_TOKEN=***", "no run of this lane in the history yet."} {
		if rc != 0 || !strings.Contains(out, want) {
			t.Fatalf("why second missing %q (rc=%d):\n%s", want, rc, out)
		}
	}
	if strings.Contains(out, "abc123") {
		t.Fatalf("secret leaked:\n%s", out)
	}
	rc, out = h.run("why", "holder")
	for _, want := range []string{"lane holder: 1 running.", "running on local/heavy-1", "(p4, pid "} {
		if rc != 0 || !strings.Contains(out, want) {
			t.Fatalf("why holder missing %q (rc=%d):\n%s", want, rc, out)
		}
	}
	h.touch("go")
	hold.wait(t, 30*time.Second)
}

func TestWhyLaneWithHistoryAndUnknownLane(t *testing.T) {
	h := newRig(t, baseConfig)
	if rc, out := h.run("3", "--lane", "done", "--", "true"); rc != 0 {
		t.Fatalf("run rc=%d\n%s", rc, out)
	}
	if rc, out := h.run("3", "--lane", "bad", "--", "sh", "-c", "exit 3"); rc != 3 {
		t.Fatalf("run rc=%d\n%s", rc, out)
	}
	rc, out := h.run("why", "done")
	if rc != 0 || !strings.Contains(out, "lane done: not running and not waiting.") ||
		!strings.Contains(out, "last run passed in ") || !strings.Contains(out, ": true") {
		t.Fatalf("why done rc=%d:\n%s", rc, out)
	}
	rc, out = h.run("why", "bad")
	if rc != 0 || !strings.Contains(out, "last run failed with exit 3 ") {
		t.Fatalf("why bad rc=%d:\n%s", rc, out)
	}
	rc, out = h.run("why", "ghost")
	if rc != 0 || !strings.Contains(out, "lane ghost: not running and not waiting.") || !strings.Contains(out, "no run of this lane") {
		t.Fatalf("why ghost rc=%d:\n%s", rc, out)
	}
	// The story of an idle queue now carries the history window sentence.
	rc, out = h.run("why")
	if rc != 0 || !strings.Contains(out, "In the last 1h: 2 runs,") {
		t.Fatalf("why with history rc=%d:\n%s", rc, out)
	}
}

func TestWhyRejectsBadArguments(t *testing.T) {
	h := newRig(t, baseConfig)
	for _, args := range [][]string{{"why", "a", "b"}, {"why", ""}, {"why", "--nope"}} {
		if rc, out := h.run(args...); rc != ExitUsage {
			t.Fatalf("heavyq %q: rc=%d, want 2:\n%s", args, rc, out)
		}
	}
}
