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

	"github.com/LysCongThanh/heavyq/internal/queue"
)

// These tests run the real heavyq binary as separate processes, with real
// flocks and real child processes. Each test gets its own queue dir.

var binPath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "heavyq-bin")
	if err != nil {
		panic(err)
	}
	binPath = filepath.Join(dir, "heavyq")
	if out, err := exec.Command("go", "build", "-race", "-o", binPath, "../..").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build heavyq: %v\n%s", err, out)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

const baseConfig = `[queue]
poll = "50ms"
resource_gate = false
[local]
heavy_slots = 1
light_slots = 1
`

type rig struct {
	t    *testing.T
	root string // scratch dir; also the default cwd
	cwd  string
	env  []string
}

func newRig(t *testing.T, config string) *rig {
	t.Helper()
	root := t.TempDir()
	cfg := filepath.Join(root, "config.toml")
	if err := os.WriteFile(cfg, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "HEAVYQ_") && !strings.HasPrefix(e, "XDG_CACHE_HOME=") && !strings.HasPrefix(e, "CLAUDE_CONFIG_DIR=") {
			env = append(env, e)
		}
	}
	env = append(env, "HEAVYQ_DIR="+filepath.Join(root, "q"), "HEAVYQ_CONFIG="+cfg,
		"HEAVYQ_HISTORY="+filepath.Join(root, "history.jsonl"), "XDG_CACHE_HOME="+filepath.Join(root, "cache"),
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
