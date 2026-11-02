package remote

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
)

// End to end against a real Mac reached over ssh. Skipped unless
// HEAVYQ_TEST_MAC_REMOTE names an ssh alias (key auth) of a Mac with git, Go
// and nothing else installed: no flock, no setsid, no Homebrew.
// HEAVYQ_TEST_MAC_ENV_PATH (default ~/.local/go-sdk/bin) is the remote's env_path.

// macRemote returns a remote with a throwaway work_dir, removed at the end.
func macRemote(t *testing.T) config.MachineConfig {
	t.Helper()
	alias := os.Getenv("HEAVYQ_TEST_MAC_REMOTE")
	if alias == "" {
		t.Skip("set HEAVYQ_TEST_MAC_REMOTE=<ssh alias of a Mac> to run the macOS remote tests")
	}
	work := fmt.Sprintf(".heavyq-test-%d", time.Now().UnixNano()%1_000_000_000)
	envPath := os.Getenv("HEAVYQ_TEST_MAC_ENV_PATH")
	if envPath == "" {
		envPath = "~/.local/go-sdk/bin"
	}
	t.Cleanup(func() { _ = exec.Command("ssh", "-o", "BatchMode=yes", alias, "rm -rf ~/"+work).Run() })
	return config.MachineConfig{Name: alias, HeavySlots: 1, LightSlots: 1, WorkDir: work, EnvPath: envPath,
		Transport: []string{"ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", alias}, GitURL: alias + ":" + work + "/git"}
}

// macShell runs a shell snippet on the Mac and returns its stdout.
func macShell(t *testing.T, m config.MachineConfig, script string) string {
	t.Helper()
	out, err := Transport(m.Transport).Output(30*time.Second, "eval \"$1\"", script)
	if err != nil {
		t.Fatalf("ssh %s: %v", m.Name, err)
	}
	return out
}

