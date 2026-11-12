package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A pool's values reach the command as HEAVYQ_<NAME>, after the inherited
// environment. A pool named after a variable heavyq reads would hand its value
// to every heavyq the command runs: "history" would move their ledger. Such a
// config is refused before anything runs, from the user config and from a
// repository's heavyq.toml alike.
func TestPoolNamedAfterAHeavyqVariableIsRefused(t *testing.T) {
	h := newRig(t, baseConfig+"[[pool]]\nname = \"history\"\nvalues = [\"leak\"]\n")
	rc, out := h.run("3", "--needs", "history", "--", "sh", "-c", "echo leaked-history=$HEAVYQ_HISTORY")
	if rc != ExitUsage || strings.Contains(out, "leaked-") || !strings.Contains(out, "reserved") {
		t.Fatalf("user config pool \"history\": rc=%d, want 2 naming the reserved name and nothing run:\n%s", rc, out)
	}

	h = newRig(t, baseConfig)
	repo := gitRepo(t, h)
	if err := os.WriteFile(filepath.Join(repo, "heavyq.toml"), []byte("[[pool]]\nname = \"test\"\nsize = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rc, out = h.run("3", "--needs", "test", "--", "sh", "-c", "echo leaked-test=$HEAVYQ_TEST")
	if rc != ExitUsage || strings.Contains(out, "leaked-") || !strings.Contains(out, "reserved") {
		t.Fatalf("heavyq.toml pool \"test\": rc=%d, want 2 naming the reserved name and nothing run:\n%s", rc, out)
	}
}
