package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// #127: agent work trees inside the repository (.claude/worktrees) never make
// the tree unknown; `heavyq init` and `heavyq doctor` say how to hide them from
// git status too, and say nothing once they are ignored.
func TestE2ENestedWorkTreesHintInInitAndDoctor(t *testing.T) {
	h := newRig(t, baseConfig)
	repo := goModRepo(t, h)
	wt := filepath.Join(repo, ".claude", "worktrees", "agent-x")
	gitIn(t, h, repo, "worktree", "add", "-q", "-b", "agent-x", wt)

	rc, out, errOut := h.runSplit("init")
	if rc != 0 || !strings.Contains(out, "nested work trees: .claude/worktrees") || !strings.Contains(out, "info/exclude") ||
		!strings.HasSuffix(strings.TrimSpace(out), "next: heavyq gate run ready") {
		t.Fatalf("init rc=%d\n%s\n%s", rc, out, errOut)
	}
	if rc, out = h.run("doctor"); !strings.Contains(out, "nested work trees: .claude/worktrees") {
		t.Fatalf("doctor rc=%d:\n%s", rc, out)
	}

	exclude := filepath.Join(repo, ".git", "info", "exclude")
	if err := os.MkdirAll(filepath.Dir(exclude), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exclude, []byte(".claude/worktrees/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, out = h.run("doctor"); strings.Contains(out, "nested work trees") {
		t.Fatalf("doctor still hints after the exclude line:\n%s", out)
	}
}
