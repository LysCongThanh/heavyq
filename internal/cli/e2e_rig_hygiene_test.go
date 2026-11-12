package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/procinfo"
)

// A test that fails while an external helper (flock and the like) still runs
// must not leave what that helper started running: the rig's cleanup stops
// the helper's whole process group, and a gate-file loop ends once the rig's
// directory is gone.
func TestRigCleanupStopsWhatAHelperStarted(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "loop.pid")
	t.Run("a helper with a child", func(t *testing.T) {
		h := newSerialRig(t, baseConfig)
		newExternal(t, h, "sh", "-c", "sh -c '"+h.gateFile("never")+"' & echo $! > "+pidFile+"; wait")
		h.until("the helper's child", func() bool { return atoiTrim(fileText(pidFile)) > 0 })
	})
	pid := atoiTrim(fileText(pidFile))
	defer func() { _ = killPid(pid) }()
	deadline := time.Now().Add(10 * time.Second)
	for procinfo.Default.Running(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("the helper's child %d outlived the test", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// The developer's own git configuration (a global commit-msg hook, excludes,
// templates) must not reach the rig's repositories: a contributor whose hook
// enforces conventional commits would otherwise fail every repo-based test.
// The global file is planted through GIT_CONFIG_GLOBAL, not a fake HOME: git
// on PATH may be a wrapper that finds the real git through HOME.
func TestRigGitIgnoresTheGlobalGitConfig(t *testing.T) {
	dir := t.TempDir()
	hooks := filepath.Join(dir, "hooks")
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hooks, "commit-msg"), []byte("#!/bin/sh\necho rejected >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	global := filepath.Join(dir, "gitconfig")
	if err := os.WriteFile(global, []byte("[core]\n\thooksPath = "+hooks+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	if out, err := exec.Command("git", "config", "--global", "core.hooksPath").Output(); err != nil || len(out) == 0 {
		t.Fatalf("this process does not see the planted global config: %q %v", out, err)
	}
	h := newSerialRig(t, baseConfig)
	cmd := exec.Command("git", "config", "--global", "core.hooksPath")
	cmd.Env = h.env
	if out, err := cmd.Output(); err == nil && len(out) > 0 {
		t.Fatalf("the rig sees the global git config: core.hooksPath = %s", out)
	}
	gitRepo(t, h) // commits: a global commit-msg hook would reject it
}

func fileText(path string) string { b, _ := os.ReadFile(path); return string(b) }

func killPid(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}
