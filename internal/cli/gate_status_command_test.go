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

func TestPrintGateNamesFlakyAndUnderLoadFailures(t *testing.T) {
	zero, one, seven := 0, 1, 7
	now := time.Now()
	g := proof.Gate{Gate: "ready", Tree: strings.Repeat("a", 40), Commands: []proof.Command{
		{Cmd: "go vet", State: proof.Proven, At: now, Exit: &zero},
		{Cmd: "go test", State: proof.Failed, Class: proof.ClassFlaky, At: now, Exit: &one, Passes: 2, Fails: 1,
			Reason: "flaky: 1 fail / 2 pass on this tree"},
		{Cmd: "go build", State: proof.Failed, Class: proof.ClassUnderLoad, At: now, Exit: &seven,
			Reason: "under load: load 9.00 >= 6.00 at start"},
		{Cmd: "make lint", State: proof.Failed, Class: proof.ClassFailed, At: now, Exit: &one, Fails: 1},
		{Cmd: "make e2e", State: proof.Missing},
	}}
	var out bytes.Buffer
	printGate(&out, g)
	got := out.String()
	for _, want := range []string{
		"(exit 1; flaky: 1 fail / 2 pass on this tree)",
		"(exit 7; under load: load 9.00 >= 6.00 at start)",
		"make lint  (exit 1)\n",
		"3 failed (1 flaky, 1 under load), 1 missing. Prove it with: heavyq gate run ready",
		"rerun once before debugging: heavyq gate run ready",
		"flaky \u2014 re-run once to confirm: heavyq gate run ready",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestGateCountsSummary(t *testing.T) {
	cases := []struct {
		n    gateCounts
		want string
	}{
		{gateCounts{}, "0 failed, 0 missing"},
		{gateCounts{failed: 2, missing: 1}, "2 failed, 1 missing"},
		{gateCounts{failed: 3, flaky: 1, underLoad: 2}, "3 failed (1 flaky, 2 under load), 0 missing"},
		{gateCounts{failed: 1, flaky: 1}, "1 failed (1 flaky), 0 missing"},
	}
	for _, c := range cases {
		if got := c.n.String(); got != c.want {
			t.Errorf("got %q, want %q", got, c.want)
		}
	}
	if h := (gateCounts{failed: 2, missing: 1}).hints("ready"); len(h) != 0 {
		t.Fatalf("plain failures need no hint: %v", h)
	}
}
