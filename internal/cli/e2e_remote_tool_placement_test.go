package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These tests use the two fake remotes of e2e_remote_machine_list_test.go (box and box2, reached through
// `sh -c`, so both are this machine) to show placement by tools (#148): a command that resolves only on
// the PATH box2's jobs get (its env_path) runs only there, a remote without it is skipped with the reason
// in `why` and `status`, an explicit remote without it fails before any code is shipped, and an exit 127
// on a remote names that remote's env_path.

// onlyTool is a command that exists only on box2: an executable in a directory that is box2's env_path.
const onlyTool = "hqtool-e2e"

// toolRemotes configures box and box2 with box2's env_path holding onlyTool, makes the repository "app"
// and returns the directory that holds the tool.
func toolRemotes(t *testing.T, h *rig, extra string) string {
	t.Helper()
	dir := filepath.Join(h.root, "only-on-box2")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, onlyTool), []byte("#!/bin/sh\necho "+onlyTool+" ran\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	h.setConfig(twoRemotesConfig(h, "env_path = \""+dir+"\"\n"+extra))
	gitRepo(t, h)
	return dir
}

func TestE2ERemoteToolsPlaceTheRunWhereItsCommandResolves(t *testing.T) {
	h := newRig(t, "")
	toolRemotes(t, h, "")
	// box is first in the list and free, but the command is not on the PATH its jobs get.
	rc, out := h.run("3", "--remote", "box,box2", "--lane", "argv0", "--", onlyTool)
	if rc != 0 || !strings.Contains(out, onlyTool+" ran") {
		t.Fatalf("rc=%d, want the run on box2:\n%s", rc, out)
	}
	if got := h.machineOfLane("argv0"); got != "box2" {
		t.Fatalf("ran on %q, want box2 (box lacks %s)", got, onlyTool)
	}
	// Through `sh -c`: the first word of the script is the command.
	if rc, out := h.run("3", "--remote", "box,box2", "--lane", "shc", "--", "sh", "-c", "FOO=1 "+onlyTool+" && echo after"); rc != 0 {
		t.Fatalf("sh -c rc=%d:\n%s", rc, out)
	}
	if got := h.machineOfLane("shc"); got != "box2" {
		t.Fatalf("sh -c ran on %q, want box2", got)
	}
	// An explicit remote that lacks a tool fails before anything is shipped, naming the tools and env_path.
	rc, out = h.run("3", "--remote", "box", "--tools", onlyTool+",hq-not-anywhere", "--", "true")
	for _, want := range []string{"box", onlyTool, "hq-not-anywhere", "env_path"} {
		if rc != ExitUsage || !strings.Contains(out, want) {
			t.Fatalf("--remote box --tools: rc=%d (want %d), output must name %q:\n%s", rc, ExitUsage, want, out)
		}
	}
	if _, err := os.Stat(filepath.Join(h.root, "remote-work", "git", "app.git")); !os.IsNotExist(err) {
		t.Fatalf("code was shipped to box, which cannot run the command (stat: %v)", err)
	}
	// Tool names are plain command names: paths and shell syntax are refused.
	for _, bad := range []string{"../bin/x", "a b", "-rf", "x;y", ",go"} {
		if rc, out := h.run("3", "--tools", bad, "--", "true"); rc != ExitUsage || !strings.Contains(out, "--tools") {
			t.Fatalf("--tools %q: rc=%d, want %d:\n%s", bad, rc, ExitUsage, out)
		}
	}
	if n := len(h.history()); n != 2 {
		t.Fatalf("history has %d runs, want the 2 that ran", n)
	}
}

