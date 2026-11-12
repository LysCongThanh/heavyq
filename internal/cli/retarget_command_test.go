package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseRetargetArgs(t *testing.T) {
	for _, c := range []struct {
		name      string
		args      []string
		ref, to   string
		ok        bool
		stderrHas string
	}{
		{"ticket then flag", []string{"123", "--remote", "box2"}, "123", "box2", true, ""},
		{"flag then ticket", []string{"--remote", "box2", "123"}, "123", "box2", true, ""},
		{"flag with equals", []string{"--remote=box,local", "123"}, "123", "box,local", true, ""},
		{"ticket id", []string{"p3-123-ab", "--remote", "auto"}, "p3-123-ab", "auto", true, ""},
		{"two tickets", []string{"1", "2", "--remote", "box"}, "", "", false, "usage: heavyq retarget"},
		{"no ticket", []string{"--remote", "box"}, "", "", false, "usage: heavyq retarget"},
		{"no flag", []string{"123"}, "", "", false, "usage: heavyq retarget"},
		{"empty target", []string{"123", "--remote="}, "", "", false, "usage: heavyq retarget"},
		{"flag without a value", []string{"123", "--remote"}, "", "", false, "needs an argument"},
		{"unknown flag", []string{"123", "--where", "box"}, "", "", false, "not defined"},
	} {
		t.Run(c.name, func(t *testing.T) {
			var stderr bytes.Buffer
			ref, to, ok := parseRetargetArgs(c.args, &stderr)
			if ref != c.ref || to != c.to || ok != c.ok {
				t.Fatalf("parse %q = (%q, %q, %v), want (%q, %q, %v)", c.args, ref, to, ok, c.ref, c.to, c.ok)
			}
			if !strings.Contains(stderr.String(), c.stderrHas) {
				t.Fatalf("stderr %q lacks %q", stderr.String(), c.stderrHas)
			}
		})
	}
}
