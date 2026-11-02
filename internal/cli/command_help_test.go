package cli

import (
	"bytes"
	"strings"
	"testing"
)

func runMain(t *testing.T, args ...string) (rc int, out, errOut string) {
	t.Helper()
	var o, e bytes.Buffer
	rc = Main(args, &o, &e)
	return rc, o.String(), e.String()
}

// Every command answers --help with its own usage block on stdout and exit 0, without a config.
func TestEveryCommandHasHelp(t *testing.T) {
	t.Setenv("HEAVYQ_CONFIG", "/nonexistent/heavyq.toml") // a missing config must not matter
	for _, c := range commandTable() {
		args := []string{c.Name, "--help"}
		if c.Name == "run" {
			args = []string{"4", "--help"}
		}
		rc, out, errOut := runMain(t, args...)
		if rc != 0 || errOut != "" {
			t.Errorf("%v: rc=%d stderr=%q", args, rc, errOut)
		}
		if !strings.Contains(out, "usage:\n  heavyq") || !strings.Contains(out, c.Summary) {
			t.Errorf("%v: no usage block:\n%s", args, out)
		}
		if strings.Contains(out, "Usage of") || strings.Contains(out, "-test.") {
			t.Errorf("%v: prints raw flag defaults:\n%s", args, out)
		}
		for _, line := range c.Synopsis {
			if !strings.Contains(out, line) {
				t.Errorf("%v: synopsis line missing: %s", args, line)
			}
		}
		for _, f := range c.Flags {
			if !strings.Contains(out, f.display()) || !strings.Contains(out, f.Help) {
				t.Errorf("%v: flag %s missing", args, f.display())
			}
		}
		for _, s := range c.Subs {
			for _, f := range s.Flags {
				if !strings.Contains(out, f.display()) {
					t.Errorf("%v: flag %s of %s missing", args, f.display(), s.Name)
				}
			}
		}
	}
}

func TestHelpWorksInEveryPosition(t *testing.T) {
	for _, args := range [][]string{
		{"gate", "run", "--help"},
		{"gate", "run", "-h"},
		{"--config", "x.toml", "status", "--help"},
		{"item", "claim", "7", "--json", "--help"},
		{"4", "--lane", "x", "--help"},
	} {
		if rc, out, _ := runMain(t, args...); rc != 0 || !strings.Contains(out, "usage:") {
			t.Errorf("%v: rc=%d out=%q", args, rc, out)
		}
	}
	rc, out, _ := runMain(t, "gate", "--help")
	if rc != 0 || !strings.Contains(out, "gate status") || !strings.Contains(out, "--keep-going") {
		t.Errorf("gate --help lacks its subcommands and flags:\n%s", out)
	}
}

// A -h after "--" belongs to the user's command, and unknown commands are not ours to answer.
func TestHelpIgnoresWhatIsNotAskedFor(t *testing.T) {
	for _, args := range [][]string{
		{"4", "--", "go", "test", "-h"},
		{"4", "--lane", "x", "--", "tool", "--help"},
		{"nosuch", "--help"},
		{"status"},
	} {
		if c, ok := wantsCommandHelp(args); ok {
			t.Errorf("%v: treated as help for %s", args, c.Name)
		}
	}
}

func TestHelpCommandTakesACommandName(t *testing.T) {
	rc, out, _ := runMain(t, "help", "status")
	if rc != 0 || !strings.Contains(out, "heavyq status:") {
		t.Errorf("help status: rc=%d out=%q", rc, out)
	}
	if rc, out, _ = runMain(t, "help", "agents"); rc != 0 || !strings.Contains(out, "heavyq") {
		t.Errorf("help agents: rc=%d", rc)
	}
	rc, _, errOut := runMain(t, "help", "nosuch")
	if rc != ExitUsage || !strings.Contains(errOut, "no help topic") {
		t.Errorf("help nosuch: rc=%d stderr=%q", rc, errOut)
	}
}

// The unknown-command error lists the table's commands, so a new command appears there by itself.
func TestUnknownCommandListsTable(t *testing.T) {
	t.Setenv("HEAVYQ_CONFIG", "")
	rc, _, errOut := runMain(t, "frobnicate")
	if rc != ExitUsage {
		t.Fatalf("rc=%d", rc)
	}
	for _, n := range commandNames() {
		if !strings.Contains(errOut, n) {
			t.Errorf("error does not list %q:\n%s", n, errOut)
		}
	}
}
