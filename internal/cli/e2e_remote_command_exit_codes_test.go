package cli

import (
	"strings"
	"testing"

	"github.com/LysCongThanh/heavyq/internal/history"
)

// 77 and 255 are heavyq's own codes for "remote setup failed" and "connection lost", but a command can exit
// with them too (77 is the automake/meson SKIP code, 255 is exit(-1)). Their status used to be decided by the
// number alone: a command's own 77 was recorded as gave_up and digested as "NOT RUN: remote setup failed",
// locally as well, and a remote command's own 255 as a lost connection.
func TestE2ECommandsOwn77And255AreTheCommandsFailure(t *testing.T) {
	h := newAgentRig(t)
	h.setConfig(remoteConfig(h, ""))
	gitRepo(t, h)
	for _, tc := range []struct {
		machine, exit string
		want          int
	}{
		{"local", "77", 77},
		{"box", "77", 77},
		{"box", "255", 96}, // ssh's 255 must stay unambiguous, so the job reports the command's 255 as 96
	} {
		rc, out := h.run("3", "--remote", tc.machine, "--", "sh", "-c", "exit "+tc.exit)
		if rc != tc.want {
			t.Fatalf("%s exit %s: rc=%d, want %d\n%s", tc.machine, tc.exit, rc, tc.want, out)
		}
		for _, bad := range []string{"NOT RUN", "did not run", "remote setup", "connection", "unreachable"} {
			if strings.Contains(out, bad) {
				t.Fatalf("%s exit %s: the command ran and failed, but the output says %q:\n%s", tc.machine, tc.exit, bad, out)
			}
		}
		if !strings.Contains(out, "FAIL") {
			t.Fatalf("%s exit %s: no FAIL digest:\n%s", tc.machine, tc.exit, out)
		}
		if r := h.lastRecord(); r.Result == history.OutcomeGaveUp || r.Machine != tc.machine {
			t.Fatalf("%s exit %s: recorded as %+v", tc.machine, tc.exit, r)
		}
	}
}
