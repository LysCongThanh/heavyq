package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LysCongThanh/heavyq/internal/history"
)

// These tests cover `--with REPO[@REF]`: sibling repositories pinned at
// enqueue, the local eligibility rule, the run identity and the cache key.
// Every repository is a real temp git repo: app (the main one) and its
// siblings next to it under the rig's root.

// commitIn writes name in dir and commits it; it returns the new HEAD.
func commitIn(t *testing.T, h *rig, dir, name, content string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, h, dir, "add", ".")
	gitIn(t, h, dir, "commit", "-q", "-m", "edit "+name)
	return gitOutput(t, h, dir, "rev-parse", "HEAD")
}

// withRig is a rig with the main repo "app" (cwd) and the sibling "lib".
func withRig(t *testing.T, cfg string) (h *rig, app, lib string) {
	t.Helper()
	h = newRig(t, cfg)
	app = gitRepo(t, h)
	lib = gitRepoAt(t, h, "lib")
	return h, app, lib
}

func TestE2EWithRefusesBadSpecsBeforeRunningAnything(t *testing.T) {
	h, _, lib := withRig(t, baseConfig)
	if err := os.MkdirAll(filepath.Join(h.root, "plain"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ name, spec, want string }{
		{"missing directory", "nowhere", "not an existing directory"},
		{"not a git repository", "plain", "not a git repository"},
		{"unknown ref", "lib@no-such-ref", "no commit"},
		{"option-looking ref", "lib@-x", "invalid ref"},
		{"dot name", ".", "directory name"},
		{"dot-dot name", "..", "directory name"},
		{"unsafe name", "li$b", "directory name"},
		{"empty ref", "lib@", "expected REPO[@REF]"},
		{"the main repo", "app", "main repository"},
		{"the main repo by path", "../app", "main repository"},
	} {
		t.Run(c.name, func(t *testing.T) {
			rc, out := h.run("3", "--with", c.spec, "--", "sh", "-c", "touch ../ran")
			if rc != 2 || !strings.Contains(out, c.want) {
				t.Fatalf("rc=%d, want 2 with %q\n%s", rc, c.want, out)
			}
			if _, err := os.Stat(filepath.Join(h.root, "ran")); err == nil {
				t.Fatal("the command ran")
			}
		})
	}
	t.Run("declared twice", func(t *testing.T) {
		rc, out := h.run("3", "--with", "lib", "--with", "lib@HEAD", "--", "true")
		if rc != 2 || !strings.Contains(out, "declared twice") {
			t.Fatalf("rc=%d\n%s", rc, out)
		}
	})
	t.Run("inside a repository, not its root", func(t *testing.T) {
		if err := os.MkdirAll(filepath.Join(lib, "sub"), 0o755); err != nil {
			t.Fatal(err)
		}
		rc, out := h.run("3", "--with", filepath.Join(lib, "sub"), "--", "true")
		if rc != 2 || !strings.Contains(out, "not its root") {
			t.Fatalf("rc=%d\n%s", rc, out)
		}
	})
}

func TestE2EWithLocalRunNeedsPinnedCleanSiblings(t *testing.T) {
	h, _, lib := withRig(t, baseConfig)
	pinned := gitOutput(t, h, lib, "rev-parse", "HEAD")
	read := []string{"sh", "-c", "cat ../lib/a.txt"}

	// A clean sibling at its pin runs in place: ../lib is the sibling.
	rc, out := h.run(append([]string{"3", "--remote", "local", "--with", "lib", "--"}, read...)...)
	if rc != 0 || !strings.Contains(out, "a\n") {
		t.Fatalf("clean pinned sibling: rc=%d\n%s", rc, out)
	}
	// A dirty sibling cannot be what the pin says: refused, naming it, nothing run.
	if err := os.WriteFile(filepath.Join(lib, "a.txt"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rc, out = h.run(append([]string{"3", "--remote", "local", "--with", "lib", "--"}, read...)...)
	if rc != 2 || !strings.Contains(out, "lib: uncommitted changes") || strings.Contains(out, "edited") {
		t.Fatalf("dirty sibling: rc=%d\n%s", rc, out)
	}
	gitIn(t, h, lib, "checkout", "-q", "--", "a.txt")
	// A sibling that moved past its pin is refused as well.
	commitIn(t, h, lib, "a.txt", "newer\n")
	rc, out = h.run(append([]string{"3", "--remote", "local", "--with", "lib@" + pinned, "--"}, read...)...)
	if rc != 2 || !strings.Contains(out, "lib: HEAD differs from pinned "+pinned[:12]) {
		t.Fatalf("moved sibling: rc=%d\n%s", rc, out)
	}
	// Without --remote and no remote configured, local is the only place: same refusal.
	rc, out = h.run(append([]string{"3", "--with", "lib@" + pinned, "--"}, read...)...)
	if rc != 2 || !strings.Contains(out, "cannot run locally") {
		t.Fatalf("implicit local: rc=%d\n%s", rc, out)
	}
	if n := len(h.history()); n != 1 {
		t.Fatalf("%d runs in the ledger, want only the clean one", n)
	}
}

func TestE2EWithSiblingIsPartOfTheRunIdentity(t *testing.T) {
	h, _, lib := withRig(t, baseConfig)
	run := func(extra ...string) history.Record {
		t.Helper()
		args := append(append([]string{"3", "--remote", "local"}, extra...), "--", "true")
		if rc, out := h.run(args...); rc != 0 {
			t.Fatalf("rc=%d\n%s", rc, out)
		}
		return h.lastRecord()
	}
	without := run()
	first := run("--with", "lib")
	sha1 := gitOutput(t, h, lib, "rev-parse", "HEAD")
	if first.With != "lib@"+sha1[:12] {
		t.Fatalf("with = %q, want lib@%s", first.With, sha1[:12])
	}
	if first.RunID == "" || first.RunID == without.RunID {
		t.Fatalf("a run with a sibling shares the identity of one without: %q vs %q", first.RunID, without.RunID)
	}
	if again := run("--with", "lib"); again.RunID != first.RunID {
		t.Fatal("the same sibling commit gives another run_id")
	}
	commitIn(t, h, lib, "a.txt", "newer\n")
	moved := run("--with", "lib")
	if moved.RunID == first.RunID || moved.With == first.With {
		t.Fatalf("a new sibling commit keeps the run_id: %+v", moved)
	}
}
