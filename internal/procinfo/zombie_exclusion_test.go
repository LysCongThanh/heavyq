package procinfo

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Real zombies (children that exited and that nothing reaps until the cleanup) are in no group of a
// breakdown and in no tree, on every process source of this machine: they use no CPU and no memory.
func TestZombiesAreNoProcessesToCount(t *testing.T) {
	bin, err := exec.LookPath("true")
	if err != nil {
		t.Skip("true(1) not installed")
	}
	link := filepath.Join(t.TempDir(), "hq-zombie")
	if err := os.Symlink(bin, link); err != nil {
		t.Fatal(err)
	}
	var pids []int
	for range 20 {
		c := exec.Command(link)
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Wait() })
		pids = append(pids, c.Process.Pid)
	}
	for name, p := range surveySources(t) {
		for _, pid := range pids { // wait until each has exited: Survey leaves out a zombie as a gone pid
			for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(5 * time.Millisecond) {
				if live, ok := p.Survey([]int{pid}); ok && len(live) == 0 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("%s: pid %d never exited", name, pid)
				}
			}
		}
		_, outside := p.Breakdown(nil, 50*time.Millisecond)
		for _, g := range outside {
			if g.Group == "hq-zombie" || g.Group == "(hq-zombie)" {
				t.Errorf("%s: %d zombies counted as a group: %+v", name, g.Procs, g)
			}
		}
		if d := p.Descendants(os.Getpid()); containsAny(d, pids) {
			t.Errorf("%s: zombies listed as descendants: %v", name, d)
		}
	}
}

func containsAny(list, want []int) bool {
	set := map[int]bool{}
	for _, w := range want {
		set[w] = true
	}
	for _, v := range list {
		if set[v] {
			return true
		}
	}
	return false
}
