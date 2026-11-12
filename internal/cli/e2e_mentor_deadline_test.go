package cli

import (
	"strings"
	"testing"
)

// The mentor hook has a time budget. When its decision comes too late, the
// note is not shown, so nothing of it may be recorded either: no inject event
// the agent never saw (it would bias the measured lift and auto-mute), and the
// record not taken as looked at, so the next call still says it.
func TestE2EMentorNoteMissedByTheDeadlineIsNeverRecorded(t *testing.T) {
	h, repo := mentorRig(t, "0")
	h.failRun(t)
	h.failRun(t)
	h.withEnv("HEAVYQ_TEST_MENTOR_BUDGET=100ms", func() {
		h.withEnv("HEAVYQ_TEST_MENTOR_DELAY=600ms", func() {
			if rc, out, errOut := h.runStdin(postHook(repo, wrapped, "PostToolUseFailure"), "hook", "claude-posttooluse"); rc != 0 || out != "" || errOut != "" {
				t.Fatalf("a late decision prints nothing: rc=%d out=%q err=%q", rc, out, errOut)
			}
		})
	})
	if log := h.read("mentor/interventions.jsonl"); strings.TrimSpace(log) != "" {
		t.Fatalf("an event nobody saw was recorded:\n%s", log)
	}
	_, out, _ := h.runStdin(postHook(repo, wrapped, "PostToolUseFailure"), "hook", "claude-posttooluse")
	if _, text := h.contextOf(t, out); !strings.Contains(text, "exited 3 twice in a row") {
		t.Fatalf("the note comes on the next call: %q", out)
	}
}
