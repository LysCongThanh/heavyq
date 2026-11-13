package fleet

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A queue whose slot locks are plain files in a shared directory (/tmp/<queue>-N.lock beside other
// programs' locks) is probed through its glob: only its own files are slots, a flock-held one is busy with
// its holder, and a symbolic link is never followed out of the directory.
func TestProbeReadsSlotGlobsInASharedDirectory(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("slot holders come from /proc/locks")
	}
	if _, err := exec.LookPath("flock"); err != nil {
		t.Skip("flock(1) not installed")
	}
	home := t.TempDir()
	t.Setenv("HOME", home) // the "~/" glob resolves on the probed machine: this one, through sh -c
	shared := filepath.Join(home, "tmp")
	outside := t.TempDir()
	if err := os.MkdirAll(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"q-1.lock", "q-2.lock", "q-12.lock", "q-l1.lock", "q-slot-1.lock", "other-1.lock", "q-x.lock"} {
		if err := os.WriteFile(filepath.Join(shared, f), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	held := filepath.Join(outside, "elsewhere.lock")
	if err := os.WriteFile(held, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(held, filepath.Join(shared, "q-3.lock")); err != nil {
		t.Fatal(err)
	}
	var holders []*exec.Cmd
	for _, f := range []string{filepath.Join(shared, "q-2.lock"), filepath.Join(shared, "other-1.lock"), held} {
		c := exec.Command("flock", f, "sleep", "30")
		c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = syscall.Kill(-c.Process.Pid, syscall.SIGKILL); _ = c.Wait() })
		holders = append(holders, c)
	}
	time.Sleep(300 * time.Millisecond) // let flock take the locks

	p, err := RunProbe(ProbeRequest{Transport: localTransport, Timeout: 20 * time.Second,
		Slots: []SlotDir{SlotGlob("bashq", "~/tmp/q-[0-9]*.lock"), SlotGlob("lone", shared+"/other-*.lock")}})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range p.Slots {
		state := "free"
		if s.Busy {
			state = "busy"
		}
		got = append(got, s.Owner+" "+s.Name+" "+state)
		if s.Busy && s.Name == "q-2" && s.Holder != holders[0].Process.Pid {
			t.Errorf("q-2 holder %d, want %d", s.Holder, holders[0].Process.Pid)
		}
	}
	sort.Strings(got)
	want := "bashq q-1 free,bashq q-12 free,bashq q-2 busy,lone other-1 busy"
	if strings.Join(got, ",") != want {
		t.Fatalf("slots %v\nwant %s (q-3 is a link, q-l1, q-slot-1 and q-x are not the queue's)", got, want)
	}
}

func TestSlotGlobSplitsDirectoryAndBaseName(t *testing.T) {
	for glob, want := range map[string]SlotDir{
		"/tmp/q-[0-9]*.lock": {Owner: "q", Dir: "/tmp", Pattern: "q-[0-9]*.lock"},
		"~/q-*.lock":         {Owner: "q", Dir: "~/", Pattern: "q-*.lock"},
		"~/.q/s*":            {Owner: "q", Dir: "~/.q", Pattern: "s*"},
		"/q-*":               {Owner: "q", Dir: "/", Pattern: "q-*"},
	} {
		if got := SlotGlob("q", glob); got != want {
			t.Errorf("%q: %+v, want %+v", glob, got, want)
		}
	}
}
