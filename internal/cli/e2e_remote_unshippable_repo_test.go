package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// unshippableRepo makes "app" under h.root with one committed file through init (a SHA-256 repository) or
// through a shallow clone, and cds into it.
func unshippableRepo(t *testing.T, h *rig, kind string) {
	t.Helper()
	repo := filepath.Join(h.root, "app")
	switch kind {
	case "sha256":
		if err := os.MkdirAll(repo, 0o755); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("git", "-C", repo, "init", "-q", "--object-format=sha256", "-b", "main")
		cmd.Env = h.env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git cannot make a SHA-256 repository: %v %s", err, out)
		}
	case "shallow":
		src := filepath.Join(h.root, "upstream")
		if err := os.MkdirAll(src, 0o755); err != nil {
			t.Fatal(err)
		}
		gitIn(t, h, src, "init", "-q", "-b", "main")
		for _, c := range []string{"one", "two", "three"} {
			if err := os.WriteFile(filepath.Join(src, "marker.txt"), []byte(c), 0o644); err != nil {
				t.Fatal(err)
			}
			gitIn(t, h, src, "add", ".")
			gitIn(t, h, src, "-c", "commit.gpgsign=false", "commit", "-q", "-m", c)
		}
		gitIn(t, h, h.root, "clone", "-q", "--depth", "1", "file://"+src, repo)
		h.cwd = repo
		return
	}
	if err := os.WriteFile(filepath.Join(repo, "marker.txt"), []byte("from-commit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, h, repo, "add", ".")
	gitIn(t, h, repo, "-c", "commit.gpgsign=false", "commit", "-q", "-m", "init")
	h.cwd = repo
}

// A clean SHA-256 repository or shallow clone can never ship to a remote (the remote side is SHA-1, the bare
// repo refuses a shallow push), yet default placement used to pick a remote by score and the run gave up
// with 77. Such a repository now stays on this machine; a remote-only target is refused up front (exit 2).
func TestE2EUnshippableRepoStaysLocal(t *testing.T) {
	for _, kind := range []string{"sha256", "shallow"} {
		t.Run(kind, func(t *testing.T) {
			h := newRig(t, "")
			h.setConfig(remoteConfig(h, ""))
			unshippableRepo(t, h, kind)
			hold := h.start("5", "--remote", "local", "--", "sh", "-c", h.gateFile("go"))
			h.running(1)
			// local is busy, the remote is free: placement by score would take the remote
			rc, out := h.run("3", "--max-wait", "500ms", "--", "cat", "marker.txt")
			if rc != ExitMaxWait {
				t.Fatalf("a %s repository must wait for this machine: rc=%d\n%s", kind, rc, out)
			}
			h.touch("go")
			hold.wait(t, 20*time.Second)
			rc, out = h.run("3", "--remote", "box", "--", "true")
			if rc != ExitUsage || !strings.Contains(out, "remote runs need") {
				t.Fatalf("--remote box in a %s repository: rc=%d, want %d with the reason\n%s", kind, rc, ExitUsage, out)
			}
			if rc, out := h.run("3", "--remote", "auto", "--", "cat", "marker.txt"); rc != 0 || !strings.Contains(out, "local/") {
				t.Fatalf("--remote auto in a %s repository runs here: rc=%d\n%s", kind, rc, out)
			}
		})
	}
}
