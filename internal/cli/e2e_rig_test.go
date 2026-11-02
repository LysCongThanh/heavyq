package cli

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/agentmode"
	"github.com/LysCongThanh/heavyq/internal/queue"
)

// These tests run the real heavyq binary as separate processes, with real
// flocks and real child processes. Each test gets its own queue dir.

var binPath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "heavyq-bin")
	if err == nil {
		dir, err = filepath.EvalSymlinks(dir) // the binary reports its own path resolved (/private/var on macOS)
	}
	if err != nil {
		panic(err)
	}
	binPath = filepath.Join(dir, "heavyq")
	// The binary the end-to-end tests run is built with the race detector, so every real run is also a race
	// test. The fast CI tier sets HEAVYQ_TEST_NORACE=1 to skip it (the full tier and local gates keep it).
	build := []string{"build", "-o", binPath, "../.."}
	if os.Getenv("HEAVYQ_TEST_NORACE") != "1" {
		build = append([]string{"build", "-race"}, build[1:]...)
	}
	if out, err := exec.Command("go", build...).CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build heavyq: %v\n%s", err, out)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// calmLimits make the local machine look idle whatever the test machine is
// doing (#67): the RAM budget is 1 MB (0 would mean "the default") and the load
// limit is the highest the config allows, so no run is ever admitted "under
// load" and a failure can only be "failed" or "flaky". A test that asserts a
// failure class, a red gate or a loop must not drop them; one that wants the
// machine to look loaded says so with loadedConfig or fakeMetrics.
const calmLimits = "job_mb = 1\nlight_mb = 1\nmax_load_ratio = 64\n"

const baseConfig = `[queue]
poll = "50ms"
resource_gate = false
[local]
heavy_slots = 1
light_slots = 1
` + calmLimits

// realTempDir is t.TempDir() with symlinks resolved. On macOS the temp directory
// is /var/folders/..., a link to /private/var/...: git and os.Executable report
// the resolved path, so a test that compares paths with what a child printed
// must build them from the resolved one.
func realTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

type rig struct {
	t    *testing.T
	root string // scratch dir; also the default cwd
	cwd  string
	env  []string
}

// newRig is a rig whose test runs in parallel with the other tests of the package: it owns its queue, ledger,
// cache, state and config directories (all under its own temp dir) and starts its own processes, so nothing
// is shared but the heavyq binary. Under -race the end-to-end tests are about 90% of the package's time and
// mostly waiting on processes, which is what makes the package fit the 5 minutes of the cli-race-fast gate.
// A test that changes the environment or working directory of the test process (t.Setenv, t.Chdir) must use
// newSerialRig instead: t.Setenv panics in a parallel test.
func newRig(t *testing.T, config string) *rig {
	t.Helper()
	t.Parallel()
	return newSerialRig(t, config)
}

// newSerialRig is newRig for a test that must not run in parallel (see newRig).
func newSerialRig(t *testing.T, config string) *rig {
	t.Helper()
	root := realTempDir(t)
	cfg := filepath.Join(root, "config.toml")
	if err := os.WriteFile(cfg, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	var env []string
	for _, e := range os.Environ() {
		name, _, _ := strings.Cut(e, "=")
		// The suite may itself run under a coding agent (CLAUDECODE=1, ...): the
		// child must not inherit agent mode unless a test asks for it.
		if !strings.HasPrefix(e, "HEAVYQ_") && !strings.HasPrefix(e, "XDG_CACHE_HOME=") && !strings.HasPrefix(e, "CLAUDE_CONFIG_DIR=") &&
			!agentmode.IsAgentVar(name) {
			env = append(env, e)
		}
	}
	// State the user's account would share (hook log, trust list, top mode, stances) lives in the rig, so
	// parallel rigs never meet; a test that wants its own location appends it later and wins.
	env = append(env, "XDG_STATE_HOME="+filepath.Join(root, "state"), "XDG_CONFIG_HOME="+filepath.Join(root, "xdg-config"),
		"XDG_DATA_HOME="+filepath.Join(root, "xdg-data"))
	env = append(env, "HEAVYQ_DIR="+filepath.Join(root, "q"), "HEAVYQ_CONFIG="+cfg,
		"HEAVYQ_HISTORY="+filepath.Join(root, "history.jsonl"), "HEAVYQ_FLOW=off", "XDG_CACHE_HOME="+filepath.Join(root, "cache"),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	return &rig{t: t, root: root, cwd: root, env: env}
}

// proc is a started heavyq process.
type proc struct {
	cmd  *exec.Cmd
	out  *lockedBuf
	done chan int
}

type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func (h *rig) start(args ...string) *proc {
	h.t.Helper()
	cmd := exec.Command(binPath, args...)
	cmd.Dir, cmd.Env = h.cwd, h.env
	p := &proc{cmd: cmd, out: &lockedBuf{}, done: make(chan int, 1)}
	cmd.Stdout, cmd.Stderr = p.out, p.out
	if err := cmd.Start(); err != nil {
		h.t.Fatal(err)
	}
	go func() { p.done <- processExit(cmd.Wait()) }()
	h.t.Cleanup(func() { _ = cmd.Process.Signal(syscall.SIGKILL) })
	return p
}

// wait returns the exit code, failing the test after timeout.
func (p *proc) wait(t *testing.T, timeout time.Duration) int {
	t.Helper()
	select {
	case rc := <-p.done:
		p.done <- rc
		return rc
	case <-time.After(timeout):
		t.Fatalf("heavyq %v did not finish in %s; output:\n%s", p.cmd.Args[1:], timeout, p.out)
		return -1
	}
}

// run executes heavyq to completion.
func (h *rig) run(args ...string) (int, string) {
	h.t.Helper()
	p := h.start(args...)
	rc := p.wait(h.t, 60*time.Second)
	return rc, p.out.String()
}

func (h *rig) snapshot() queue.Snapshot {
	d, err := queue.Open(filepath.Join(h.root, "q"))
	if err != nil {
		h.t.Fatal(err)
	}
	s, _ := d.List()
	return s
}

// until polls cond for up to 20 s.
func (h *rig) until(what string, cond func() bool) {
	h.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			h.t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (h *rig) waiting(n int) {
	h.t.Helper()
	h.until(fmt.Sprintf("%d waiting tickets", n), func() bool { return len(h.snapshot().Waiting) == n })
}

func (h *rig) running(n int) {
	h.t.Helper()
	h.until(fmt.Sprintf("%d running tickets", n), func() bool { return len(h.snapshot().Running) == n })
}

// gateFile returns a shell snippet that blocks until file name exists in root.
func (h *rig) gateFile(name string) string {
	return fmt.Sprintf(`while [ ! -e %q ]; do sleep 0.02; done`, filepath.Join(h.root, name))
}

func (h *rig) touch(name string) {
	h.t.Helper()
	if err := os.WriteFile(filepath.Join(h.root, name), nil, 0o600); err != nil {
		h.t.Fatal(err)
	}
}

func (h *rig) read(name string) string {
	b, _ := os.ReadFile(filepath.Join(h.root, name))
	return string(b)
}
