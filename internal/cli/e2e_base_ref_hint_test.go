package cli

import (
	"strings"
	"testing"
)

// When a command on a remote fails because git there has no origin/<branch>, the failure digest says how to
// get it there instead of leaving the missing ref to be puzzled out (#147).

func TestE2ERemoteFailureOnAMissingOriginRefSuggestsBaseRef(t *testing.T) {
	h := newRig(t, "")
	h.setConfig(remoteConfig(h, ""))
	repo := gitRepo(t, h)
	addOrigin(t, h, repo, "main")
	gitIn(t, h, repo, "push", "-q", "origin", "main:develop") // origin/develop exists here, not on the remote

	diff := func(where, ref string) string {
		t.Helper()
		rc, out := h.run("3", "--digest", "--remote", where, "--", "git", "diff", "--stat", ref)
		if rc == 0 {
			t.Fatalf("git diff %s on %s succeeded:\n%s", ref, where, out)
		}
		return out
	}
	if out := diff("box", "origin/develop"); !strings.Contains(out, "next: the remote has no origin/develop; re-run with --base-ref origin/develop\n") {
		t.Fatalf("the digest must suggest --base-ref origin/develop:\n%s", out)
	}
	// Nothing to suggest when this repository has no such ref either, or when the run was here.
	for _, c := range []struct{ where, ref string }{{"box", "origin/nowhere"}, {"local", "origin/nowhere"}} {
		if out := diff(c.where, c.ref); strings.Contains(out, "--base-ref") || !strings.Contains(out, "next: fix the failure above, then re-run this command\n") {
			t.Fatalf("%s %s: the digest must keep its usual next line:\n%s", c.where, c.ref, out)
		}
	}
	// The hint works, too: with the ref shipped the same command passes.
	if rc, out := h.run("3", "--remote", "box", "--base-ref", "origin/develop", "--", "git", "diff", "--stat", "origin/develop"); rc != 0 {
		t.Fatalf("with --base-ref origin/develop: rc=%d\n%s", rc, out)
	}
}
