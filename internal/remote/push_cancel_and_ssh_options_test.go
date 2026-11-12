package remote

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
)

// sshRemote is fakeRemote whose git URL goes through ssh ("host:path"), so git push starts whatever
// GIT_SSH_COMMAND or "ssh" on PATH is; the bare repo is still prepared through `sh -c`.
func sshRemote(t *testing.T) (m config.MachineConfig) {
	t.Helper()
	m = fakeRemote(t)
	m.GitURL = "fakehost:" + m.GitURL
	return m
}

// waitForFile waits up to 15 s for path to exist and returns its contents.
func waitForFile(t *testing.T, path string) string {
	t.Helper()
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
			return strings.TrimSpace(string(b))
		}
	}
	t.Fatalf("%s never appeared", path)
	return ""
}

// gone waits up to 5 s for pid to stop existing.
func gone(pid int) bool {
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if syscall.Kill(pid, 0) != nil {
			return true
		}
	}
	return false
}

// A push over a link that went silent used to block heavyq (and the slot it holds) until TCP gave up, deaf
// to SIGTERM and to `heavyq cancel`. The push now stops, with git and its ssh, when its context ends.
func TestPushStopsWithItsSSHWhenItsContextEnds(t *testing.T) {
	isolateGitConfig(t, "")
	repo, sha := newRepo(t, t.TempDir(), "app", "f", "x")
	m := sshRemote(t)
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "ssh.pid")
	stub := filepath.Join(dir, "silent-ssh")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\necho $$ > '"+pidFile+"'\nexec sleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_SSH_COMMAND", stub)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- PushContext(ctx, m, repo, "app", sha, sha[:12], 20*time.Second) }()
	pid, err := strconv.Atoi(waitForFile(t, pidFile))
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a cancelled push reported success")
		}
	case <-time.After(10 * time.Second):
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Fatal("the push ignored the end of its context")
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("the push took %s to stop", time.Since(start))
	}
	if !gone(pid) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Fatal("the push's ssh outlived the push")
	}
}

// git push reaches the remote with the same safety options as heavyq's own ssh (no prompt, bounded
// connect), plus keepalives so a dead link ends the push instead of hanging it.
func TestPushRunsSSHWithBatchModeAndKeepalives(t *testing.T) {
	isolateGitConfig(t, "")
	for _, v := range []string{"GIT_SSH_COMMAND", "GIT_SSH"} {
		t.Setenv(v, "")
		_ = os.Unsetenv(v)
	}
	repo, sha := newRepo(t, t.TempDir(), "app", "f", "x")
	argsFile := filepath.Join(t.TempDir(), "ssh.args")
	stubDir(t, map[string]string{"ssh": `printf '%s\n' "$@" >> '` + argsFile + `'; exit 1`})
	if err := Push(sshRemote(t), repo, "app", sha, sha[:12], 20*time.Second); err == nil {
		t.Fatal("a push through a failing ssh succeeded")
	}
	args := waitForFile(t, argsFile)
	for _, want := range []string{"BatchMode=yes", "ConnectTimeout=", "ServerAliveInterval=", "ServerAliveCountMax="} {
		if !strings.Contains(args, want) {
			t.Fatalf("git's ssh got no %s:\n%s", want, args)
		}
	}
}

// A user's own ssh command for git wins: heavyq adds options only where git would run plain ssh.
func TestPushKeepsTheUsersOwnGitSSHCommand(t *testing.T) {
	isolateGitConfig(t, "")
	repo, sha := newRepo(t, t.TempDir(), "app", "f", "x")
	argsFile := filepath.Join(t.TempDir(), "ssh.args")
	stub := filepath.Join(t.TempDir(), "my-ssh")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" >> '"+argsFile+"'\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "config", "core.sshCommand", stub)
	for _, v := range []string{"GIT_SSH_COMMAND", "GIT_SSH"} {
		t.Setenv(v, "")
		_ = os.Unsetenv(v)
	}
	if err := Push(sshRemote(t), repo, "app", sha, sha[:12], 20*time.Second); err == nil {
		t.Fatal("a push through a failing ssh succeeded")
	}
	if args := waitForFile(t, argsFile); strings.Contains(args, "BatchMode") {
		t.Fatalf("heavyq overrode core.sshCommand:\n%s", args)
	}
}
