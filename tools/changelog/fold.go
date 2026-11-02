package main

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	unreleasedHeading = "## [Unreleased]"
	linkLine          = regexp.MustCompile(`^\[[^\]]+\]: `)
	unreleasedLink    = regexp.MustCompile(`^\[Unreleased\]: (\S+)/compare/v?(\S+?)\.\.\.HEAD\s*$`)
	versionRe         = regexp.MustCompile(`^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)
)

// section is a "### Heading" of a version and its lines, without the blank lines around them.
type section struct {
	Heading string
	Lines   []string
}

// canonicalRank orders the headings a release uses; others (a legacy "### Tests") sort after.
func canonicalRank(h string) int {
	for i, k := range kinds {
		if heading(k) == h {
			return i
		}
	}
	return len(kinds)
}

// parseSections splits the body of a version into sections; text before the first "###" is dropped when blank
// and is an error otherwise, so folding never loses prose.
func parseSections(body []string) ([]section, error) {
	var secs []section
	for _, l := range body {
		if strings.HasPrefix(l, "### ") {
			secs = append(secs, section{Heading: strings.TrimRight(l, " ")})
			continue
		}
		if len(secs) == 0 {
			if strings.TrimSpace(l) != "" {
				return nil, fmt.Errorf("the [Unreleased] section has text before its first ### heading: %q", l)
			}
			continue
		}
		secs[len(secs)-1].Lines = append(secs[len(secs)-1].Lines, l)
	}
	for i := range secs {
		secs[i].Lines = trimBlank(secs[i].Lines)
	}
	return secs, nil
}

func trimBlank(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// addFragments appends each fragment to the last section of its kind (the changelog may already hold some that
// were written by hand), or opens that section where it belongs in the canonical order.
func addFragments(secs []section, frags []fragment) []section {
	for _, f := range frags {
		h := heading(f.Kind)
		idx := -1
		for i, s := range secs {
			if s.Heading == h {
				idx = i
			}
		}
		if idx < 0 {
			at := len(secs)
			for i, s := range secs {
				if canonicalRank(s.Heading) > canonicalRank(h) {
					at = i
					break
				}
			}
			secs = append(secs, section{})
			copy(secs[at+1:], secs[at:])
			secs[at] = section{Heading: h}
			idx = at
		}
		secs[idx].Lines = append(secs[idx].Lines, strings.Split(f.Text, "\n")...)
	}
	return secs
}

// fold moves the fragments, and whatever [Unreleased] already holds, into a new "## [version] - date" section
// above the previous release, and leaves an empty [Unreleased] behind. The compare links at the bottom follow.
func fold(changelog, version, date string, frags []fragment) (string, error) {
	if !versionRe.MatchString(version) {
		return "", fmt.Errorf("version %q is not MAJOR.MINOR.PATCH", version)
	}
	lines := strings.Split(strings.TrimRight(changelog, "\n"), "\n")
	start := -1
	for i, l := range lines {
		if strings.TrimRight(l, " ") == unreleasedHeading {
			start = i
		}
		if strings.HasPrefix(l, "## ["+version+"]") {
			return "", fmt.Errorf("CHANGELOG.md already has a section for %s", version)
		}
	}
	if start < 0 {
		return "", fmt.Errorf("CHANGELOG.md has no %q heading", unreleasedHeading)
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## [") || linkLine.MatchString(lines[i]) {
			end = i
			break
		}
	}
	secs, err := parseSections(lines[start+1 : end])
	if err != nil {
		return "", err
	}
	secs = addFragments(secs, frags)
	if len(secs) == 0 {
		return "", fmt.Errorf("nothing to release: no fragments and an empty [Unreleased]")
	}

	var out []string
	out = append(out, lines[:start]...)
	out = append(out, unreleasedHeading, "", fmt.Sprintf("## [%s] - %s", version, date), "")
	for _, s := range secs {
		out = append(out, s.Heading, "")
		out = append(out, s.Lines...)
		out = append(out, "")
	}
	out = append(out, withLinks(lines[end:], version)...)
	return strings.Join(out, "\n") + "\n", nil
}

// withLinks moves "[Unreleased]: .../compare/vPREV...HEAD" to the new version and adds "[version]: ...vPREV...vVERSION".
func withLinks(rest []string, version string) []string {
	var out []string
	for _, l := range rest {
		m := unreleasedLink.FindStringSubmatch(l)
		if m == nil {
			out = append(out, l)
			continue
		}
		base, prev := m[1], m[2]
		out = append(out,
			fmt.Sprintf("[Unreleased]: %s/compare/v%s...HEAD", base, version),
			fmt.Sprintf("[%s]: %s/compare/v%s...v%s", version, base, prev, version))
	}
	return out
}
