package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `--with` on the fake remote "box" (reached through sh -c, see remoteConfig):
// the sibling's pinned commit is pushed and materialised next to the main
// worktree, so ../lib resolves from the command, whatever the local work trees hold.

func TestE2EWithRemoteMaterialisesPinnedSiblingNextToTheWorktree(t *testing.T) {
	h, _, lib := withRig(t, "")
	h.setConfig(remoteConfig(h, ""))
	pinned := gitOutput(t, h, lib, "rev-parse", "HEAD")
	commitIn(t, h, lib, "a.txt", "newer\n")
	// uncommitted edit: never shipped
	if err := os.WriteFile(filepath.Join(lib, "a.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	probe := "ls ..; cat ../lib/a.txt; git -C ../lib rev-parse HEAD"

	// HEAD of lib at enqueue time is the committed "newer", the edit stays home.
	rc, out := h.run("3", "--remote", "box", "--with", "lib", "--", "sh", "-c", probe)
	head := gitOutput(t, h, lib, "rev-parse", "HEAD")
	if rc != 0 || !strings.Contains(out, "lib\n") || !strings.Contains(out, "newer\n") ||
		strings.Contains(out, "dirty") || !strings.Contains(out, head) {
		t.Fatalf("pinned HEAD: rc=%d\n%s", rc, out)
	}
	// An older pin is materialised as it was.
	rc, out = h.run("3", "--remote", "box", "--with", "lib@"+pinned, "--", "sh", "-c", probe)
	if rc != 0 || strings.Contains(out, "newer") || !strings.Contains(out, pinned) {
		t.Fatalf("older pin: rc=%d\n%s", rc, out)
	}
	// The ledger says where it ran and with what; the remote cleaned up after both runs.
	if r := h.lastRecord(); r.Machine != "box" || r.With != "lib@"+pinned[:12] {
		t.Fatalf("record: machine=%q with=%q", r.Machine, r.With)
	}
	wts, _ := filepath.Glob(filepath.Join(h.root, "remote-work", "wt", "*"))
	if len(wts) != 0 {
		t.Fatalf("remote worktrees left behind: %v", wts)
	}
}

func TestE2EWithAutoUsesOnlyRemotesWhenALocalSiblingIsOff(t *testing.T) {
	h, _, lib := withRig(t, "")
	h.setConfig(remoteConfig(h, ""))
	if err := os.WriteFile(filepath.Join(lib, "scratch"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rc, out := h.run("3", "--remote", "auto", "--with", "lib", "--", "sh", "-c", "ls ../lib")
	if rc != 0 || !strings.Contains(out, "lib: uncommitted changes => remote machines only") {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
	if r := h.lastRecord(); r.Machine != "box" {
		t.Fatalf("ran on %q, want box", r.Machine)
	}
	// ../lib in the command does not keep a --with run from going remote by default placement.
	gitIn(t, h, lib, "clean", "-fdq")
	rc, out = h.run("3", "--with", "lib", "--", "sh", "-c", "ls ../lib")
	if rc != 0 {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
	if hist := h.history(); len(hist) != 2 {
		t.Fatalf("history: %d records", len(hist))
	}
}

func TestE2EWithCacheKeyHasTheSiblingCommit(t *testing.T) {
	h, _, lib := withRig(t, baseConfig)
	args := []string{"3", "--remote", "local", "--cache", "--with", "lib", "--", "sh", "-c", "echo run >> " + filepath.Join(h.root, "count-a")}
	for i, want := range []int{1, 1, 2} { // run, hit, then a new sibling commit: run again
		if i == 2 {
			commitIn(t, h, lib, "a.txt", "newer\n")
		}
		if rc, out := h.run(args...); rc != 0 || h.runs("a") != want {
			t.Fatalf("run %d: rc=%d runs=%d, want %d\n%s", i+1, rc, h.runs("a"), want, out)
		}
	}
	if r := h.lastRecord(); r.Cache != "miss" {
		t.Fatalf("last run cache=%q, want miss", r.Cache)
	}
}

func TestE2EWithShowsInStatusAndHistory(t *testing.T) {
	h, _, lib := withRig(t, baseConfig)
	sha := gitOutput(t, h, lib, "rev-parse", "HEAD")
	hold := h.start("3", "--remote", "local", "--", "sh", "-c", h.gateFile("go")) // takes the only slot
	h.running(1)
	waiter := h.start("3", "--remote", "local", "--with", "lib", "--", "true")
	h.waiting(1)
	if rc, out := h.run("status"); rc != 0 || !strings.Contains(out, "with: lib@"+sha[:12]) {
		t.Fatalf("status rc=%d\n%s", rc, out)
	}
	if rc, out := h.run("status", "--json"); rc != 0 || !strings.Contains(strings.Join(strings.Fields(out), ""), `"with":["lib@`+sha[:12]+`"]`) {
		t.Fatalf("status --json rc=%d\n%s", rc, out)
	}
	h.touch("go")
	if hold.wait(t, 20e9) != 0 || waiter.wait(t, 20e9) != 0 {
		t.Fatal("runs failed")
	}
	if rc, out := h.run("history", "-v"); rc != 0 || !strings.Contains(out, "with: lib@"+sha[:12]) {
		t.Fatalf("history rc=%d\n%s", rc, out)
	}
}
