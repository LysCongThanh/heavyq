package remote

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Shipping a commit is plumbing, not a git operation the repository's owner
// asked for: no git hook may run in it, whether the hook belongs to the repo
// being shipped or to the machine that receives it, and nothing a hook prints
// may reach the job's output. The hooks here are real executables that log,
// print and fail; the "remote" is this machine reached through `sh -c`.

// failingHooks writes one executable hook per name into dir. Each appends its
// name to the returned log file, prints a line on stdout and exits 1, so a hook
// that runs either blocks the operation or leaves a trace.
func failingHooks(t *testing.T, dir string, names ...string) (logFile string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	logFile = filepath.Join(t.TempDir(), "hooks.log")
	for _, n := range names {
		body := "#!/bin/sh\necho " + n + " >> '" + logFile + "'\necho 'stdout of " + n + "'\nexit 1\n"
		if err := os.WriteFile(filepath.Join(dir, n), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return logFile
}

// receivingHooks are the hooks a push to a bare repo and a worktree made from it can run.
var receivingHooks = []string{"pre-receive", "update", "post-receive", "post-update", "post-checkout", "reference-transaction"}

// globalHooksPath makes every git this process starts (and the "remote" it reaches through sh -c, which
// is this machine) use hooksDir as core.hooksPath, the way a machine with a global hooks directory does.
func globalHooksPath(t *testing.T, hooksDir string) {
	t.Helper()
	home := t.TempDir()
	cfg := "[core]\n\thooksPath = " + hooksDir + "\n"
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

func assertNoHookRan(t *testing.T, logFile string) {
	t.Helper()
	if b, err := os.ReadFile(logFile); err == nil {
		t.Fatalf("git hooks ran while shipping: %s", strings.Fields(string(b)))
	}
}

func TestPushSkipsThePrePushHookOfTheShippedRepo(t *testing.T) {
	repo, sha := newRepo(t, t.TempDir(), "app", "f", "x")
	logFile := failingHooks(t, filepath.Join(repo, ".git", "hooks"), "pre-push")
	m := fakeRemote(t)
	if err := Push(m, repo, "app", sha, sha[:12], 20*time.Second); err != nil {
		t.Fatalf("a failing pre-push hook must not stop the ship: %v", err)
	}
	assertNoHookRan(t, logFile)
	rc, out, _ := runJob(t, JobSpec{Machine: m, Repo: "app", SHA: sha, Lane: "x", Slot: "h1", Argv: []string{"cat", "f"}})
	if rc != 0 || out != "x" {
		t.Fatalf("rc=%d out=%q", rc, out)
	}
}

func TestShippingIgnoresHooksOfTheReceivingMachine(t *testing.T) {
	repo, sha := newRepo(t, t.TempDir(), "app", "f", "x")
	hooksDir := filepath.Join(t.TempDir(), "global-hooks")
	logFile := failingHooks(t, hooksDir, append([]string{"pre-push"}, receivingHooks...)...)
	globalHooksPath(t, hooksDir) // after the commit: only the ship runs under these hooks
	m := fakeRemote(t)
	for i := 0; i < 2; i++ { // the second push meets a bare repo that already exists
		if err := Push(m, repo, "app", sha, sha[:12], 20*time.Second); err != nil {
			t.Fatalf("push %d: a hook of the receiving machine must not stop the ship: %v", i+1, err)
		}
	}
	rc, out, errs := runJob(t, JobSpec{Machine: m, Repo: "app", SHA: sha, Lane: "x", Slot: "h1", Argv: []string{"cat", "f"}})
	if rc != 0 || out != "x" {
		t.Fatalf("rc=%d out=%q err=%q", rc, out, errs)
	}
	if strings.Contains(out+errs, "stdout of") {
		t.Fatalf("hook output reached the job:\nstdout:%q\nstderr:%q", out, errs)
	}
	assertNoHookRan(t, logFile)
	assertNoWorktrees(t, m)
}

// A bare repo made by an earlier heavyq (or by git init with a template that carries hooks) already has
// hooks in place: the next ship must switch them off, not only the repos it creates.
func TestShippingSilencesHooksAlreadyInAnExistingBareRepo(t *testing.T) {
	repo, sha := newRepo(t, t.TempDir(), "app", "f", "x")
	m := fakeRemote(t)
	bare := filepath.Join(m.GitURL, "app.git")
	if err := os.MkdirAll(m.GitURL, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "init", "-q", "--bare", bare)
	logFile := failingHooks(t, filepath.Join(bare, "hooks"), receivingHooks...)
	if err := Push(m, repo, "app", sha, sha[:12], 20*time.Second); err != nil {
		t.Fatalf("hooks in an existing bare repo must not stop the ship: %v", err)
	}
	rc, out, errs := runJob(t, JobSpec{Machine: m, Repo: "app", SHA: sha, Lane: "x", Slot: "h1", Argv: []string{"cat", "f"}})
	if rc != 0 || out != "x" || strings.Contains(errs, "stdout of") {
		t.Fatalf("rc=%d out=%q err=%q", rc, out, errs)
	}
	assertNoHookRan(t, logFile)
}
