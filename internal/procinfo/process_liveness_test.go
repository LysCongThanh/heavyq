package procinfo

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// A ticket's start token is written by one heavyq and checked by every
// other one (waiters' rounds, status, the dashboard). On the ps source the
// token is a wall-clock date, so it must not depend on the TZ of whoever
// reads it, or a checker in another zone prunes a live ticket.
func TestPSStartTokenIgnoresCallerTimeZone(t *testing.T) {
	p := psSource(t)
	// POSIX zone strings: no tzdata needed, nine hours apart.
	t.Setenv("TZ", "UTC0")
	utc, err := p.StartTime(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TZ", "JST-9")
	east, err := p.StartTime(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if utc != east {
		t.Fatalf("start token depends on TZ: %q under UTC, %q nine hours east", utc, east)
	}
	if !p.Alive(os.Getpid(), utc) {
		t.Fatal("a token written under another TZ must still match this live process")
	}
}

// When kill(pid, 0) has seen the pid but ps cannot answer (not runnable,
// killed by its timeout or a signal), Alive does not know, and an unknown
// answer must keep the ticket: deleting it would drop a live waiter or let
// a second job into a slot that is in use.
func TestPSAliveKeepsLiveProcessWhenPSCannotAnswer(t *testing.T) {
	p := psSource(t)
	st, err := p.StartTime(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	reaped := exec.Command("true")
	if err := reaped.Run(); err != nil {
		t.Fatal(err)
	}

	killed := t.TempDir() // a ps that dies on a signal, as one killed by its timeout does
	script := "#!/bin/sh\nkill -KILL $$\n"
	if err := os.WriteFile(filepath.Join(killed, "ps"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{"ps not runnable": t.TempDir(), "ps killed": killed} {
		t.Setenv("PATH", path)
		if !p.Alive(os.Getpid(), st) {
			t.Errorf("%s: a live process must not be declared dead", name)
		}
		if p.Alive(reaped.Process.Pid, st) {
			t.Errorf("%s: a pid kill(2) cannot see is dead whatever ps does", name)
		}
	}
}

// A zombie is a process that has exited: its parent has not reaped it yet,
// but it runs nothing and holds no locks. Its ticket must be pruned.
func TestAliveRejectsZombie(t *testing.T) {
	sources := map[string]ProcFS{"default": Default}
	if ps := (ProcFS{UsePS: true}); ps.Available() {
		sources["ps"] = ps
	}
	for name, p := range sources {
		cmd := exec.Command("true")
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		pid := cmd.Process.Pid
		st, err := p.StartTime(pid) // readable whether it still runs or is already a zombie
		if err != nil {
			_ = cmd.Wait()
			t.Fatalf("%s: StartTime(child) = %v", name, err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for p.Running(pid) { // not waited for: once it exits it stays a zombie
			if time.Now().After(deadline) {
				_ = cmd.Wait()
				t.Fatalf("%s: child never exited", name)
			}
			time.Sleep(20 * time.Millisecond)
		}
		alive := p.Alive(pid, st)
		_ = cmd.Wait()
		if alive {
			t.Errorf("%s: an exited, unreaped child (zombie) must not be alive", name)
		}
	}
}
