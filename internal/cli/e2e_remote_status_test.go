package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/slots"
)

// remoteConfig adds a "remote" reached through `sh -c` (no ssh needed).
func remoteConfig(h *rig, extra string) string {
	work := filepath.Join(h.root, "remote-work")
	return baseConfig + `[[remote]]
name = "box"
transport = ["sh", "-c"]
work_dir = "` + work + `"
git_url = "` + filepath.Join(work, "git") + `"
heavy_slots = 1
` + extra
}

// gitRepo makes a clean committed repo "app" under h.root and cds into it.
func gitRepo(t *testing.T, h *rig) string {
	t.Helper()
	repo := filepath.Join(h.root, "app")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"config", "commit.gpgsign", "false"}} {
		gitIn(t, h, repo, args...)
	}
	if err := os.WriteFile(filepath.Join(repo, "marker.txt"), []byte("from-commit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, h, repo, "add", ".")
	gitIn(t, h, repo, "commit", "-q", "-m", "init")
	h.cwd = repo
	return repo
}

func gitIn(t *testing.T, h *rig, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = h.env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestRemoteRunByCommit(t *testing.T) {
	h := newRig(t, "")
	h.setConfig(remoteConfig(h, ""))
	repo := gitRepo(t, h)
	// an uncommitted edit must not travel to the remote
	if err := os.WriteFile(filepath.Join(repo, "marker.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rc, out := h.run("3", "--remote", "box", "--lane", "api", "--", "sh", "-c", "cat marker.txt; exit 5")
	if rc != 5 || !strings.Contains(out, "from-commit") || strings.Contains(out, "dirty") {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
	if hist := h.read("history.jsonl"); !strings.Contains(hist, `"machine":"box"`) {
		t.Fatalf("history: %s", hist)
	}
	// dirty repo + default placement => stays local, runs the dirty tree in place
	rc, out = h.run("3", "--", "cat", "marker.txt")
	if rc != 0 || !strings.Contains(out, "dirty") || !strings.Contains(out, "local/") {
		t.Fatalf("dirty repo must stay local: rc=%d\n%s", rc, out)
	}
}

func TestAutoPlacementOffloadsOnlyWhenSafe(t *testing.T) {
	h := newRig(t, "")
	h.setConfig(remoteConfig(h, ""))
	gitRepo(t, h)
	hold := h.start("5", "--remote", "local", "--", "sh", "-c", h.gateFile("go"))
	h.running(1)
	// clean repo, command inside the repo: placed on the free remote
	rc, out := h.run("3", "--", "cat", "marker.txt")
	if rc != 0 || !strings.Contains(out, "box/h1") || !strings.Contains(out, "placed on box") {
		t.Fatalf("expected auto offload: rc=%d\n%s", rc, out)
	}
	// a path outside the repo pins the ticket to local, which is busy
	rc, out = h.run("3", "--max-wait", "500ms", "--", "cat", "../config.toml")
	if rc != ExitMaxWait {
		t.Fatalf("outside-repo command must stay local: rc=%d\n%s", rc, out)
	}
	h.touch("go")
	hold.wait(t, 20*time.Second)
}

func TestRemoteUnreachableExits76(t *testing.T) {
	h := newRig(t, `[queue]
poll = "50ms"
remote_down_max = "1s"
[[remote]]
name = "gone"
transport = ["false"]
git_url = "/nonexistent"
`)
	gitRepo(t, h)
	if rc, out := h.run("3", "--remote", "gone", "--", "true"); rc != ExitRemoteDown {
		t.Fatalf("rc=%d want 76\n%s", rc, out)
	}
}

func TestStatusExplainsWaitAndAntiStarvation(t *testing.T) {
	h := newRig(t, `[queue]
poll = "50ms"
force_after = "2s"
[local]
heavy_slots = 2
light_slots = 0
job_mb = 100000000
`)
	hold := h.start("5", "--lane", "api", "--", "sh", "-c", h.gateFile("go"))
	h.running(1) // an idle machine always admits the first job
	start := time.Now()
	starved := h.start("2", "--lane", "web", "--", "echo", "got-in")
	h.waiting(1)
	rc, out := h.run("status")
	for _, want := range []string{"RUNNING", "api", "WAITING", "web", "HEAD TICKET web", "RAM", "load by lane", "heavy-1 api"} {
		if rc != 0 || !strings.Contains(out, want) {
			t.Fatalf("status missing %q (rc=%d):\n%s", want, rc, out)
		}
	}
	if rc := starved.wait(t, 20*time.Second); rc != 0 || time.Since(start) < 2*time.Second {
		t.Fatalf("anti-starvation: rc=%d after %s", rc, time.Since(start))
	}
	h.touch("go")
	hold.wait(t, 20*time.Second)
	if rc, out := h.run("history"); rc != 0 || !strings.Contains(out, "web") {
		t.Fatalf("history rc=%d\n%s", rc, out)
	}
}

func (h *rig) setConfig(body string) {
	h.t.Helper()
	if err := os.WriteFile(filepath.Join(h.root, "config.toml"), []byte(body), 0o600); err != nil {
		h.t.Fatal(err)
	}
}

// newExternal starts a non-heavyq process (e.g. flock(1)) tracked like a proc.
func newExternal(t *testing.T, h *rig, name string, args ...string) *proc {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir, cmd.Env = h.root, h.env
	p := &proc{cmd: cmd, out: &lockedBuf{}, done: make(chan int, 1)}
	cmd.Stdout, cmd.Stderr = p.out, p.out
	if err := cmd.Start(); err != nil {
		t.Skipf("%s unavailable: %v", name, err)
	}
	go func() { p.done <- processExit(cmd.Wait()) }()
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	return p
}

func isLocked(path string) bool { return slots.Busy(path) }

func killPidFile(path string) {
	if pid := atoiTrim(readFile(path)); pid > 0 {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}

func readFile(p string) string {
	b, _ := os.ReadFile(p)
	return string(b)
}

func atoiTrim(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}
