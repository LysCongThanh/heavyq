package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sample = `# Changelog

Intro text.

## [Unreleased]

### Added

- Old thing, written by hand.
  Second line of it.

### Tests

- A legacy section.

## [0.4.0] - 2026-11-02

### Added

- Four.

[Unreleased]: https://github.com/o/r/compare/v0.4.0...HEAD
[0.4.0]: https://github.com/o/r/compare/v0.3.0...v0.4.0
`

func writeFragments(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestFoldMergesFragmentsIntoTheNewVersion(t *testing.T) {
	dir := writeFragments(t, map[string]string{
		"88-top.added.md":       "**Top.** A terminal monitor.\nSecond line.\n",
		"90-bug.fixed.md":       "- Fixed the bug.\n- Fixed another.\n",
		"91-cve.security.md":    "Closed a hole.",
		"README.md":             "how to write a fragment",
		".gitkeep":              "",
		"89-more.added.md":      "More.",
		"92-reshape.changed.md": "Reshaped.",
	})
	frags, err := readFragments(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(frags) != 5 {
		t.Fatalf("read %d fragments, want 5 (README.md and dotfiles are not fragments)", len(frags))
	}
	got, err := fold(sample, "0.5.0", "2026-11-03", frags)
	if err != nil {
		t.Fatal(err)
	}
	want := `# Changelog

Intro text.

## [Unreleased]

## [0.5.0] - 2026-11-03

### Added

- Old thing, written by hand.
  Second line of it.
- **Top.** A terminal monitor.
  Second line.
- More.

### Changed

- Reshaped.

### Fixed

- Fixed the bug.
- Fixed another.

### Security

- Closed a hole.

### Tests

- A legacy section.

## [0.4.0] - 2026-11-02

### Added

- Four.

[Unreleased]: https://github.com/o/r/compare/v0.5.0...HEAD
[0.5.0]: https://github.com/o/r/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/o/r/compare/v0.3.0...v0.4.0
`
	if got != want {
		t.Errorf("folded changelog differs:\n%s\n--- want ---\n%s", got, want)
	}
}

// Folding the real CHANGELOG.md changes nothing that was written: every old line is still there, in order, and
// the released versions below [Unreleased] are byte for byte the same.
func TestFoldKeepsTheRealChangelogContent(t *testing.T) {
	real, err := os.ReadFile("../../CHANGELOG.md")
	if err != nil {
		t.Skip("no CHANGELOG.md next to the tool")
	}
	frag := []fragment{{Kind: "added", Text: bullets("A new fragment.")}}
	got, err := fold(string(real), "9.9.9", "2030-01-01", frag)
	if err != nil {
		t.Fatal(err)
	}
	old := string(real)
	oldReleased := old[strings.Index(old, "\n## [0."):]
	oldReleased = oldReleased[:strings.Index(oldReleased, "\n[Unreleased]:")]
	if !strings.Contains(got, oldReleased) {
		t.Error("the released versions are not preserved byte for byte")
	}
	pos := 0
	for _, l := range strings.Split(old, "\n") {
		if l == "" || strings.HasPrefix(l, "[Unreleased]:") {
			continue
		}
		i := strings.Index(got[pos:], l)
		if i < 0 {
			t.Fatalf("line %q is gone or out of order after folding", l)
		}
		pos += i
	}
	if !strings.Contains(got, "## [Unreleased]\n\n## [9.9.9] - 2030-01-01\n") || !strings.Contains(got, "- A new fragment.") {
		t.Error("the new version section is missing")
	}
}

func TestFoldErrors(t *testing.T) {
	frag := []fragment{{Kind: "added", Text: "- x"}}
	for name, c := range map[string]struct{ log, version string }{
		"bad version":         {sample, "five"},
		"version exists":      {sample, "0.4.0"},
		"no unreleased":       {"# Changelog\n\n## [0.4.0] - 2026-11-02\n", "0.5.0"},
		"prose in unreleased": {"## [Unreleased]\n\nLoose prose.\n\n### Added\n\n- x\n", "0.5.0"},
	} {
		if _, err := fold(c.log, c.version, "2026-11-03", frag); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if _, err := fold("## [Unreleased]\n\n## [0.4.0] - 2026-11-02\n", "0.5.0", "2026-11-03", nil); err == nil {
		t.Error("an empty release must be refused")
	}
}

func TestFoldWithoutLinksLeavesNoneBehind(t *testing.T) {
	got, err := fold("# C\n\n## [Unreleased]\n\n### Fixed\n\n- a\n", "1.0.0", "2026-01-01", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "compare") || !strings.Contains(got, "## [1.0.0] - 2026-01-01\n\n### Fixed\n\n- a\n") {
		t.Errorf("unexpected result:\n%s", got)
	}
}

func TestFragmentNames(t *testing.T) {
	for _, ok := range []string{"88.added.md", "88-top.fixed.md", "v0.5.stances.security.md", "a_b.changed.md"} {
		if _, _, err := parseFragmentName(ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{"88.md", "88.add.md", ".added.md", "88.added.txt", "a b.added.md", "x.Added.md", "88.added"} {
		if _, _, err := parseFragmentName(bad); err == nil {
			t.Errorf("%s: accepted", bad)
		}
	}
}

func TestReadFragmentsRejectsMistakes(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"typo in the kind": {"a.add.md": "x"},
		"empty":            {"a.added.md": " \n"},
	} {
		if _, err := readFragments(writeFragments(t, files)); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestCheckPullRequest(t *testing.T) {
	code := "internal/cli/status_command.go"
	cases := []struct {
		name   string
		files  []string
		labels []string
		bad    bool
	}{
		{"code without fragment", []string{code}, nil, true},
		{"code with fragment", []string{code, "changelog.d/88.added.md"}, nil, false},
		{"cmd counts too", []string{"cmd/heavyq/main.go"}, nil, true},
		{"label exempts", []string{code}, []string{"ci", "no-changelog"}, false},
		{"docs only", []string{"README.md", "docs/queue.md", ".github/workflows/ci.yml"}, nil, false},
		{"tests only", []string{"internal/cli/x_test.go", "internal/hook/testdata/a.json"}, nil, false},
		{"bad fragment name", []string{code, "changelog.d/88.md"}, nil, true},
		{"bad name even with the label", []string{"changelog.d/88.md"}, []string{"no-changelog"}, true},
		{"README is not a fragment", []string{code, "changelog.d/README.md"}, nil, true},
		{"empty list", nil, nil, false},
	}
	for _, c := range cases {
		if got := checkPullRequest(c.files, c.labels); (len(got) > 0) != c.bad {
			t.Errorf("%s: problems=%v, want bad=%v", c.name, got, c.bad)
		}
	}
}

func TestReleaseEndToEnd(t *testing.T) {
	dir := writeFragments(t, map[string]string{"1.added.md": "One.", "2.fixed.md": "Two."})
	file := filepath.Join(t.TempDir(), "CHANGELOG.md")
	if err := os.WriteFile(file, []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	args := []string{"release", "v0.5.0", "-date", "2026-11-03", "-dir", dir, "-file", file}

	if rc := run(append(args, "-dry-run"), nil, &out, &errb); rc != 0 || !strings.Contains(out.String(), "## [0.5.0] - 2026-11-03") {
		t.Fatalf("dry run rc=%d\n%s%s", rc, out.String(), errb.String())
	}
	if left, _ := os.ReadDir(dir); len(left) != 2 {
		t.Fatal("a dry run deleted fragments")
	}
	if b, _ := os.ReadFile(file); string(b) != sample {
		t.Fatal("a dry run wrote the changelog")
	}

	out.Reset()
	if rc := run(args, nil, &out, &errb); rc != 0 {
		t.Fatalf("release rc=%d\n%s", rc, errb.String())
	}
	if left, _ := os.ReadDir(dir); len(left) != 0 {
		t.Errorf("%d fragments left", len(left))
	}
	b, _ := os.ReadFile(file)
	if !strings.Contains(string(b), "## [0.5.0] - 2026-11-03") || !strings.Contains(string(b), "- One.") || !strings.Contains(string(b), "- Two.") {
		t.Errorf("changelog not folded:\n%s", b)
	}

	// the same version twice is refused and touches nothing
	if rc := run(args, nil, &out, &errb); rc != 1 {
		t.Errorf("second release rc=%d, want 1", rc)
	}
}

func TestCheckSubcommand(t *testing.T) {
	var out, errb bytes.Buffer
	in := strings.NewReader("internal/cli/a.go\nchangelog.d/1.added.md\n")
	if rc := run([]string{"check"}, in, &out, &errb); rc != 0 {
		t.Errorf("rc=%d %s", rc, errb.String())
	}
	errb.Reset()
	if rc := run([]string{"check", "-labels", "x,y"}, strings.NewReader("internal/cli/a.go\n"), &out, &errb); rc != 1 || !strings.Contains(errb.String(), "no-changelog") {
		t.Errorf("rc=%d %s", rc, errb.String())
	}
	if rc := run([]string{"bogus"}, nil, &out, &errb); rc != 2 {
		t.Errorf("unknown command rc=%d", rc)
	}
}
