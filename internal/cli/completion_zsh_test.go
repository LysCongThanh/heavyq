package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// zshStubs replace the completion builtins, so the script's own logic can be run without a terminal:
// each prints what it was asked to offer.
const zshStubs = `
_describe() { local -a a; a=("${(@P)4}"); print -r -- "describe: ${(j: :)${(o)${(@)a%%:*}}}" }
compadd() { local -a b; b=("${(@)argv[2,-1]}"); print -r -- "compadd: ${(j: :)${(o)b}}" }
_files() { print -r -- "files: $*" }
_command_names() { print -r -- "commands:" }
compdef() { :; }
`

// The zsh script is run for real, with the completion builtins stubbed: zsh is given a command line
// ("words" and CURRENT) and what the script offers is read back.
func TestZshCompletionCandidates(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh not installed")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "_heavyq")
	if err := os.WriteFile(script, []byte(completionScript(t, "zsh")), 0o600); err != nil {
		t.Fatal(err)
	}
	offered := func(words ...string) string {
		t.Helper()
		quoted := make([]string, len(words))
		for i, w := range words {
			quoted[i] = "'" + w + "'"
		}
		prog := zshStubs + "source " + script + "\nwords=(" + strings.Join(quoted, " ") + "); CURRENT=${#words}; _heavyq\n"
		file := filepath.Join(dir, "run.zsh")
		if err := os.WriteFile(file, []byte(prog), 0o600); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command(zsh, "-f", file).CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %v\n%s", words, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	cases := []struct {
		words []string
		want  string
	}{
		{[]string{"heavyq", "gate", ""}, "describe: run status"},
		{[]string{"heavyq", "completion", ""}, "describe: bash fish zsh"},
		{[]string{"heavyq", "hook", "install", ""}, "describe: claude"},
		{[]string{"heavyq", "4", "--ma"}, "describe: --max-wait"},
		{[]string{"heavyq", "4", "--remote", ""}, "compadd: auto local"},
		{[]string{"heavyq", "hook", "install", "claude", "--scope", ""}, "compadd: local project user"},
		{[]string{"heavyq", "--config", "c.toml", "status", "--j"}, "describe: --json"},
		{[]string{"heavyq", "--config", ""}, "files:"},
		{[]string{"heavyq", "4", "--repo", ""}, "files: -/"},
		{[]string{"heavyq", "4", "--lane", ""}, ""},
		{[]string{"heavyq", "4", "--", "pyth"}, "commands:"},
		{[]string{"heavyq", "4", "--", "pytest", "x"}, "files:"},
	}
	for _, c := range cases {
		if got := offered(c.words...); got != c.want {
			t.Errorf("%q: got %q, want %q", c.words, got, c.want)
		}
	}
	if top := " " + offered("heavyq", "") + " "; !strings.Contains(top, " status ") || !strings.Contains(top, " gate ") {
		t.Errorf("top level: %s", top)
	}
}
