package statusline

import (
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/procinfo"
)

// A status-line command past its time limit is killed with everything it
// started, not only the shell: a script whose child is blocked and silent
// (git on a slow mount, a cold npx) would otherwise leave that child running
// after every slow refresh, and Claude Code refreshes often.
func TestWrapTimeoutKillsTheWholeCommandNotJustTheShell(t *testing.T) {
	old := wrapTimeout
	wrapTimeout = 300 * time.Millisecond
	t.Cleanup(func() { wrapTimeout = old })

	dir := t.TempDir()
	// The child writes nothing, so a closed stdout pipe never kills it.
	command := `sleep 30 >/dev/null 2>&1 & echo $! > child.pid; echo partial; wait`
	start := time.Now()
	out, _ := runWrapped(command, nil, dir, io.Discard)
	if took := time.Since(start); took > wrapTimeout+2*time.Second {
		t.Fatalf("runWrapped took %v past a %v limit", took, wrapTimeout)
	}
	if out != "partial" {
		t.Errorf("output before the timeout: got %q, want %q", out, "partial")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "child.pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 {
		t.Fatalf("child pid %q: %v", raw, err)
	}
	// Never leave the sleeper behind, whatever the outcome.
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })

	deadline := time.Now().Add(3 * time.Second)
	for procinfo.Default.Running(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("child %d of the timed-out command still runs", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
