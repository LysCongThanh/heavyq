package remote

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/slots"
)

// The Darwin branches of the probe and job scripts, exercised on any machine
// by putting `uname`, `sysctl` and `vm_stat` stubs first on PATH. The stubs
// replay the verbatim output shapes macOS prints (also the fixtures of
// internal/metrics); the real Mac is covered by HEAVYQ_TEST_MAC_REMOTE.

const darwinVMStat = `Mach Virtual Memory Statistics: (page size of 16384 bytes)
Pages free:                                5061.
Pages active:                            210522.
Pages inactive:                          199893.
Pages speculative:                         4312.
Pages purgeable:                           9731.
"Translation faults":                 903441215.
Pageouts:                                 11823.`

func darwinStubs(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("perl"); err != nil {
		t.Skip("perl is required for the macOS probe")
	}
	stubDir(t, map[string]string{
		"uname":   `echo Darwin`,
		"sysctl":  `printf '17179869184\n8\n{ 2.34 2.10 1.98 }\n'`,
		"vm_stat": "cat <<'EOF'\n" + darwinVMStat + "\nEOF",
	})
}

func TestDarwinProbeReadsSysctlVMStatAndPerlLocks(t *testing.T) {
	darwinStubs(t)
	m := fakeRemote(t)
	if err := os.MkdirAll(filepath.Join(m.WorkDir, "slots"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"h1", "l1"} { // a lock file that exists but is free is not busy
		if err := os.WriteFile(filepath.Join(m.WorkDir, "slots", s+".lock"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	l, err := slots.TryLock(filepath.Join(m.WorkDir, "slots", "h1.lock"))
	if err != nil || l == nil {
		t.Fatal("cannot hold remote slot", err)
	}
	defer l.Release()
	res, err := Probe(m, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	got := res.Metrics
	if !got.Known || got.TotalMB != 16384 || got.FreeMB != 3421 || got.Cores != 8 || got.Load1 != 2.34 {
		t.Fatalf("metrics = %+v", got)
	}
	if !res.Busy["h1"] || res.Busy["l1"] || len(res.Busy) != 1 {
		t.Fatalf("busy = %v, want only h1", res.Busy)
	}
}

func TestDarwinProbeWithBrokenToolsDegradesToSlotsOnly(t *testing.T) {
	stubDir(t, map[string]string{"uname": `echo Darwin`, "sysctl": `exit 1`, "vm_stat": `exit 1`})
	if _, err := exec.LookPath("perl"); err != nil {
		t.Skip("perl is required for the macOS probe")
	}
	res, err := Probe(fakeRemote(t), 10*time.Second)
	if err != nil || res.Metrics.Known || res.Metrics.Cores != 1 || len(res.Busy) != 0 {
		t.Fatalf("got %+v, %v", res, err)
	}
}

func TestParseProbeDarwinOutput(t *testing.T) {
	out := "noise\nheavyq-probe-darwin h1,l1\n17179869184\n8\n{ 0.50 0.40 0.30 }\n" + darwinVMStat + "\n"
	r, err := parseProbe(out)
	if err != nil || !r.Metrics.Known || r.Metrics.Cores != 8 || !r.Busy["h1"] || !r.Busy["l1"] {
		t.Fatalf("got %+v, %v", r, err)
	}
	if r, err = parseProbe("heavyq-probe-darwin -\n"); err != nil || r.Metrics.Known || len(r.Busy) != 0 {
		t.Fatalf("truncated output must give unknown metrics: %+v, %v", r, err)
	}
}

// printPath runs `echo $PATH` as the remote command and returns it.
func printPath(t *testing.T, spec JobSpec) string {
	t.Helper()
	spec.Argv = []string{"sh", "-c", "echo $PATH"}
	rc, out, errs := runJob(t, spec)
	if rc != 0 {
		t.Fatalf("rc=%d\n%s\n%s", rc, out, errs)
	}
	return strings.TrimSpace(out)
}

func TestJobEnvPathGoesFirstAndExpandsHome(t *testing.T) {
	repo, sha := pushApp(t)
	m := fakeRemote(t)
	if err := Push(m, repo, "app", sha, sha[:12], 20*time.Second); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	m.EnvPath = "~/go/bin::/opt/extra/bin"
	got := printPath(t, JobSpec{Machine: m, Repo: "app", SHA: sha, Lane: "x", Slot: "h1"})
	if !strings.HasPrefix(got, home+"/go/bin:/opt/extra/bin:") || strings.Contains(got, "::") {
		t.Fatalf("PATH = %q", got)
	}
}

func TestJobAddsLoginShellPathOnDarwin(t *testing.T) {
	repo, sha := pushApp(t)
	m := fakeRemote(t)
	if err := Push(m, repo, "app", sha, sha[:12], 20*time.Second); err != nil {
		t.Fatal(err)
	}
	darwinStubs(t)
	home := t.TempDir()
	// noise on stdout must not confuse the PATH capture
	profile := "echo profile noise\nexport PATH=/heavyq-login-test/bin:$PATH\n"
	if err := os.WriteFile(filepath.Join(home, ".bash_profile"), []byte(profile), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/bash")
	m.EnvPath = "/first/bin"
	got := printPath(t, JobSpec{Machine: m, Repo: "app", SHA: sha, Lane: "x", Slot: "h1"})
	login := strings.Index(got, "/heavyq-login-test/bin")
	if !strings.HasPrefix(got, "/first/bin:") || login < 0 || strings.Contains(got, "profile noise") {
		t.Fatalf("PATH = %q", got)
	}
}
