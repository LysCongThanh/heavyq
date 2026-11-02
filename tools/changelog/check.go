package main

import (
	"fmt"
	"path"
	"strings"
)

// skipLabel on a pull request says it needs no changelog entry (a refactor, CI, a typo).
const skipLabel = "no-changelog"

// fragmentDir is where fragments live, relative to the repository root.
const fragmentDir = "changelog.d"

// needsFragment reports whether a changed file is user-visible code: it lives under internal/ or cmd/ and is not
// a test or a test fixture.
func needsFragment(file string) bool {
	if !strings.HasPrefix(file, "internal/") && !strings.HasPrefix(file, "cmd/") {
		return false
	}
	return !strings.HasSuffix(file, "_test.go") && !strings.Contains(file, "/testdata/")
}

// checkPullRequest is the rule CI applies to a pull request: given the files it adds or changes (not the ones
// it deletes) and its labels, it returns what is wrong, one message each. Every fragment it brings must be
// named <slug>.<kind>.md; a PR that changes internal/ or cmd/ code must bring one unless it is labelled
// no-changelog.
func checkPullRequest(files, labels []string) []string {
	var problems []string
	haveFragment, firstCode := false, ""
	for _, f := range files {
		f = strings.TrimSpace(f)
		if dir, name := path.Split(f); dir == fragmentDir+"/" {
			if isIgnored(name) {
				continue
			}
			if _, _, err := parseFragmentName(name); err != nil {
				problems = append(problems, err.Error())
				continue
			}
			haveFragment = true
		} else if firstCode == "" && needsFragment(f) {
			firstCode = f
		}
	}
	for _, l := range labels {
		if strings.TrimSpace(l) == skipLabel {
			return problems
		}
	}
	if firstCode != "" && !haveFragment {
		problems = append(problems, fmt.Sprintf(
			"%s changes the code but the PR has no changelog fragment. Add %s/<slug>.<kind>.md (kind: %s) with one line for the release notes, or label the PR %q.",
			firstCode, fragmentDir, strings.Join(kinds, ", "), skipLabel))
	}
	return problems
}
