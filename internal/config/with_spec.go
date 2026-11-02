package config

import (
	"fmt"
	"strings"
	"unicode"
)

// ParseSiblingSpec splits a --with value (or an entry of a gate's `with`)
// "REPO[@REF]" and validates its shape without touching the disk. REPO is a
// bare directory name that lives next to the main repository (only
// A-Za-z0-9._-, starting with a letter or digit, at most 64 characters, so
// never "." or ".."), or a path containing "/". REF defaults to HEAD. The
// first "@" separates them, so a path holding "@" cannot be used.
func ParseSiblingSpec(spec string) (repo, ref string, err error) {
	repo, ref, hasRef := strings.Cut(spec, "@")
	if !hasRef {
		ref = "HEAD"
	}
	bad := func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }
	switch {
	case repo == "" || ref == "":
		return "", "", fmt.Errorf("--with %q: expected REPO[@REF]", spec)
	case strings.HasPrefix(ref, "-") || strings.IndexFunc(ref, bad) >= 0:
		return "", "", fmt.Errorf("--with %q: invalid ref %q", spec, ref)
	case strings.IndexFunc(repo, unicode.IsControl) >= 0:
		return "", "", fmt.Errorf("--with %q: invalid repository", spec)
	case !strings.Contains(repo, "/") && !SafeName.MatchString(repo):
		return "", "", fmt.Errorf("--with %q: directory name %q must be 1-64 characters from A-Za-z0-9._- starting with a letter or digit", spec, repo)
	}
	return repo, ref, nil
}
