package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/agentmode"
	"github.com/LysCongThanh/heavyq/internal/config"
)

// A waiter that adopts an edited config also adopts its probe timeout: the
// remotes it probes from then on get the new one, not the one it started with.
func TestReloadConfigAdoptsTheEditedProbeTimeout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	write := func(timeout string) {
		t.Helper()
		if err := os.WriteFile(path, []byte("[queue]\nprobe_timeout = \""+timeout+"\"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("3s")
	cfg, err := config.Load(path, true)
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	r := &run{cfg: cfg, cfgWatch: config.NewWatch(cfg), stderr: &stderr,
		probes: newProbeCache(cfg.Queue.ProbeTimeout.Duration)}
	r.out = newRunOutput(agentmode.Human, false, &stderr, time.Now(), "x")
	if rc := r.reloadConfig(); rc != 0 || r.probes.timeout != 3*time.Second {
		t.Fatalf("untouched file: rc=%d timeout=%v", rc, r.probes.timeout)
	}

	write("9s")
	if rc := r.reloadConfig(); rc != 0 {
		t.Fatalf("rc=%d:\n%s", rc, &stderr)
	}
	r.probes.mu.Lock()
	got := r.probes.timeout
	r.probes.mu.Unlock()
	if got != 9*time.Second || r.cfg.Queue.ProbeTimeout.Duration != 9*time.Second {
		t.Fatalf("probe timeout after the edit = %v (config %v):\n%s", got, r.cfg.Queue.ProbeTimeout.Duration, &stderr)
	}
	if !strings.Contains(stderr.String(), "config changed while waiting") {
		t.Fatalf("the reload is announced:\n%s", &stderr)
	}
}
