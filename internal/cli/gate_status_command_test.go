package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/proof"
)

func TestTreeLabelShowsBothEndsOfADirtyId(t *testing.T) {
	head := "0123456789abcdef0123456789abcdef01234567"
	digest := "fedcba9876543210fedcba9876543210"
	for _, c := range []struct {
		g    proof.Gate
		want string
	}{
		{proof.Gate{Tree: head}, "tree 0123456789ab"},
		{proof.Gate{Tree: head + "+" + digest, Dirty: true}, "tree 0123456789ab+fedcba98 (uncommitted edits)"},
		{proof.Gate{}, "an unidentified tree"},
	} {
		if got := treeLabel(c.g); got != c.want {
			t.Errorf("treeLabel(%+v) = %q, want %q", c.g, got, c.want)
		}
	}
}

func TestClipKeepsCharactersWhole(t *testing.T) {
	if got := clip("short", 14); got != "short" {
		t.Fatal(got)
	}
	if got := clip("a-very-long-lane-name", 14); got != "a-very-long-l~" {
		t.Fatal(got)
	}
	if got := clip("ééééééééé", 6); strings.ContainsRune(got, '�') || !strings.HasSuffix(got, "~") {
		t.Fatalf("%q", got)
	}
}

// Command lines in the table and the JSON go through the same redaction as
// everywhere else, even though they come from a committed file.
func TestPrintGateRedactsSecrets(t *testing.T) {
	exit := 1
	g := proof.Gate{Gate: "ready", Tree: strings.Repeat("a", 40), Commands: []proof.Command{
		{Cmd: "API_TOKEN=abc123 make check", State: proof.Failed, At: time.Now(), Lane: "l", Exit: &exit},
	}}
	var out bytes.Buffer
	printGate(&out, g)
	if strings.Contains(out.String(), "abc123") || !strings.Contains(out.String(), "API_TOKEN=***") || !strings.Contains(out.String(), "(exit 1)") {
		t.Fatalf("\n%s", out.String())
	}
	if redactedGate(g).Commands[0].Cmd == g.Commands[0].Cmd {
		t.Fatal("redactedGate must not return the raw command")
	}
	if !strings.Contains(g.Commands[0].Cmd, "abc123") {
		t.Fatal("redactedGate must not modify its argument")
	}
}
