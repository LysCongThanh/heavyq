package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// hook.binary is shell-quoted into every rewritten command, so a "~/" the
// shell would have expanded becomes a literal path and every rewritten agent
// command exits 127. "~/" is expanded at load; values that cannot work as
// one quoted word are refused.
func TestHookBinaryTildeIsExpandedAndRuns(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	bin := filepath.Join(home, ".local", "bin", "heavyq")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	c, err := Load(writeConfig(t, "[hook]\nbinary = \"~/.local/bin/heavyq\"\n"), true)
	if err != nil {
		t.Fatal(err)
	}
	if c.Hook.Binary != bin {
		t.Fatalf("binary = %q, want %q", c.Hook.Binary, bin)
	}
	// The hook writes the binary single-quoted, as one word.
	if out, err := exec.Command("sh", "-c", "'"+c.Hook.Binary+"' 4 -- true").CombinedOutput(); err != nil {
		t.Fatalf("rewritten command fails: %v\n%s", err, out)
	}
}

func TestHookBinaryShapes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, ok := range []string{"heavyq", "/opt/my tools/heavyq", "/usr/local/bin/heavyq"} {
		if _, err := Load(writeConfig(t, "[hook]\nbinary = \""+ok+"\"\n"), true); err != nil {
			t.Errorf("binary %q refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"heavyq --config /x", "bin/heavyq", "./heavyq", "~other/heavyq", "/opt/heavyq\\t"} {
		_, err := Load(writeConfig(t, "[hook]\nbinary = \""+bad+"\"\n"), true)
		if err == nil || !strings.Contains(err.Error(), "hook.binary") {
			t.Errorf("binary %q: err = %v", bad, err)
		}
	}
}
