package remote

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The canary runs through the real job script on the fake remote (sh -c), with the setsid of the incident
// first on the remote's env_path.
func TestExitCanaryVerdicts(t *testing.T) {
	cases := []struct {
		name    string
		wrapper string // "" = the machine's own PATH
		trusted bool
	}{
		{"own PATH", "", true},
		{"forking setsid: the job script does not use it", forkingSetsid, true},
		{"setsid that also lies about its version", lyingSetsid, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo, sha := pushApp(t)
			m := fakeRemote(t)
			if c.wrapper != "" {
				m.EnvPath = wrapperDir(t, c.wrapper)
			}
			if err := Push(m, repo, "app", sha, sha[:12], 20*time.Second); err != nil {
				t.Fatal(err)
			}
			trusted, reason, err := RunCanary(m, CanaryJob{Repo: "app", SHA: sha, Lane: "x", Slot: "h1"}, 30*time.Second)
			if !trusted {
				// the lost command finishes on its own: let it write before the temp dir goes
				waitForGlob(filepath.Join(m.WorkDir, "cache", "heavyq-canary-*"), 3*time.Second)
			}
			if err != nil || trusted != c.trusted || (!trusted && !strings.Contains(reason, "came back as 0")) {
				t.Fatalf("trusted=%v reason=%q err=%v, want trusted=%v", trusted, reason, err, c.trusted)
			}
			assertNoWorktrees(t, m)
		})
	}
}

// A canary that cannot run (here: the commit is not on the remote) is no verdict either way.
func TestExitCanaryThatCannotRunIsNoVerdict(t *testing.T) {
	m := fakeRemote(t)
	_, _, err := RunCanary(m, CanaryJob{Repo: "app", SHA: strings.Repeat("a", 40), Lane: "x", Slot: "h1"}, 30*time.Second)
	if err == nil || !strings.Contains(err.Error(), "98") {
		t.Fatalf("err = %v, want the setup failure", err)
	}
}

func TestExitCanaryStoreKeyAndTTL(t *testing.T) {
	dir := t.TempDir()
	m := fakeRemote(t)
	now := time.Now()
	if err := SaveCanary(dir, m, CanaryVerdict{Trusted: false, Reason: "r", At: now}); err != nil {
		t.Fatal(err)
	}
	v, ok := LoadCanary(dir, m)
	if !ok || v.Trusted || v.Reason != "r" || !v.Fresh(now) || v.Fresh(now.Add(CanaryFailTTL)) {
		t.Fatalf("loaded %+v ok=%v", v, ok)
	}
	if (CanaryVerdict{Trusted: true, At: now}).Fresh(now.Add(CanaryPassTTL)) {
		t.Fatal("a pass outlives its TTL")
	}
	m.EnvPath = "/elsewhere/bin" // another setup: the stored answer is not about it
	if _, ok := LoadCanary(dir, m); ok {
		t.Fatal("a verdict for another env_path was reused")
	}
}

func waitForGlob(pattern string, d time.Duration) {
	for deadline := time.Now().Add(d); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if m, _ := filepath.Glob(pattern); len(m) > 0 {
			return
		}
	}
}
