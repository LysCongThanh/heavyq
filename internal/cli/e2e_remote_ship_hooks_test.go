package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Shipping a commit to a remote never runs a git hook: not the repository's
// own pre-push, and not a hook of the machine that receives it (a global
// core.hooksPath). Hooks here are real executables that log, print and fail.

func TestE2ERemoteRunRunsNoGitHooksAndShowsNoHookOutput(t *testing.T) {
	h := newAgentRig(t)
	h.setConfig(remoteConfig(h, ""))
	repo := gitRepo(t, h)

	logFile := filepath.Join(h.root, "hooks.log")
	hook := "#!/bin/sh\necho \"$0\" >> '" + logFile + "'\necho 'stdout of a hook'\nexit 1\n"
	write := func(dir, name string) {
		t.Helper()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(hook), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	repoHooks := filepath.Join(h.root, "repo-hooks") // the repository's own hooks, as a tracked hooks directory would be
	write(repoHooks, "pre-push")
	gitIn(t, h, repo, "config", "core.hooksPath", repoHooks)
	global := filepath.Join(h.root, "global-hooks")
	for _, n := range []string{"pre-push", "pre-receive", "update", "post-receive", "post-update", "post-checkout", "reference-transaction"} {
		write(global, n)
	}
	// The user's own git config is replaced, so the test does not depend on the machine it runs on.
	// GIT_CONFIG_GLOBAL (git 2.32+), not HOME: a git installed as a wrapper that finds its real binary
	// under $HOME (a portable git on a Mac without developer tools) stops working when HOME moves.
	gitconfig := filepath.Join(h.root, "gitconfig")
	if err := os.WriteFile(gitconfig, []byte("[core]\n\thooksPath = "+global+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.env = append(h.env, "GIT_CONFIG_GLOBAL="+gitconfig, "GIT_CONFIG_NOSYSTEM=1")

	rc, out := h.run("3", "--remote", "box", "--lane", "api", "--", "sh", "-c", "cat marker.txt")
	noANSI(t, out)
	if rc != 0 || !strings.Contains(out, "from-commit") {
		t.Fatalf("a failing hook must not stop the run: rc=%d\n%s", rc, out)
	}
	if strings.Contains(out, "stdout of a hook") {
		t.Fatalf("hook output reached the run's output:\n%s", out)
	}
	if b, err := os.ReadFile(logFile); err == nil {
		t.Fatalf("git hooks ran while shipping: %s", b)
	}
	if n := len(heavyqLines(out)); n != 2 {
		t.Fatalf("agent budget: %d heavyq lines, want running + digest:\n%s", n, out)
	}
}
