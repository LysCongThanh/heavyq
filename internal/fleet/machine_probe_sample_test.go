package fleet

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
)

// startSpinner starts a bash busy loop of its own process group (the cleanup kills it, stopped or not)
// and returns its pid. body runs after the loop's start time s (µs) is taken.
func startSpinner(t *testing.T, body string) int {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not installed")
	}
	c := exec.Command(bash, "-c", `s=${EPOCHREALTIME/./}; `+body)
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-c.Process.Pid, syscall.SIGKILL); _ = c.Wait() })
	return c.Process.Pid
}

// procCPU is the %CPU the probe gave pid (-1 when it is not in the table).
func procCPU(p Probe, pid int) float64 {
	for _, pr := range p.Procs {
		if pr.Pid == pid {
			return pr.CPU
		}
	}
	return -1
}

// The sample's seconds are those between its two reads of the process table, not those around its sleep:
// on a loaded machine starting the second read takes seconds of its own, and a sample timed around the
// sleep alone charged a one-core loop several cores. Here every cat waits 1 s before it reads (a fork and
// exec that slow): a single-threaded busy loop still reads at most one core.
func TestProbeSampleIsTimedByItsReads(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the sample reads /proc")
	}
	cat, err := exec.LookPath("cat")
	if err != nil {
		t.Skip("cat not installed")
	}
	shim := t.TempDir()
	if err := os.WriteFile(filepath.Join(shim, "cat"), []byte("#!/bin/sh\nsleep 1\nexec "+cat+" \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shim+string(os.PathListSeparator)+os.Getenv("PATH"))
	spin := startSpinner(t, `while :; do :; done`)
	busy0 := statCPU(t, spin)
	p, err := RunProbe(ProbeRequest{Transport: localTransport, Timeout: 30 * time.Second, TopArgs: 4})
	if err != nil || p.Partial != "" {
		t.Fatalf("probe: %v, partial %q", err, p.Partial)
	}
	pct, used := procCPU(p, spin), statCPU(t, spin)-busy0
	t.Logf("busy loop: %.1f%% over a %.2f s sample (it used %v of CPU during the probe)", pct, p.intervalNS/1e9, used)
	if pct > 120 {
		t.Fatalf("a one-thread busy loop read %.1f%%: the sample's seconds miss the time its reads took", pct)
	}
	if pct < 0 {
		t.Fatal("the busy loop is not in the process table")
	}
}

// A read of the process table takes time of its own, and not the same twice: here the first read is
// quick and the second waits 2 ms before each file, seconds in all. Each process is timed between its own
// two reads, so a one-thread busy loop still reads one core wherever its stat file falls in the reads.
func TestProbeSampleTimesEachProcessByItsOwnReads(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the sample reads /proc")
	}
	cat, err1 := exec.LookPath("cat")
	bash, err2 := exec.LookPath("bash")
	if err1 != nil || err2 != nil {
		t.Skip("cat or bash not installed")
	}
	shim := t.TempDir()
	tick := filepath.Join(shim, "tick")
	if err := syscall.Mkfifo(tick, 0o600); err != nil {
		t.Fatal(err)
	}
	// the first cat is the real one; the second reads file by file, a 2 ms wait (a read of a fifo nobody
	// writes) before each, and starts no process
	body := "#!" + bash + "\nm=" + filepath.Join(shim, "second") + "\n[ -e \"$m\" ] || { : > \"$m\"; exec " + cat + " \"$@\"; }\n" +
		"exec 3<> " + tick + "\nfor f; do read -r -t 0.002 -u 3; while IFS= read -r l; do printf '%s\\n' \"$l\"; done < \"$f\"; done\n"
	if err := os.WriteFile(filepath.Join(shim, "cat"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shim+string(os.PathListSeparator)+os.Getenv("PATH"))
	spin := startSpinner(t, `while :; do :; done`)
	p, err := RunProbe(ProbeRequest{Transport: localTransport, Timeout: 40 * time.Second, TopArgs: 4})
	if err != nil || p.Partial != "" {
		t.Fatalf("probe: %v, partial %q", err, p.Partial)
	}
	pct := procCPU(p, spin)
	t.Logf("busy loop: %.1f%% over a %.2f s sample of %d processes", pct, p.intervalNS/1e9, len(p.Procs))
	if pct < 0 || pct > 120 {
		t.Fatalf("a one-thread busy loop read %.1f%%: it was not timed between its own reads", pct)
	}
}

// A task that ran during the sample and then stopped (SIGSTOP, a frozen cgroup on v1, or D) has a tick
// delta, so it is among the busiest, and its stat says T: its command line is never read, however many
// command lines the probe reads. The loop spins 0.9 s from just before the probe, so it runs across the
// sample's first read and is stopped by its second (at least 1 s later).
func TestProbeNeverReadsTheCommandLineOfAStoppedTask(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the sample reads /proc")
	}
	spin := startSpinner(t, `while (( ${EPOCHREALTIME/./} - s < 900000 )); do :; done; kill -STOP $$`)
	p, err := RunProbe(ProbeRequest{Transport: localTransport, Timeout: 20 * time.Second, TopArgs: 1000})
	if err != nil || p.Partial != "" {
		t.Fatalf("probe: %v, partial %q", err, p.Partial)
	}
	if st := procState(spin); st != "T" {
		t.Fatalf("the loop is %q after the probe, not stopped", st)
	}
	if p.ticks[spin] <= 0 {
		t.Skip("the probe's first read came after the loop had stopped (a slow start): it had no tick delta")
	}
	if a, ok := p.Args[spin]; ok {
		t.Fatalf("the command line of stopped pid %d was read: %q", spin, a)
	}
}
