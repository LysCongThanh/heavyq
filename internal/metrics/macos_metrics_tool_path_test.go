package metrics

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// toolPath must find a tool in its install directory even when PATH does not
// list that directory, and the resolved path must really run.
func TestToolPathFindsToolOutsidePath(t *testing.T) {
	sbin := t.TempDir()
	tool := filepath.Join(sbin, "fake-sysctl")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\necho measured\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir()) // an empty directory: no PATH lookup can succeed

	got := toolPath("fake-sysctl", []string{t.TempDir(), sbin})
	if got != tool {
		t.Fatalf("toolPath = %q, want %q", got, tool)
	}
	out, err := run(got)
	if err != nil || strings.TrimSpace(out) != "measured" {
		t.Fatalf("run(%q) = %q, %v", got, out, err)
	}
	if _, err := run("fake-sysctl"); err == nil {
		t.Fatal("the bare name must not resolve under this PATH")
	}
}

// Anything that is not an executable regular file is skipped, and with no
// candidate left the bare name is returned so PATH still gets its say.
func TestToolPathSkipsNonExecutablesAndFallsBackToName(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plain"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "adir"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"plain", "adir", "missing"} {
		if got := toolPath(name, []string{dir}); got != name {
			t.Errorf("toolPath(%q) = %q, want the bare name", name, got)
		}
	}
}
