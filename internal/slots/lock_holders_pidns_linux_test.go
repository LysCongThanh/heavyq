//go:build linux

package slots

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const pidNSHelperEnv = "HEAVYQ_SLOTS_PIDNS_HELPER"

// TestHeldByAnyonePidNSHelper runs only as the child of the test below,
// inside its own pid namespace, and prints what it sees for the file.
func TestHeldByAnyonePidNSHelper(t *testing.T) {
	p := os.Getenv(pidNSHelperEnv)
	if p == "" {
		t.Skip("helper for TestHeldByAnyoneInAnotherPidNamespace")
	}
	busy, known := heldByAnyone(p)
	fmt.Printf("RESULT busy=%v known=%v Busy=%v\n", busy, known, Busy(p))
}

// /proc/locks hides locks whose owner is not visible in the pid namespace of
// the /proc mount. A container or sandbox with its own pid namespace that
// shares the queue dir must still see a slot held from outside as busy,
// never as a certain "free".
func TestHeldByAnyoneInAnotherPidNamespace(t *testing.T) {
	if os.Getenv(pidNSHelperEnv) != "" {
		t.Skip("running as the helper")
	}
	unshare, err := exec.LookPath("unshare")
	if err != nil {
		t.Skip("unshare(1) not installed")
	}
	nsArgs := []string{"--user", "--map-root-user", "--pid", "--fork", "--mount-proc"}
	if out, err := exec.Command(unshare, append(nsArgs, "true")...).CombinedOutput(); err != nil {
		t.Skipf("no unprivileged user+pid namespaces here: %v %s", err, out)
	}
	p := filepath.Join(t.TempDir(), "heavy-1.lock")
	l, err := TryLock(p)
	if err != nil || l == nil {
		t.Fatalf("lock: %v %v", l, err)
	}
	defer l.Release()

	run := func(args ...string) string {
		cmd := exec.Command(unshare, append(nsArgs, args...)...)
		cmd.Env = append(os.Environ(), pidNSHelperEnv+"="+p)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	out := run(os.Args[0], "-test.run=^TestHeldByAnyonePidNSHelper$", "-test.count=1")
	var res string
	for _, line := range strings.Split(out, "\n") {
		if r, ok := strings.CutPrefix(line, "RESULT "); ok {
			res = r
		}
	}
	if res == "" {
		t.Fatalf("helper printed no result:\n%s", out)
	}
	if strings.Contains(res, "busy=false known=true") || !strings.Contains(res, "Busy=true") {
		t.Fatalf("a slot held outside the pid namespace looks free inside it: %s", res)
	}
	if _, err := exec.LookPath("flock"); err == nil {
		// The kernel agrees the file is held there: flock -n fails.
		cmd := exec.Command(unshare, append(nsArgs, "flock", "-n", p, "true")...)
		if cmd.Run() == nil {
			t.Fatal("flock -n took a file this test holds")
		}
	}
}
