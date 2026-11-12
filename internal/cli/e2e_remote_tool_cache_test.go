package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// These tests count the tool probes (#148): the two fake remotes are reached through a transport script that
// notes each probe (the one command line that holds the probe script, which says "heavyq-tool") before it runs
// the command through `sh -c`. So a cached answer can be told from a new probe. "box" has no env_path; "box2"
// has an env_path directory that holds the tool, so the same command resolves on one and not on the other.

const cachedTool = "hqtool-cache"

// countingRemotes configures box and box2 behind the counting transport, with box2's env_path set to
// envPath2, and returns the file that gets one line per tool probe (see rig.lines).
func countingRemotes(h *rig, envPath2 string) string {
	counter := filepath.Join(h.root, "tool-probes")
	transport := filepath.Join(h.root, "count-transport.sh")
	script := "#!/bin/sh\ncase \"$1\" in *heavyq-tool*) echo probe >> " + counter + " ;; esac\nexec sh -c \"$1\"\n"
	if err := os.WriteFile(transport, []byte(script), 0o755); err != nil {
		h.t.Fatal(err)
	}
	table := func(name, extra string) string {
		work := filepath.Join(h.root, name+"-work")
		return "[[remote]]\nname = \"" + name + "\"\ntransport = [\"" + transport + "\"]\nwork_dir = \"" + work +
			"\"\ngit_url = \"" + filepath.Join(work, "git") + "\"\nheavy_slots = 1\n" + extra
	}
	h.setConfig(baseConfig + table("box", "") + table("box2", "env_path = \""+envPath2+"\"\n"))
	return counter
}

// toolDirWith makes a directory that holds an executable per name, to be a remote's env_path.
func toolDirWith(t *testing.T, names ...string) string {
	t.Helper()
	dir := realTempDir(t)
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("#!/bin/sh\necho "+n+" ok\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestE2ERemoteToolAnswersAreCachedPerRemoteAndFollowTheConfig(t *testing.T) {
	h := newRig(t, "")
	countingRemotes(h, toolDirWith(t, cachedTool))
	gitRepo(t, h)
	run := func(lane string) {
		t.Helper()
		if rc, out := h.run("3", "--remote", "box,box2", "--tools", cachedTool, "--lane", lane, "--", "true"); rc != 0 {
			t.Fatalf("%s: rc=%d\n%s", lane, rc, out)
		}
		if got := h.machineOfLane(lane); got != "box2" {
			t.Fatalf("%s ran on %q, want box2 (box lacks %s)", lane, got, cachedTool)
		}
	}
	run("one")
	if n := h.lines("tool-probes"); n != 2 {
		t.Fatalf("the first run asks each remote once, got %d probes", n)
	}
	for _, f := range []string{"box.json", "box2.json"} { // the answers sit beside the ledger, one file per remote
		if _, err := os.Stat(filepath.Join(h.root, "remote-tools", f)); err != nil {
			t.Fatalf("the answer is kept in <state>/remote-tools: %v", err)
		}
	}
	run("two") // another process, inside the TTL: the cached answers are used, nothing is asked
	if n := h.lines("tool-probes"); n != 2 {
		t.Fatalf("a cached answer must not be probed again, got %d probes", n)
	}
	// env_path is part of what an answer is about: changing it asks again, for that remote only.
	countingRemotes(h, toolDirWith(t, cachedTool, "other-tool"))
	run("three")
	if n := h.lines("tool-probes"); n != 3 {
		t.Fatalf("a changed env_path asks that remote again, and only it: got %d probes, want 3", n)
	}
}

// A list that names local is not refused when every remote lacks the tool: this machine takes the run, and
// is not probed (the command either runs here or exits 127 as it always did).
func TestE2ERemoteToolsFallBackToLocalWhenItIsACandidate(t *testing.T) {
	h := newRig(t, "")
	countingRemotes(h, toolDirWith(t))
	gitRepo(t, h)
	rc, out := h.run("3", "--remote", "box,box2,local", "--tools", "hq-not-anywhere", "--lane", "fb", "--", "true")
	if got := h.machineOfLane("fb"); rc != 0 || got != "local" {
		t.Fatalf("rc=%d on %q, want 0 on local:\n%s", rc, got, out)
	}
	if n := h.lines("tool-probes"); n != 2 {
		t.Fatalf("each remote is asked once and local never: got %d probes", n)
	}
}
