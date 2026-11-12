package remote

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The tool probe runs the job script's own PATH block, so what it finds is what a job there finds.
func TestProbeToolsSeesThePathAJobGets(t *testing.T) {
	m := fakeRemote(t)
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "hq-probe-only-here"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	m.EnvPath = bin
	tools := []string{"hq-probe-only-here", "sh", "hq-probe-nowhere"}
	got, err := ProbeTools(m, tools, 20*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got["hq-probe-only-here"] != filepath.Join(bin, "hq-probe-only-here") || got["sh"] == "" {
		t.Fatalf("with env_path: %q", got)
	}
	if p, ok := got["hq-probe-nowhere"]; !ok || p != "" {
		t.Fatalf("a missing tool is answered with \"\": %q", got)
	}
	m.EnvPath = ""
	if got, err = ProbeTools(m, tools, 20*time.Second); err != nil || got["hq-probe-only-here"] != "" {
		t.Fatalf("without env_path: %q, %v", got, err)
	}
	if _, err := ProbeTools(m, []string{"../x"}, time.Second); err == nil {
		t.Fatal("a tool name that is not a plain command name must be refused")
	}
	m.Transport = []string{"false"}
	if _, err := ProbeTools(m, []string{"go"}, 5*time.Second); err == nil {
		t.Fatal("an unreachable remote is an error, not an answer")
	}
}

// A file on PATH that is not executable is not a tool a job can run: bash's `command -v` names it, the probe
// must not. A builtin has no path and is found.
func TestProbeToolsFindsOnlyWhatARunCanExecute(t *testing.T) {
	m := fakeRemote(t)
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "hq-probe-plain-file"), []byte("#!/bin/sh\nexit 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(bin, "hq-probe-a-directory"), 0o755); err != nil {
		t.Fatal(err)
	}
	m.EnvPath = bin
	got, err := ProbeTools(m, []string{"hq-probe-plain-file", "hq-probe-a-directory", "cd"}, 20*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"hq-probe-plain-file", "hq-probe-a-directory"} {
		if p, ok := got[name]; !ok || p != "" {
			t.Fatalf("%s is not executable: want the answer \"\", got %q", name, got)
		}
	}
	if got["cd"] == "" {
		t.Fatalf("a builtin is found (a job's bash -c has it): %q", got)
	}
}

func TestJobPathBlockIsCutFromTheJobScript(t *testing.T) {
	if !strings.Contains(jobPathScript, "HEAVYQ_ENV_PATH") || !strings.Contains(jobPathScript, "export PATH") {
		t.Fatalf("the PATH block of remote_job.sh was not found:\n%s", jobPathScript)
	}
	if !strings.Contains(jobScript, jobPathScript) {
		t.Fatal("the probe's PATH block must be the job script's own text")
	}
}
