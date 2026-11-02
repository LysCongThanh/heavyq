package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// kinds are the Keep a Changelog headings a fragment may use, in the order a release lists them.
var kinds = []string{"added", "changed", "fixed", "security"}

// heading is the section heading of a kind: "added" is "### Added".
func heading(kind string) string { return "### " + strings.ToUpper(kind[:1]) + kind[1:] }

func validKind(kind string) bool {
	for _, k := range kinds {
		if k == kind {
			return true
		}
	}
	return false
}

// fragment is one changelog.d/<slug>.<kind>.md file.
type fragment struct {
	Path string // as read, for deleting it after the release
	Slug string
	Kind string
	Text string // one or more Markdown bullets
}

// parseFragmentName splits "<slug>.<kind>.md". The slug is a PR number or a short name: letters, digits, "-", "_"
// and "." (so "88-top" and "v0.5.stances" both work); the kind is one of kinds.
func parseFragmentName(name string) (slug, kind string, err error) {
	base, ok := strings.CutSuffix(name, ".md")
	if !ok {
		return "", "", fmt.Errorf("%s: a fragment is named <slug>.<kind>.md", name)
	}
	i := strings.LastIndex(base, ".")
	if i <= 0 || i == len(base)-1 {
		return "", "", fmt.Errorf("%s: a fragment is named <slug>.<kind>.md, kind is one of %s", name, strings.Join(kinds, ", "))
	}
	slug, kind = base[:i], base[i+1:]
	if !validKind(kind) {
		return "", "", fmt.Errorf("%s: unknown kind %q, use one of %s", name, kind, strings.Join(kinds, ", "))
	}
	for _, r := range slug {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return "", "", fmt.Errorf("%s: the slug may hold letters, digits, - _ and . only", name)
		}
	}
	return slug, kind, nil
}

// isIgnored: the explainer next to the fragments and dotfiles (.gitkeep) are not fragments.
func isIgnored(name string) bool { return name == "README.md" || strings.HasPrefix(name, ".") }

// bullets turns the text of a fragment into Markdown bullets. Text that already starts with "- " is kept; any
// other text becomes one bullet, its continuation lines indented.
func bullets(text string) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	if strings.HasPrefix(text, "- ") {
		return text
	}
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		l = strings.TrimRight(l, " \t")
		switch {
		case i == 0:
			lines[i] = "- " + l
		case l != "":
			lines[i] = "  " + l
		default:
			lines[i] = ""
		}
	}
	return strings.Join(lines, "\n")
}

// readFragments reads every fragment of dir, sorted by file name. A file that is not a well-formed fragment, or
// is empty, is an error: a typo in a kind must not drop an entry from the release notes.
func readFragments(dir string) ([]fragment, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []fragment
	for _, e := range entries {
		if e.IsDir() || isIgnored(e.Name()) {
			continue
		}
		slug, kind, err := parseFragmentName(e.Name())
		if err != nil {
			return nil, err
		}
		path := filepath.Join(dir, e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(string(b)) == "" {
			return nil, fmt.Errorf("%s: empty fragment", e.Name())
		}
		out = append(out, fragment{Path: path, Slug: slug, Kind: kind, Text: bullets(string(b))})
	}
	sort.SliceStable(out, func(i, j int) bool { return filepath.Base(out[i].Path) < filepath.Base(out[j].Path) })
	return out, nil
}
