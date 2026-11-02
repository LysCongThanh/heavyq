package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func completionScript(t *testing.T, shell string) string {
	t.Helper()
	var out, errb bytes.Buffer
	if rc := Main([]string{"completion", shell}, &out, &errb); rc != 0 {
		t.Fatalf("completion %s: rc=%d stderr=%s", shell, rc, errb.String())
	}
	return out.String()
}

func TestCompletionUsageErrors(t *testing.T) {
	for _, args := range [][]string{{"completion"}, {"completion", "powershell"}, {"completion", "bash", "zsh"}} {
		var out, errb bytes.Buffer
		if rc := Main(args, &out, &errb); rc != ExitUsage || out.Len() != 0 || errb.Len() == 0 {
			t.Errorf("%v: rc=%d stdout=%q stderr=%q, want a usage error", args, rc, out.String(), errb.String())
		}
	}
}

// Every command and every flag of the table is in each script: they are generated from it.
func TestCompletionScriptsHoldTheTable(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		script := completionScript(t, shell)
		for _, name := range commandNames() {
			if !strings.Contains(script, name) {
				t.Errorf("%s: command %q missing", shell, name)
			}
		}
		for _, c := range commandTable {
			flags := append([]flagSpec{}, c.Flags...)
			for _, s := range c.Subs {
				flags = append(flags, s.Flags...)
			}
			for _, f := range flags {
				want := "--" + f.Long
				if shell == "fish" {
					want = "-l " + f.Long
				}
				if f.Long != "" && !strings.Contains(script, want) {
					t.Errorf("%s: flag %s of %s missing", shell, want, c.Name)
				}
			}
		}
	}
}

// The bash script is run for real: bash is given a command line and the candidates are read back.
func TestBashCompletionCandidates(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not installed")
	}
	script := filepath.Join(t.TempDir(), "heavyq.bash")
	if err := os.WriteFile(script, []byte(completionScript(t, "bash")), 0o600); err != nil {
		t.Fatal(err)
	}
	complete := func(words ...string) []string {
		t.Helper()
		quoted := make([]string, len(words))
		for i, w := range words {
			quoted[i] = "'" + w + "'"
		}
		prog := `source "$1"; COMP_WORDS=(` + strings.Join(quoted, " ") + `); COMP_CWORD=$((${#COMP_WORDS[@]} - 1)); _heavyq; printf '%s\n' "${COMPREPLY[@]}"`
		out, err := exec.Command(bash, "-c", prog, "bash", script).CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %v\n%s", words, err, out)
		}
		got := strings.Fields(string(out))
		sort.Strings(got)
		return got
	}
	cases := []struct {
		words []string
		want  string // space-separated, exact
	}{
		{[]string{"heavyq", "gate", ""}, "run status"},
		{[]string{"heavyq", "completion", ""}, "bash fish zsh"},
		{[]string{"heavyq", "hook", "install", ""}, "claude"},
		{[]string{"heavyq", "hook", "install", "claude", "--sc"}, "--scope"},
		{[]string{"heavyq", "hook", "install", "claude", "--scope", ""}, "local project user"},
		{[]string{"heavyq", "4", "--ma"}, "--max-wait"},
		{[]string{"heavyq", "4", "--remote", ""}, "auto local"},
		{[]string{"heavyq", "4", "--lane", ""}, ""},
		{[]string{"heavyq", "--config", "c.toml", "status", "--j"}, "--json"},
		{[]string{"heavyq", "gate", "run", "--k"}, "--keep-going"},
		{[]string{"heavyq", "history", "-"}, "-n -v"},
		{[]string{"heavyq", "item", "claim", "--"}, "--json --next"},
		{[]string{"heavyq", "nosuch", ""}, ""},
	}
	for _, c := range cases {
		if got := strings.Join(complete(c.words...), " "); got != c.want {
			t.Errorf("%q: got %q, want %q", c.words, got, c.want)
		}
	}
	top := strings.Join(complete("heavyq", ""), " ")
	for _, name := range commandNames() {
		if !strings.Contains(" "+top+" ", " "+name+" ") {
			t.Errorf("top level does not offer %q: %s", name, top)
		}
	}
	// After "--" the user's own command is completed, not heavyq's words.
	if got := complete("heavyq", "4", "--", "pytho"); len(got) == 0 || strings.Contains(strings.Join(got, " "), "--") {
		t.Errorf("after --: got %q, want program names", got)
	}
}

// Syntax check with the real shells, where installed.
func TestCompletionScriptsParse(t *testing.T) {
	for _, c := range []struct{ shell, flag string }{{"bash", "-n"}, {"zsh", "-n"}, {"fish", "--no-execute"}} {
		path, err := exec.LookPath(c.shell)
		if err != nil {
			t.Logf("%s not installed: syntax not checked", c.shell)
			continue
		}
		script := filepath.Join(t.TempDir(), "heavyq."+c.shell)
		if err := os.WriteFile(script, []byte(completionScript(t, c.shell)), 0o600); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command(path, c.flag, script).CombinedOutput(); err != nil {
			t.Errorf("%s rejects its script: %v\n%s", c.shell, err, out)
		}
	}
}
