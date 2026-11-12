package cli

import (
	"strings"
	"testing"
)

// An agent's run prints the running line and the digest, nothing else (docs/agent-mode.md), on a remote too.
// The exit canary's verdict printed a line of its own on top, although the digest says the same: a remote
// that loses exit codes cost every agent run there a line over its budget, refused or not.
func TestE2EAgentRunOnAnUntrustedRemoteStaysInItsLineBudget(t *testing.T) {
	h := newAgentRig(t)
	lyingRemote(t, h, "allow_untrusted_exit = true\n")
	gitRepo(t, h)
	rc, out := h.run("3", "--remote", "box", "--", "true")
	if lines := heavyqLines(out); len(lines) != 2 || !strings.Contains(lines[1], "PASS?") || !strings.Contains(out, untrustedBox) {
		t.Fatalf("an opted-in run: rc=%d, want the running line and a digest that says the result proves nothing:\n%s", rc, out)
	}
	rc, out = h.run("3", "--remote", "box", "--strict", "--", "true")
	if lines := heavyqLines(out); rc != ExitRemoteSetup || len(lines) != 2 || !strings.Contains(lines[1], "NOT RUN") ||
		!strings.Contains(out, untrustedBox) {
		t.Fatalf("a refused proof: rc=%d, want the running line and a NOT RUN digest that names the distrust:\n%s", rc, out)
	}
}