// macPushed pushes a tiny Go module whose test passes (or fails) and returns its SHA.
func macPushed(t *testing.T, m config.MachineConfig, failing bool) (string, string) {
	t.Helper()
	repo, _ := newRepo(t, t.TempDir(), "tiny", "go.mod", "module tiny\n\ngo 1.18\n")
	want := "1"
	if failing {
		want = "2"
	}
	src := "package tiny\n\nimport \"testing\"\n\nfunc TestOne(t *testing.T) {\n\tif 1 != " + want + " {\n\t\tt.Fatal(\"one is not " + want + "\")\n\t}\n}\n"
	if err := os.WriteFile(filepath.Join(repo, "tiny_test.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-q", "-m", "test")
	sha := gitRun(t, repo, "rev-parse", "HEAD")
	if err := Push(m, repo, "tiny", sha, sha[:12], 60*time.Second); err != nil {
		t.Fatal(err)
	}
	return repo, sha
}

func TestMacProbe(t *testing.T) {
	m := macRemote(t)
	res, err := Probe(m, 20*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	got := res.Metrics
	if !got.Known || got.TotalMB < 1024 || got.FreeMB <= 0 || got.FreeMB > got.TotalMB || got.Cores < 1 || got.Load1 < 0 || len(res.Busy) != 0 {
		t.Fatalf("probe = %+v busy=%v", got, res.Busy)
	}
	t.Logf("%s: %d cores, load %.2f, %d of %d MiB available", m.Name, got.Cores, got.Load1, got.FreeMB, got.TotalMB)
}

func TestMacRunsGoTestAtTheExactCommit(t *testing.T) {
	m := macRemote(t)
	_, sha := macPushed(t, m, false)
	spec := JobSpec{Machine: m, Repo: "tiny", SHA: sha, Lane: "api", Slot: "h1",
		Argv: []string{"env", "GOTOOLCHAIN=local", "go", "test", "-count=1", "./..."}}
	rc, out, errs := runJob(t, spec)
	if rc != 0 || !strings.Contains(out, "ok") || !strings.Contains(out, "tiny") {
		t.Fatalf("rc=%d\nstdout:%s\nstderr:%s", rc, out, errs)
	}
	if ls := macShell(t, m, "ls ~/"+m.WorkDir+"/wt"); strings.TrimSpace(ls) != "" {
		t.Fatalf("worktrees left behind: %s", ls)
	}
	_, sha = macPushed(t, m, true)
	spec.SHA = sha
	if rc, out, _ := runJob(t, spec); rc != 1 || !strings.Contains(out, "FAIL") {
		t.Fatalf("a failing test must exit 1 and say FAIL: rc=%d\n%s", rc, out)
	}
}

func TestMacSlotBusyAndFreedAfterwards(t *testing.T) {
	m := macRemote(t)
	_, sha := macPushed(t, m, false)
	macShell(t, m, "mkdir -p ~/"+m.WorkDir+"/slots")
	// a holder that is not heavyq: Perl flock on the slot file until its stdin closes
	hold := exec.Command("ssh", "-o", "BatchMode=yes", m.Name,
		"perl -MFcntl=:flock -e "+ShellQuote(`$|=1; open(F,">>",$ARGV[0]) or die; flock(F,LOCK_EX) or die; print "held\n"; <STDIN>`)+" ~/"+m.WorkDir+"/slots/h1.lock")
	stdin, err := hold.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	var held syncBuffer
	hold.Stdout = &held
	if err := hold.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = hold.Process.Kill(); _ = hold.Wait() }()
	deadline := time.Now().Add(20 * time.Second)
	for !strings.Contains(held.String(), "held") && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if !strings.Contains(held.String(), "held") {
		t.Fatal("holder never took the slot")
	}
	res, err := Probe(m, 20*time.Second)
	if err != nil || !res.Busy["h1"] || res.Busy["l1"] {
		t.Fatalf("probe = %+v, %v", res, err)
	}
	start := time.Now()
	rc, _, errs := runJob(t, JobSpec{Machine: m, Repo: "tiny", SHA: sha, Lane: "x", Slot: "h1", Argv: []string{"true"}})
	if rc != ExitSlotBusy || !strings.Contains(errs, "busy") || time.Since(start) > 25*time.Second {
		t.Fatalf("busy slot: rc=%d after %s: %s", rc, time.Since(start), errs)
	}
	stdin.Close() // the holder exits, the slot is free again
	_ = hold.Wait()
	rc, _, errs = runJob(t, JobSpec{Machine: m, Repo: "tiny", SHA: sha, Lane: "x", Slot: "h1", Argv: []string{"true"}})
	if rc != 0 {
		t.Fatalf("slot not free after the holder left: rc=%d %s", rc, errs)
	}
}

// Cancel (heavyq forwards a signal to ssh), heavyq dying (the lifeline pipe
// closes) and a hard disconnect (ssh killed) must all leave nothing on the Mac.
func TestMacTeardownLeavesNoOrphans(t *testing.T) {
	for name, stop := range map[string]func(*Started){
		"cancel":     func(j *Started) { j.Signal(os.Interrupt) },
		"lifeline":   func(j *Started) { j.lifeline.Close() },
		"disconnect": func(j *Started) { j.Signal(os.Kill) },
	} {
		t.Run(name, func(t *testing.T) {
			m := macRemote(t)
			_, sha := macPushed(t, m, false)
			token, pattern := marker()
			var out syncBuffer
			// a background grandchild as well as the foreground sleep: the whole group must go
			j, err := StartJob(JobSpec{Machine: m, Repo: "tiny", SHA: sha, Lane: "x", Slot: "h1",
				Argv: []string{"sh", "-c", "(sleep " + token + " &); echo started; sleep " + token}, Stdout: &out, Stderr: &out})
			if err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(30 * time.Second)
			for !strings.Contains(out.String(), "started") && time.Now().Before(deadline) {
				time.Sleep(100 * time.Millisecond)
			}
			if !strings.Contains(out.String(), "started") {
				j.Signal(os.Kill)
				t.Fatalf("job never started: %s", out.String())
			}
			stop(j)
			done := make(chan int, 1)
			go func() { done <- j.Wait() }()
			select {
			case <-done:
			case <-time.After(30 * time.Second):
				j.Signal(os.Kill)
				t.Fatal("transport did not end")
			}
			jobs := strings.Replace(m.WorkDir, ".heavyq-test", ".heavyq-tes[t]", 1)
			var left string
			for end := time.Now().Add(30 * time.Second); time.Now().Before(end); time.Sleep(500 * time.Millisecond) {
				left = macShell(t, m, "pgrep -fl '"+pattern+"|"+jobs+"'; ls ~/"+m.WorkDir+"/wt; true")
				if strings.TrimSpace(left) == "" {
					return
				}
			}
			t.Fatalf("left on the Mac after %s:\n%s", name, strings.TrimSpace(left))
		})
	}
}
