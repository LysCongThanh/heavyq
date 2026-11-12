package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

// A remote job whose base ref could not be pushed runs without a base, and the
// ledger says so. Its result must not be stored under the cache key, which
// holds the local base: a later --cache run with that base (gate run and item
// done --run use --cache) would replay it and record that base for a run that
// never had it, a false proof.
func TestE2ECacheNeverStoresARemoteResultThatMissedItsBase(t *testing.T) {
	h := newRig(t, "")
	h.setConfig(remoteConfig(h, ""))
	repo := gitRepo(t, h)
	x := gitOutput(t, h, repo, "rev-parse", "HEAD")
	gitIn(t, h, repo, "update-ref", "refs/remotes/origin/main", x)
	if rc, out := h.run("5", "--remote", "box", "--base-ref", "none", "--", "true"); rc != 0 {
		t.Fatalf("first ship: rc=%d\n%s", rc, out)
	}
	// A ref below the base's name makes the remote refuse the base push (a
	// directory/file conflict), and only that push.
	bare := filepath.Join(h.root, "remote-work", "git", "app.git")
	gitIn(t, h, bare, "update-ref", "refs/heavyq/base-"+x[:12]+"/block", x)

	argv := []string{"sh", "-c", "git rev-parse -q --verify origin/main || echo NO''BASE"} // NOBASE is the output, not the echoed command
	rc, out := h.run(append([]string{"5", "--cache", "--remote", "box", "--base-ref", "origin/main", "--"}, argv...)...)
	if rc != 0 || !strings.Contains(out, "base ref not pushed") || !strings.Contains(out, "NOBASE") {
		t.Fatalf("the job must run without the base it could not get: rc=%d\n%s", rc, out)
	}
	noBase := h.lastRecord()
	rc, out = h.run(append([]string{"5", "--cache", "--remote", "local", "--base-ref", "origin/main", "--"}, argv...)...)
	if rc != 0 || strings.Contains(out, "NOBASE") || !strings.Contains(out, x) {
		t.Fatalf("a run with base %.12s replayed the result of a run without it: rc=%d\n%s", x, rc, out)
	}
	if withBase := h.lastRecord(); withBase.Cache == "hit" || withBase.RunID == noBase.RunID {
		t.Fatalf("the runs with and without the base must differ: %+v / %+v", noBase, withBase)
	}
}
