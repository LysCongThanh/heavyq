package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/agentmode"
	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/queue"
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
	for range 2 { // the first check that reads an edit only remembers it
		if rc := r.reloadConfig(); rc != 0 {
			t.Fatalf("rc=%d:\n%s", rc, &stderr)
		}
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

// A config edit refused because the repository's heavyq.toml (whose pools the
// ticket's needs may refer to) does not load is retried on every poll, so fixing
// heavyq.toml alone is enough, and the refusal is reported once, not per poll.
func TestReloadConfigRetriesAnEditRefusedForABrokenHeavyqToml(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "app")
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	path := filepath.Join(root, "config.toml")
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(name, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	const pool = "[[pool]]\nname = \"one\"\nsize = 2\n"
	write(path, pool)
	cfg, err := config.Load(path, true)
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	r := &run{cfg: cfg, cfgWatch: config.NewWatch(cfg), stderr: &stderr, pl: &placement{wd: repo}, me: &queue.Ticket{},
		probes: newProbeCache(cfg.Queue.ProbeTimeout.Duration)}
	r.out = newRunOutput(agentmode.Human, false, &stderr, time.Now(), "x")
	if err := r.opts.addNeeds("one"); err != nil {
		t.Fatal(err)
	}
	if err := r.preparePools(); err != nil {
		t.Fatal(err)
	}
	if rc := r.reloadConfig(); rc != 0 { // the first check reads the file once more
		t.Fatalf("rc=%d:\n%s", rc, &stderr)
	}

	write(filepath.Join(repo, "heavyq.toml"), "[[pool]\n") // does not parse
	write(path, "[queue]\npoll = \"3s\"\n"+pool)
	old := time.Now().Add(-time.Minute) // past the watch's racy window: a stat alone says "unchanged"
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if rc := r.reloadConfig(); rc != 0 || r.cfg.Queue.Poll.Duration == 3*time.Second {
			t.Fatalf("an edit refused for heavyq.toml must not apply: rc=%d poll=%v\n%s", rc, r.cfg.Queue.Poll.Duration, &stderr)
		}
	}
	if n := strings.Count(stderr.String(), "config edit not applied"); n != 1 {
		t.Fatalf("the refusal is reported once, got %d:\n%s", n, &stderr)
	}

	write(filepath.Join(repo, "heavyq.toml"), "")
	if rc := r.reloadConfig(); rc != 0 || r.cfg.Queue.Poll.Duration != 3*time.Second { // the config file reads the same: no second check needed
		t.Fatalf("fixing heavyq.toml alone lets the edit apply: rc=%d poll=%v\n%s", rc, r.cfg.Queue.Poll.Duration, &stderr)
	}
}
