package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/control"
)

// A view that stays open (top, the dashboard) follows an edit of the config in
// everything it does with it, not only in its snapshots: its probes take the
// edited probe_timeout, and `top --control` pauses a remote the edit added.
func TestLiveConfigFollowsEditsInProbesAndControlRequests(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("[queue]\nprobe_timeout = \"3s\"\n")
	cfg, err := config.Load(path, true)
	if err != nil {
		t.Fatal(err)
	}
	probes := newProbeCache(cfg.Queue.ProbeTimeout.Duration)
	live := newLiveConfig(cfg, probes)
	a := &topActions{cfg: live.get, box: control.Mailbox{Dir: filepath.Join(dir, "ctl")}}
	if err := a.SetPaused("late", true); err == nil {
		t.Fatal("a machine the config does not have cannot be paused")
	}

	write("[queue]\nprobe_timeout = \"9s\"\n[[remote]]\nname = \"late\"\nssh = \"late\"\n")
	live.get() // the first read of an edit is only remembered (config.Watch)
	if err := a.SetPaused("late", true); err != nil {
		t.Fatalf("a remote added by the edit can be paused: %v", err)
	}
	probes.mu.Lock()
	got := probes.timeout
	probes.mu.Unlock()
	if got != 9*time.Second {
		t.Fatalf("probe timeout after the edit = %v, want 9s", got)
	}
}
