package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A remote run proves the committed tree. When the work tree holds edits that
// are not committed, the run says so, once, so a pass on the shipped files is
// never mistaken for a pass on the files being edited.

// leaveEditsBehind makes three uncommitted files in repo: a modified tracked
// file and two untracked files in a new directory. An ignored file sits
// beside them and does not count.
func leaveEditsBehind(t *testing.T, h *rig, repo string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("ignored.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, h, repo, "add", ".")
	gitIn(t, h, repo, "commit", "-q", "-m", "ignore logs")
	files := map[string]string{"marker.txt": "edited\n", "scratch/a.txt": "a\n", "scratch/b.txt": "b\n", "ignored.log": "noise\n"}
	for name, content := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(repo, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestE2ERemoteRunSaysUncommittedEditsAreNotShipped(t *testing.T) {
	h := newRig(t, "")
	h.setConfig(remoteConfig(h, ""))
	repo := gitRepo(t, h)
	leaveEditsBehind(t, h, repo)

	rc, out := h.run("3", "--remote", "box", "--lane", "api", "--", "sh", "-c", "cat marker.txt")
	const want = "heavyq: warning: 3 uncommitted files are not shipped: box runs the committed tree only"
	if rc != 0 || !strings.Contains(out, "from-commit") || strings.Contains(out, "edited") {
		t.Fatalf("the run itself changed: rc=%d\n%s", rc, out)
	}
	if n := strings.Count(out, "not shipped"); n != 1 || !strings.Contains(out, want) {
		t.Fatalf("want the line %q exactly once, got %d:\n%s", want, n, out)
	}
	// One uncommitted file reads in the singular.
	gitIn(t, h, repo, "clean", "-fdq")
	rc, out = h.run("3", "--remote", "box", "--lane", "api", "--", "true")
	if want := "heavyq: warning: 1 uncommitted file is not shipped: box runs"; rc != 0 || !strings.Contains(out, want) {
		t.Fatalf("rc=%d, want %q:\n%s", rc, want, out)
	}
}

// Agent mode keeps its budget: the note is part of the running line, never a line of its own.
func TestE2EAgentRemoteRunFoldsTheUnshippedNoteIntoTheRunningLine(t *testing.T) {
	h := newAgentRig(t)
	h.setConfig(remoteConfig(h, ""))
	repo := gitRepo(t, h)
	leaveEditsBehind(t, h, repo)
	tree := gitOutput(t, h, repo, "rev-parse", "HEAD^{tree}")[:12]

	rc, out := h.run("3", "--remote", "box", "--lane", "api", "--", "sh", "-c", "cat marker.txt")
	noANSI(t, out)
	lines := heavyqLines(out)
	const running = "heavyq: running sh -c cat marker.txt (p3, lane api, box/h1, tree "
	if rc != 0 || len(lines) != 2 {
		t.Fatalf("rc=%d, want the running line and the digest, nothing more:\n%s", rc, out)
	}
	if want := running + tree + ", 3 uncommitted files not shipped)"; lines[0] != want {
		t.Fatalf("running line\n got %q\nwant %q", lines[0], want)
	}
	if strings.Contains(lines[1], "not shipped") {
		t.Fatalf("the digest does not repeat the note: %q", lines[1])
	}
}

// Nothing is said when nothing is left behind, or when the run is not remote.
func TestE2ENoUnshippedNoteWithoutEditsOrWithoutARemote(t *testing.T) {
	h := newRig(t, "")
	h.setConfig(remoteConfig(h, ""))
	repo := gitRepo(t, h)

	if rc, out := h.run("3", "--remote", "box", "--", "true"); rc != 0 || strings.Contains(out, "not shipped") {
		t.Fatalf("clean tree, remote: rc=%d\n%s", rc, out)
	}
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("*.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, h, repo, "add", ".")
	gitIn(t, h, repo, "commit", "-q", "-m", "ignore logs")
	if err := os.WriteFile(filepath.Join(repo, "debug.log"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if rc, out := h.run("3", "--remote", "box", "--", "true"); rc != 0 || strings.Contains(out, "not shipped") {
		t.Fatalf("only an ignored file, remote: rc=%d\n%s", rc, out)
	}
	if err := os.WriteFile(filepath.Join(repo, "marker.txt"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if rc, out := h.run("3", "--remote", "local", "--", "true"); rc != 0 || strings.Contains(out, "not shipped") {
		t.Fatalf("an edit, but the run is local and sees it: rc=%d\n%s", rc, out)
	}
}
