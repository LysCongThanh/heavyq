package slots

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// age makes path look untouched for days, as a lock file created long ago
// and only ever opened and flocked since.
func age(t *testing.T, path string, d time.Duration) {
	t.Helper()
	old := time.Now().Add(-d)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
}

// cleanTmp runs a real age-based temp cleaner over dir, the way macOS's
// periodic clean-tmps or tmpreaper do: it deletes files untouched for more
// than 3 days and does not care about flocks.
func cleanTmp(t *testing.T, dir string) {
	t.Helper()
	out, err := exec.Command("find", dir, "-type", "f", "-atime", "+3", "-mtime", "+3", "-delete").CombinedOutput()
	if err != nil {
		t.Fatalf("find: %v %s", err, out)
	}
}

// A held slot whose lock file is days old must survive the cleaner, or the
// next run creates a fresh file, locks it and shares the slot.
func TestHeldSlotSurvivesAnAgeBasedTempCleaner(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "slots", "heavy-1.lock")
	first, err := TryLock(p)
	if err != nil || first == nil {
		t.Fatalf("create: %v %v", first, err)
	}
	first.Release()
	age(t, p, 10*24*time.Hour)

	held, err := TryLock(p)
	if err != nil || held == nil {
		t.Fatalf("lock: %v %v", held, err)
	}
	defer held.Release()
	cleanTmp(t, dir)
	if second, _ := TryLock(p); second != nil {
		second.Release()
		t.Fatal("two holders in one slot: the cleaner deleted the held lock file")
	}
}

// A job holding a slot for longer than the cleaner's age must keep its lock
// file fresh the whole time, not only when it took it.
func TestHeldLockStaysFreshWhileHeld(t *testing.T) {
	defer func(d time.Duration) { refreshEvery = d }(refreshEvery)
	refreshEvery = 20 * time.Millisecond
	p := filepath.Join(t.TempDir(), "heavy-1.lock")
	l, err := TryLock(p)
	if err != nil || l == nil {
		t.Fatalf("lock: %v %v", l, err)
	}
	defer l.Release()
	age(t, p, 10*24*time.Hour)
	deadline := time.Now().Add(5 * time.Second)
	for {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if time.Since(fi.ModTime()) < time.Hour {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("held lock file still dated %s", fi.ModTime())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A cleaner that unlinks a free lock file between our open and our flock
// leaves us locking an orphan inode. The lock must notice and lock the file
// that is at the path, or the next caller takes that one too.
func TestTryLockRechecksTheLockedFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "heavy-1.lock")
	unlinkOnce(t, p)
	a, err := TryLock(p)
	if err != nil || a == nil {
		t.Fatalf("lock: %v %v", a, err)
	}
	defer a.Release()
	if b, _ := TryLock(p); b != nil {
		b.Release()
		t.Fatal("two holders: the first lock is on an unlinked file")
	}
}

// unlinkOnce removes path the first time a lock has opened it and is about
// to flock it, as an outside cleaner would.
func unlinkOnce(t *testing.T, path string) {
	t.Helper()
	done := false
	beforeFlock = func(p string) {
		if p == path && !done {
			done = true
			if err := os.Remove(p); err != nil {
				t.Error(err)
			}
		}
	}
	t.Cleanup(func() { beforeFlock = func(string) {} })
}
