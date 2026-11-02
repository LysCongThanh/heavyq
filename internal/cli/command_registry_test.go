package cli

import (
	"bytes"
	"strings"
	"testing"
)

// The usage text is generated from the table: every synopsis line and every flag of the run form is in it,
// and nothing about it is written by hand per command.
func TestUsageTextIsGeneratedFromTheTable(t *testing.T) {
	u := usageText()
	if !strings.HasPrefix(u, usageTagline+"\n\nusage:\n") {
		t.Errorf("usage starts with %q", strings.SplitN(u, "\n", 2)[0])
	}
	last := -1
	for _, c := range commandTable() {
		for _, line := range c.Synopsis {
			i := strings.Index(u, "  "+line+"\n")
			if i < 0 {
				t.Errorf("usage misses the synopsis line of %s: %q", c.Name, line)
			}
			if i < last {
				t.Errorf("usage lists %s out of table order", c.Name)
			}
			last = i
		}
	}
	run, _ := commandByName("run")
	for _, f := range run.Flags {
		if !strings.Contains(u, f.display()) || !strings.Contains(u, f.Help) {
			t.Errorf("usage misses the run flag %s", f.display())
		}
	}
	if !strings.Contains(u, run.Notes) {
		t.Error("usage misses the notes of the run form (priorities, exit codes, agent mode)")
	}
}

// --help, -h and help print the usage on stdout; no arguments print it on stderr with the usage exit code.
func TestUsageSurfaces(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"help"}} {
		rc, out, _ := runMain(t, args...)
		if rc != 0 || out != usageText() {
			t.Errorf("%v: rc=%d, usage on stdout=%v", args, rc, out == usageText())
		}
	}
	var stdout, stderr bytes.Buffer
	if rc := Main(nil, &stdout, &stderr); rc != ExitUsage || stderr.String() != usageText() || stdout.Len() != 0 {
		t.Errorf("no arguments: rc=%d, usage on stderr=%v, stdout=%q", rc, stderr.String() == usageText(), stdout.String())
	}
}

// A first argument that is no command and no priority is a usage error that names the commands.
func TestUnknownFirstArgument(t *testing.T) {
	for _, arg := range []string{"nope", "0", "10", "run"} {
		rc, _, errOut := runMain(t, arg)
		if rc != ExitUsage || !strings.Contains(errOut, "first argument must be a priority 1-9 or a command") || !strings.Contains(errOut, "status") {
			t.Errorf("%q: rc=%d\n%s", arg, rc, errOut)
		}
	}
}

// Every table entry is dispatchable by name, and a priority is the run form.
func TestLookupCommand(t *testing.T) {
	for _, c := range commandTable() {
		if c.Name == "run" {
			continue
		}
		if got, ok := lookupCommand(c.Name); !ok || got.Name != c.Name {
			t.Errorf("lookupCommand(%q) = %q, %v", c.Name, got.Name, ok)
		}
		for _, a := range c.Aliases {
			if got, ok := lookupCommand(a); !ok || got.Name != c.Name {
				t.Errorf("alias %q of %s resolves to %q, %v", a, c.Name, got.Name, ok)
			}
		}
	}
	for _, p := range []string{"1", "5", "9", "05"} {
		if got, ok := lookupCommand(p); !ok || got.Name != "run" {
			t.Errorf("priority %q resolves to %q, %v", p, got.Name, ok)
		}
	}
}

// Order decides the position; equal orders sort by name; the table stays sorted whatever the init order was.
func TestTableIsOrdered(t *testing.T) {
	tab := commandTable()
	for i := 1; i < len(tab); i++ {
		a, b := tab[i-1], tab[i]
		if a.Order > b.Order || (a.Order == b.Order && a.Name >= b.Name) {
			t.Errorf("%s (order %d) is listed before %s (order %d)", a.Name, a.Order, b.Name, b.Order)
		}
	}
	if tab[0].Name != "run" {
		t.Errorf("the table must start with the run form, starts with %q", tab[0].Name)
	}
}

// A malformed or duplicate entry stops the program at start.
func TestRegisterCommandRejectsBadEntries(t *testing.T) {
	saved := append([]commandSpec{}, registered...)
	t.Cleanup(func() { registered = saved })
	ok := func(*invocation) int { return 0 }
	for name, c := range map[string]commandSpec{
		"no name":     {Summary: "s", Synopsis: []string{"heavyq x"}, Run: ok},
		"no summary":  {Name: "x", Synopsis: []string{"heavyq x"}, Run: ok},
		"no synopsis": {Name: "x", Summary: "s", Run: ok},
		"no run":      {Name: "x", Summary: "s", Synopsis: []string{"heavyq x"}},
		"duplicate":   {Name: "status", Summary: "s", Synopsis: []string{"heavyq status"}, Run: ok},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: registerCommand did not panic", name)
				}
			}()
			registerCommand(c)
		}()
	}
}
