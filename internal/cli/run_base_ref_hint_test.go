package cli

import (
	"testing"

	"github.com/LysCongThanh/heavyq/internal/config"
)

// The lines are what git (and tools that call it) print when an origin/<branch> is not there.
func TestMissingOriginRefReadsWhatGitSays(t *testing.T) {
	for _, c := range []struct{ name, out, want string }{
		{"ambiguous argument", "fatal: ambiguous argument 'origin/develop': unknown revision or path not in the working tree.\n", "origin/develop"},
		{"a range", "fatal: ambiguous argument 'origin/main...HEAD': unknown revision or path not in the working tree.\n", "origin/main"},
		{"a two dot range", "fatal: ambiguous argument 'origin/main..HEAD': unknown revision or path not in the working tree.\n", "origin/main"},
		{"bad revision with a slash in the branch", "fatal: bad revision 'origin/release/1.2..HEAD'\n", "origin/release/1.2"},
		{"not a valid object name", "fatal: Not a valid object name origin/main\n", "origin/main"},
		{"pathspec", "error: pathspec 'origin/feat-x' did not match any file(s) known to git\n", "origin/feat-x"},
		{"invalid reference", "fatal: invalid reference: origin/main\n", "origin/main"},
		{"ends a sentence", "error: unknown revision origin/main.\n", "origin/main"},
		{"the last mention counts", "fatal: bad revision 'origin/old'\nretrying\nfatal: bad revision 'origin/new'\nUse '--' to separate paths\n", "origin/new"},
		{"in the middle of the output", "start\nfatal: Not a valid object name origin/main\nmore output\nFAIL\n", "origin/main"},
		{"a ref is not a missing ref", "Your branch is ahead of 'origin/main' by 2 commits.\n", ""},
		{"a path is not a ref", "ENOENT: no such file or directory, open 'src/origin/x.ts'\n", ""},
		{"another remote", "fatal: bad revision 'upstream/main'\n", ""},
		{"nothing to do with refs", "FAIL src/a.test.ts\n  expected 1, got 2\n", ""},
		{"empty", "", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := missingOriginRef([]byte(c.out)); got != c.want {
				t.Fatalf("missingOriginRef(%q) = %q, want %q", c.out, got, c.want)
			}
		})
	}
}

// What was asked for decides, and only the default needs a repository (the end-to-end tests cover that).
func TestEffectiveBaseRefTakesWhatWasAskedFor(t *testing.T) {
	for _, c := range []struct{ dir, asked, want string }{
		{t.TempDir(), config.BaseRefNone, ""},
		{t.TempDir(), "origin/develop", "origin/develop"},
		{t.TempDir(), "upstream/main", "upstream/main"},
		{t.TempDir(), "", ""}, // not a repository: no default
		{"", "", ""},          // no repository at all
		{"", config.BaseRefNone, ""},
	} {
		if got := effectiveBaseRef(c.dir, c.asked); got != c.want {
			t.Errorf("effectiveBaseRef(%q, %q) = %q, want %q", c.dir, c.asked, got, c.want)
		}
	}
}
