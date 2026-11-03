package cli

import (
	"bytes"
	"testing"
)

func TestNotFoundLineMatchesShellReportsOnly(t *testing.T) {
	yes := []string{
		"sh: 1: nosuchcmd-xyz: not found",
		"sh: 1: npx: not found",
		"bash: line 1: npx: command not found",
		"bash: line 12: ./tool: command not found",
		"bash: npx: command not found",
		"sh: line 1: tsc: command not found",
		"sh: tsc: command not found",
		"zsh: command not found: tsc",
		"-bash: foo: command not found",
		"./ci.sh: line 7: tsc: command not found",
		"sh: 1: npx: not found\r",
		"dash: 1: vitest: not found  ",
	}
	for _, l := range yes {
		if !isNotFoundLine(l) {
			t.Errorf("should match: %q", l)
		}
	}
	no := []string{
		"",
		"not found",
		"FAIL  src/a.test.ts > resolves the user: not found",
		"user: 12: record: not found",
		"Error: module 'x' not found",
		"  sh: 1: nosuchcmd: not found", // indented: quoted by something else
		"expected output to contain \"command not found\"",
		"AssertionError: sh: 1: foo: not found is not in the output",
		"bash: line 1: foo: command not found (expected)",
		"cat: nofile: No such file or directory",
		"error: command not found: foo", // not a shell
		"bash: a b: command not found",  // a command name has no space
		"sh: 1: foo: not found because the PATH is empty",
	}
	for _, l := range no {
		if isNotFoundLine(l) {
			t.Errorf("should not match: %q", l)
		}
	}
}

func TestSuspectScanFindsLineAcrossWritesAndStreams(t *testing.T) {
	s := &suspectScan{}
	var out, errOut bytes.Buffer
	o, e := s.wrap(&out), s.wrap(&errOut)
	for _, chunk := range []string{"building...\nsh: 1: no", "such", "cmd-xyz: no", "t found\nlast\n"} {
		if _, err := e.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := o.Write([]byte("fine\n")); err != nil {
		t.Fatal(err)
	}
	if got := s.Line(); got != "sh: 1: nosuchcmd-xyz: not found" {
		t.Fatalf("line %q", got)
	}
	if errOut.String() != "building...\nsh: 1: nosuchcmd-xyz: not found\nlast\n" {
		t.Fatalf("the scan must pass bytes through unchanged: %q", errOut.String())
	}
	if out.String() != "fine\n" {
		t.Fatalf("stdout %q", out.String())
	}
}

func TestSuspectScanLastLineWithoutNewlineAndLongLines(t *testing.T) {
	s := &suspectScan{}
	var sink bytes.Buffer
	w := s.wrap(&sink)
	long := bytes.Repeat([]byte("x"), 5000)
	_, _ = w.Write(long)
	_, _ = w.Write([]byte("\nsh: 1: foo: not found"))
	if got := s.Line(); got != "sh: 1: foo: not found" {
		t.Fatalf("line %q", got)
	}
	clean := &suspectScan{}
	w = clean.wrap(&sink)
	_, _ = w.Write(append(append([]byte("sh: 1: foo: not found "), long...), '\n'))
	if got := clean.Line(); got != "" {
		t.Fatalf("a long line is not a shell report: %q", got)
	}
}

func TestWarnShellPipe(t *testing.T) {
	warn := map[string][]string{
		"sh -c with a pipe":        {"sh", "-c", "npx tsc | tail -15"},
		"bash -c":                  {"bash", "-c", "a | b"},
		"login shell flags":        {"bash", "-lc", "a | b"},
		"pipe next to &&":          {"sh", "-c", "a | tail && b | tail"},
		"|& is a pipe":             {"bash", "-c", "a |& b"},
		"absolute path to a shell": {"/bin/sh", "-c", "a | b"},
	}
	for name, argv := range warn {
		var b bytes.Buffer
		warnShellPipe(&b, argv)
		if b.Len() == 0 {
			t.Errorf("%s: expected a warning", name)
		}
	}
	quiet := map[string][]string{
		"no pipe":                 {"sh", "-c", "a && b"},
		"or is not a pipe":        {"sh", "-c", "a || b"},
		"pipe in single quotes":   {"sh", "-c", "grep 'a|b' f"},
		"pipe in double quotes":   {"sh", "-c", `grep "a|b" f`},
		"pipefail is asked for":   {"bash", "-o", "pipefail", "-c", "a | b"},
		"pipefail inside":         {"bash", "-c", "set -o pipefail; a | b"},
		"not a shell":             {"go", "test", "-run", "A|B"},
		"no -c":                   {"sh", "script.sh", "a|b"},
		"the gate wrapper itself": {"sh", "-c", `command -v bash >/dev/null 2>&1 && exec bash -o pipefail -c "$1"; eval "$1"`, "heavyq-gate", "a | b"},
	}
	for name, argv := range quiet {
		var b bytes.Buffer
		warnShellPipe(&b, argv)
		if b.Len() != 0 {
			t.Errorf("%s: unexpected warning %q", name, b.String())
		}
	}
}
