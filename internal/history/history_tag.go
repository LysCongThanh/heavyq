package history

import "regexp"

// TagPattern is what session, item and gate names must match: 1-80 characters
// that are safe in a shell word, a file name and a JSON line.
const TagPattern = `^[A-Za-z0-9._:-]{1,80}$`

var tagRe = regexp.MustCompile(TagPattern)

// ValidTag reports whether s may be recorded as a session, item or gate name.
func ValidTag(s string) bool { return tagRe.MatchString(s) }
