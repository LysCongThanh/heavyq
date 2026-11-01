package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeAgentsBlock(t *testing.T) {
	block := agentsBlock()
	stale := agentsBegin + "\nold text the user never edits\n" + agentsEnd
	cases := []struct {
		name, doc, want, outcome string
		wantErr                  bool
	}{
		{"empty file", "", block + "\n", outcomeAdded, false},
		{"file with text", "# T\n", "# T\n\n" + block + "\n", outcomeAdded, false},
		{"no trailing newline", "# T", "# T\n\n" + block + "\n", outcomeAdded, false},
		{"current block", "# T\n\n" + block + "\n", "# T\n\n" + block + "\n", outcomeCurrent, false},
		{"stale block is replaced in place", "a\n" + stale + "\nz\n", "a\n" + block + "\nz\n", outcomeUpdated, false},
		{"text around the block is kept byte for byte", "a\r\n" + stale + "\r\nz", "a\r\n" + block + "\r\nz", outcomeUpdated, false},
		{"begin without end", "x\n" + agentsBegin + "\ny\n", "", "", true},
		{"end without begin", "x\n" + agentsEnd + "\n", "", "", true},
		{"reversed", agentsEnd + "\n" + agentsBegin + "\n", "", "", true},
		{"two blocks", stale + "\n" + stale + "\n", "", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, outcome, err := mergeAgentsBlock(c.doc)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			if got != c.want || outcome != c.outcome {
				t.Fatalf("got (%q, %q)\nwant (%q, %q)", got, outcome, c.want, c.outcome)
			}
			if err == nil {
				// Idempotent: merging the result changes nothing.
				again, outcome2, err := mergeAgentsBlock(got)
				if err != nil || again != got || outcome2 != outcomeCurrent {
					t.Fatalf("second merge: (%q, %q, %v)", again, outcome2, err)
				}
			}
		})
	}
}

func TestUpdateAgentDocSymlinks(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	agents := filepath.Join(root, "AGENTS.md")
	claude := filepath.Join(root, "CLAUDE.md")
	if err := os.WriteFile(agents, []byte("# Rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The common layout: CLAUDE.md is a symlink to AGENTS.md. Both are
	// updated through one real file, which ends up with a single block, and
	// the symlink stays a symlink.
	if err := os.Symlink("AGENTS.md", claude); err != nil {
		t.Fatal(err)
	}
	if out, err := updateAgentDoc(root, agents, true); err != nil || out != outcomeAdded {
		t.Fatalf("AGENTS.md: %q, %v", out, err)
	}
	if out, err := updateAgentDoc(root, claude, false); err != nil || out != outcomeCurrent {
		t.Fatalf("CLAUDE.md -> AGENTS.md: %q, %v", out, err)
	}
	if fi, err := os.Lstat(claude); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the symlink was replaced: %v %v", fi, err)
	}
	if got, _ := os.ReadFile(agents); strings.Count(string(got), agentsBegin) != 1 {
		t.Fatalf("want one block:\n%s", got)
	}

	// A symlink leaving the repository is refused and its target untouched.
	outside := filepath.Join(t.TempDir(), "elsewhere.md")
	if err := os.WriteFile(outside, []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "docs.md")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if out, err := updateAgentDoc(root, link, false); err == nil || !strings.Contains(err.Error(), "outside the repository") {
		t.Fatalf("outside symlink: %q, %v", out, err)
	}
	if got, _ := os.ReadFile(outside); string(got) != "keep\n" {
		t.Fatalf("the target outside was modified: %q", got)
	}
	// A dangling symlink is an error too, not a file to create.
	dangling := filepath.Join(root, "gone.md")
	if err := os.Symlink(filepath.Join(root, "nowhere.md"), dangling); err != nil {
		t.Fatal(err)
	}
	if _, err := updateAgentDoc(root, dangling, true); err == nil {
		t.Fatal("dangling symlink must be refused")
	}
}

func TestUpdateAgentDocRefusesWhatItCannotRewriteSafely(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Absent and not to be created: skipped silently.
	if out, err := updateAgentDoc(root, filepath.Join(root, "CLAUDE.md"), false); out != "" || err != nil {
		t.Fatalf("absent optional file: %q, %v", out, err)
	}
	// A directory, an oversized file, and one with a broken marker pair.
	dir := filepath.Join(root, "AGENTS.md")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := updateAgentDoc(root, dir, true); err == nil {
		t.Fatal("a directory must be refused")
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	big := strings.Repeat("x", maxAgentDocBytes+1)
	if err := os.WriteFile(dir, []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := updateAgentDoc(root, dir, true); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("oversized: %v", err)
	}
	broken := "intro\n" + agentsBegin + "\nno end\n"
	if err := os.WriteFile(dir, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := updateAgentDoc(root, dir, true); err == nil || !strings.Contains(err.Error(), "marker") {
		t.Fatalf("broken markers: %v", err)
	}
	if got, _ := os.ReadFile(dir); string(got) != broken {
		t.Fatalf("a refused file was modified:\n%s", got)
	}
}
