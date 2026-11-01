package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestCopyLinesPrefixesEveryLine(t *testing.T) {
	long := strings.Repeat("x", 70<<10) // longer than the read buffer: split, prefixed once
	cases := []struct{ name, in, want string }{
		{"lines", "a\nb\n", "[cmd 3] a\n[cmd 3] b\n"},
		{"empty line", "a\n\nb\n", "[cmd 3] a\n[cmd 3] \n[cmd 3] b\n"},
		{"no final newline", "a\nb", "[cmd 3] a\n[cmd 3] b\n"},
		{"empty stream", "", ""},
		{"crlf", "a\r\nb\r\n", "[cmd 3] a\n[cmd 3] b\n"},
		{"long line", long + "\nz\n", "[cmd 3] " + long + "\n[cmd 3] z\n"},
		{"long line without newline", long, "[cmd 3] " + long + "\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var out bytes.Buffer
			s := &childSet{}
			s.copyLines(&out, strings.NewReader(c.in), "[cmd 3] ")
			if out.String() != c.want {
				t.Fatalf("got %.80q (%d bytes), want %.80q (%d bytes)", out.String(), out.Len(), c.want, len(c.want))
			}
		})
	}
}

func TestChildArgsPassTheConfigOn(t *testing.T) {
	run := []string{"3", "--gate", "g", "--", "sh", "-c", "true"}
	if got := (&childSet{}).childArgs(run); strings.Join(got, " ") != strings.Join(run, " ") {
		t.Fatalf("no config: %v", got)
	}
	got := (&childSet{cfg: "/etc/h.toml"}).childArgs(run)
	if len(got) != len(run)+2 || got[0] != "--config" || got[1] != "/etc/h.toml" || got[2] != "3" {
		t.Fatalf("with config: %v", got)
	}
}
