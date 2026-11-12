package remote

import (
	"crypto/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// isolateGitConfig replaces the user's and the system's git config for every git this test starts with the
// given global config, so a test of how shipping treats git config does not depend on the machine it runs on.
func isolateGitConfig(t *testing.T, global string) {
	t.Helper()
	f := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(f, []byte(global), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", f)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

// Runs gated on the same new commit and admitted together on one remote all ship it. receive-pack creates
// the ref against the old value the push saw (none), so the slower push used to be refused with "reference
// already exists" and its run exited 77 although the commit was there. On a remote with no bare repo yet
// (the first `gate run --parallel` of a repo) the runs also race on `git init --bare`: the losers used to
// spend their first attempt on "could not lock config file" and their only retry on the ref race. The pack
// is not trivial so the winner's push is still going while the others start theirs.
func TestConcurrentFirstPushesOfOneCommitAllSucceed(t *testing.T) {
	repo, _ := newRepo(t, t.TempDir(), "app", "f", "x")
	big := make([]byte, 2<<20)
	if _, err := rand.Read(big); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "blob.bin"), big, 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-q", "-m", "big")
	sha := gitRun(t, repo, "rev-parse", "HEAD")
	const pushes = 6
	for round := 0; round < 8; round++ {
		m := fakeRemote(t) // a fresh remote: no bare repo, and the commit and its ref are new to it
		errs := make(chan error, pushes)
		for i := 0; i < pushes; i++ {
			go func() { errs <- Push(m, repo, "app", sha, sha[:12], 20*time.Second) }()
		}
		for i := 0; i < pushes; i++ {
			if err := <-errs; err != nil {
				t.Fatalf("round %d: a concurrent push of the same commit failed: %v", round, err)
			}
		}
	}
}

// Waiting out a lost race for the bare repo must not hide a real failure of git init: a path that cannot
// become a repository still fails the ship, with git's own message.
func TestPushReportsAGitInitThatCannotSucceed(t *testing.T) {
	repo, sha := newRepo(t, t.TempDir(), "app", "f", "x")
	m := fakeRemote(t)
	if err := os.MkdirAll(filepath.Join(m.WorkDir, "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(m.WorkDir, "git", "app.git"), []byte("not a repo"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Push(m, repo, "app", sha, sha[:12], 20*time.Second)
	if err == nil || !strings.Contains(err.Error(), "prepare bare repo") || !strings.Contains(err.Error(), "app.git") {
		t.Fatalf("a bare repo that cannot be created must fail the ship with git's reason, got %v", err)
	}
}

// Shipping is plumbing: the user's push settings are for their own remotes. push.recurseSubmodules=on-demand
// used to push a submodule's unpushed branch to the submodule's real origin, push.gpgSign and push.pushOption
// made every ship fail because the bare repo supports neither.
func TestPushIgnoresTheUsersPushConfig(t *testing.T) {
	isolateGitConfig(t, "[push]\n\tautoSetupRemote = true\n[protocol \"file\"]\n\tallow = always\n")
	src := t.TempDir()
	lib, _ := newRepo(t, src, "lib", "l.txt", "lib")
	libOrigin := filepath.Join(src, "lib-origin.git")
	gitRun(t, src, "clone", "-q", "--bare", lib, libOrigin)
	app, _ := newRepo(t, src, "app", "a.txt", "app")
	gitRun(t, app, "submodule", "add", "-q", libOrigin, "libs")
	sub := filepath.Join(app, "libs") // newRepo already has a directory "sub"
	gitRun(t, sub, "checkout", "-q", "-b", "feature")
	if err := os.WriteFile(filepath.Join(sub, "wip.txt"), []byte("not for origin"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, sub, "add", ".")
	gitRun(t, sub, "commit", "-q", "-m", "wip")
	gitRun(t, app, "add", ".")
	gitRun(t, app, "commit", "-q", "-m", "with submodule")
	sha := gitRun(t, app, "rev-parse", "HEAD")
	for _, kv := range [][2]string{{"push.recurseSubmodules", "on-demand"}, {"push.gpgSign", "true"}, {"push.pushOption", "ci.skip"}} {
		gitRun(t, app, "config", kv[0], kv[1])
	}

	m := fakeRemote(t)
	if err := Push(m, app, "app", sha, sha[:12], 20*time.Second); err != nil {
		t.Fatalf("the user's push config must not decide a ship: %v", err)
	}
	if refs := gitRun(t, libOrigin, "for-each-ref", "--format=%(refname)"); strings.Contains(refs, "feature") {
		t.Fatalf("shipping pushed the submodule's unpushed branch to its origin:\n%s", refs)
	}
	if up, err := exec.Command("git", "-C", sub, "config", "--get", "branch.feature.remote").Output(); err == nil {
		t.Fatalf("shipping set an upstream for the submodule's branch: %s", up)
	}
}
