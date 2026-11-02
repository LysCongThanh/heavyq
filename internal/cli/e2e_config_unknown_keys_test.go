package cli

import (
	"strings"
	"testing"
)

// #128: a user config written for a newer heavyq (a future section, a future
// key in a known section) never stops this one: each command warns once on
// stderr and runs; `heavyq doctor` lists the keys.
func TestE2EUserConfigWithFutureKeysStillRuns(t *testing.T) {
	h := newRig(t, baseConfig+"\n[queue_v9]\nlanes = 12\n\n[hook]\nfuture_knob = true\n")

	if rc, out, errOut := h.runSplit("version"); rc != 0 || out == "" {
		t.Fatalf("version rc=%d\nstdout: %s\nstderr: %s", rc, out, errOut)
	}
	rc, out, errOut := h.runSplit("3", "--", "true")
	if rc != 0 || strings.Count(errOut, "warning") != 1 || !strings.Contains(errOut, `queue_v9 (line `) || !strings.Contains(errOut, "hook.future_knob") {
		t.Fatalf("heavyq 3 -- true rc=%d\nstdout: %s\nstderr: %s", rc, out, errOut)
	}

	rc, out, _ = h.runSplit("doctor")
	for _, want := range []string{"2 key(s) this heavyq does not know, ignored", "queue_v9", "hook.future_knob"} {
		if rc != 0 || !strings.Contains(out, want) {
			t.Fatalf("doctor rc=%d lacks %q:\n%s", rc, want, out)
		}
	}
}

// A typo in a known value is still refused, and doctor runs even then: it is
// the command that says what is wrong with the config.
func TestE2EDoctorReportsAConfigThatDoesNotLoad(t *testing.T) {
	h := newRig(t, "[queue]\npoll = \"soon\"\n")
	if rc, _, errOut := h.runSplit("3", "--", "true"); rc != ExitUsage || !strings.Contains(errOut, "poll") {
		t.Fatalf("run with a bad value rc=%d\n%s", rc, errOut)
	}
	rc, out, _ := h.runSplit("doctor")
	if rc != 1 || !strings.Contains(out, "config: does not load") {
		t.Fatalf("doctor rc=%d\n%s", rc, out)
	}
}
