package cli

import (
	"strings"
	"testing"
)

// A long session (5 heavy runs of the lane in 30 minutes) with items and no open run span gets one extra line
// in the SessionStart brief, once per session; opening a span silences it.
func TestE2EBriefNudgesPreflightOnceInALongSession(t *testing.T) {
	h := newRig(t, baseConfig)
	app := preflightRepo(t, h, preflightGoAnchor, "")
	const nudge = "next: heavyq preflight"
	start := func(session, source string) string {
		t.Helper()
		rc, out, errOut := h.hookIn(app, nil, sessionStartJSON(app, session, source, ""), "hook", "claude-sessionstart")
		if rc != 0 {
			t.Fatalf("rc=%d %s", rc, errOut)
		}
		_, text := additionalContextOf(t, out)
		return text
	}

	if text := start("s0", "startup"); strings.Contains(text, nudge) {
		t.Fatalf("a quiet session is not nudged:\n%s", text)
	}
	for i := 0; i < 5; i++ {
		if rc, out, errOut := h.inDir(app, nil, "3", "--", "true"); rc != 0 {
			t.Fatalf("run %d: rc=%d %s%s", i, rc, out, errOut)
		}
	}
	first := start("s1", "compact")
	if strings.Count(first, nudge) != 1 || !strings.HasSuffix(first, nudge+"\n") {
		t.Fatalf("a long session is nudged, on its own last line:\n%s", first)
	}
	if again := start("s1", "compact"); strings.Contains(again, nudge) {
		t.Fatalf("the same session is nudged once:\n%s", again)
	}
	if other := start("s2", "startup"); !strings.Contains(other, nudge) {
		t.Fatalf("another session is a new state change:\n%s", other)
	}
	if rc, out, errOut := h.inDir(app, nil, "preflight"); rc != 0 {
		t.Fatalf("preflight: rc=%d %s%s", rc, out, errOut)
	}
	if text := start("s3", "compact"); strings.Contains(text, nudge) {
		t.Fatalf("an open run span silences the nudge:\n%s", text)
	}
	if rc, _, _ := h.inDir(app, nil, "run", "end"); rc != 0 {
		t.Fatal("run end")
	}
	if text := start("s4", "compact"); !strings.Contains(text, nudge) {
		t.Fatalf("once the span is ended the long session is nudged again:\n%s", text)
	}
}
