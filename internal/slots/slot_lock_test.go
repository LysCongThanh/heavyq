package slots

import (
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestTryLockExclusive(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s", "heavy-1.lock")
	a, err := TryLock(p)
	if err != nil || a == nil {
		t.Fatalf("first lock: %v %v", a, err)
	}
	b, err := TryLock(p)
	if err != nil || b != nil {
		t.Fatalf("second lock must report busy, got %v %v", b, err)
	}
	if !Busy(p) {
		t.Fatal("Busy must see the held lock")
	}
	a.Release()
	a.Release() // idempotent
	if Busy(p) {
		t.Fatal("released lock still busy")
	}
}

// A legacy `flock <file> cmd` user and heavyq must exclude each other.
func TestInteropWithFlockCommand(t *testing.T) {
	if _, err := exec.LookPath("flock"); err != nil {
		t.Skip("flock(1) not installed")
	}
	p := filepath.Join(t.TempDir(), "legacy.lock")
	cmd := exec.Command("flock", p, "sleep", "2")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	deadline := time.Now().Add(2 * time.Second)
	for !Busy(p) {
		if time.Now().After(deadline) {
			t.Fatal("flock(1) never took the lock")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if l, _ := TryLock(p); l != nil {
		t.Fatal("heavyq took a slot held by flock(1)")
	}
	l, err := TryLock(filepath.Join(t.TempDir(), "x.lock"))
	if err != nil || l == nil {
		t.Fatal(err)
	}
	defer l.Release()
	if err := exec.Command("flock", "-n", l.Path, "true").Run(); err == nil {
		t.Fatal("flock(1) took a slot held by heavyq")
	}
}