func TestE2EWhyAndStatusNameTheRemoteThatLacksATool(t *testing.T) {
	h := newRig(t, "")
	toolRemotes(t, h, "")
	hold := h.start("3", "--remote", "box2", "--lane", "hold", "--", "sh", "-c", h.gateFile("go"))
	h.running(1)
	wants := h.start("3", "--remote", "box,box2", "--lane", "wants", "--", onlyTool)
	h.waiting(1) // box is free but lacks the tool; box2 has it but is busy
	rc, out := h.run("why", "wants")
	if rc != 0 || !strings.Contains(out, "box: lacks "+onlyTool) {
		t.Fatalf("why rc=%d, must name the tool box lacks:\n%s", rc, out)
	}
	if rc, out = h.run("status"); rc != 0 || !strings.Contains(out, "lacks "+onlyTool) {
		t.Fatalf("status rc=%d, must name the tool box lacks:\n%s", rc, out)
	}
	h.touch("go")
	hold.wait(t, 20*time.Second)
	if rc := wants.wait(t, 20*time.Second); rc != 0 {
		t.Fatalf("waiting run rc=%d:\n%s", rc, wants.out)
	}
	if got := h.machineOfLane("wants"); got != "box2" {
		t.Fatalf("ran on %q, want box2", got)
	}
}

func TestE2ERemoteExit127NamesTheRemoteAndItsEnvPath(t *testing.T) {
	h := newRig(t, "")
	dir := toolRemotes(t, h, "")
	tool := filepath.Join(dir, onlyTool)
	if rc, out := h.run("3", "--remote", "box2", "--", onlyTool); rc != 0 {
		t.Fatalf("first run rc=%d:\n%s", rc, out)
	}
	// The answer "box2 has it" is cached, so the next run goes there without asking and meets a 127.
	if err := os.Rename(tool, tool+".off"); err != nil {
		t.Fatal(err)
	}
	rc, out := h.run("3", "--remote", "box2", "--", onlyTool)
	if rc != 127 || !strings.Contains(out, "box2") || !strings.Contains(out, "env_path") {
		t.Fatalf("rc=%d (want 127), the hint must name box2 and env_path:\n%s", rc, out)
	}
	// The 127 dropped what was cached about box2: it is asked again, and the run fails before shipping.
	if rc, out = h.run("3", "--remote", "box2", "--", onlyTool); rc != ExitUsage || !strings.Contains(out, "box2 lacks "+onlyTool) {
		t.Fatalf("after the 127: rc=%d (want %d):\n%s", rc, ExitUsage, out)
	}
	// Installed again: a cached "missing" never refuses a run without asking the remote once more.
	if err := os.Rename(tool+".off", tool); err != nil {
		t.Fatal(err)
	}
	if rc, out = h.run("3", "--remote", "box2", "--", onlyTool); rc != 0 {
		t.Fatalf("after reinstalling: rc=%d:\n%s", rc, out)
	}
}

func TestE2EGateToolsPlaceTheGateAndDoctorListsThem(t *testing.T) {
	h := newRig(t, "")
	toolRemotes(t, h, "")
	app := h.cwd
	// argv[0] of the command is bash running a script file: only the declared tool tells box from box2.
	files := map[string]string{
		"heavyq.toml":      "[[gate]]\nname = \"envgate\"\nrun = [\"bash scripts/check.sh\"]\nremote = \"box,box2\"\ntools = [\"" + onlyTool + "\"]\n",
		"scripts/check.sh": onlyTool + "\n",
	}
	for name, body := range files {
		p := filepath.Join(app, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitIn(t, h, app, "add", ".")
	gitIn(t, h, app, "commit", "-q", "-m", "gate with tools")
	if rc, out, errOut := h.inDir(app, nil, "gate", "run", "envgate"); rc != 0 {
		t.Fatalf("gate run rc=%d:\n%s%s", rc, out, errOut)
	}
	h.wantMachines("envgate", "box2")
	rc, out, errOut := h.inDir(app, nil, "doctor")
	lines := map[string]string{}
	for _, l := range strings.Split(out, "\n") {
		if name, rest, ok := strings.Cut(strings.TrimSpace(l), ": "); ok {
			lines[name] = rest
		}
	}
	box, box2 := lines["box"], lines["box2"]
	_, boxMissing, _ := strings.Cut(box, "missing:")
	box2Found, _, _ := strings.Cut(box2, "missing:")
	if rc != 0 || !strings.Contains(boxMissing, onlyTool) || !strings.Contains(box2Found, onlyTool) {
		t.Fatalf("doctor rc=%d: box must miss %s and box2 have it:\n%s%s", rc, onlyTool, out, errOut)
	}
}
