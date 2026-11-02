package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMissingProgram(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "ok.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "plain.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		cmd  string
		want string // substring of the reason; "" = runnable
	}{
		{"sh -c true", ""},
		{"FOO=1 sh -c true", ""},
		{"./ok.sh --flag", ""},
		{"./missing.sh", "not an executable file"},
		{"./plain.txt", "not an executable file"},
		{"heavyq-no-such-tool-xyz test", "`heavyq-no-such-tool-xyz` is not installed"},
		{"cd sub && make", ""}, // builtin first
		{"", ""},
		{"$(echo sh) -c true", ""}, // shell-shaped: left to the shell
		{"'quoted tool' x", ""},    // not a plain word
		{"FOO=1", ""},              // only assignments
	}
	for _, c := range cases {
		got := missingProgram(c.cmd, root)
		if (c.want == "") != (got == "") || !strings.Contains(got, c.want) {
			t.Errorf("missingProgram(%q) = %q, want %q", c.cmd, got, c.want)
		}
	}
}
