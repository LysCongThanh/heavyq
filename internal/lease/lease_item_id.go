package lease

import (
	"fmt"
	"regexp"
	"strings"
)

// itemRe is the anchor item id rule plus '/', which derived ids (from plan
// files such as "plans/foo/phase-01") use.
var itemRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._/-]{0,127}$`)

// ValidateItem fails closed on any id that is not a clean, relative,
// slash-separated path of [a-z0-9._-] segments: no empty, "." or ".."
// segments, no trailing slash.
func ValidateItem(item string) error {
	if !itemRe.MatchString(item) {
		return fmt.Errorf("invalid item id %q: want [a-z0-9][a-z0-9._/-]{0,127}", item)
	}
	for _, seg := range strings.Split(item, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("invalid item id %q: empty, \".\" or \"..\" path segment", item)
		}
	}
	return nil
}

// fileName maps a validated item id to a flat file name. '/' becomes '+',
// which the id alphabet excludes, so the mapping is injective and the name
// can never contain a path separator.
func fileName(item string) string {
	return strings.ReplaceAll(item, "/", "+") + ".json"
}
