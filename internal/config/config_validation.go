package config

import (
	"regexp"
	"strings"
	"time"
)

func setDur(d *Duration, def time.Duration) {
	if d.Duration <= 0 {
		d.Duration = def
	}
}

var (
	baseRefPattern  = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._/-]+$`)
	sshAliasPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@-]*$`)
)

func validBaseRef(s string) bool {
	return baseRefPattern.MatchString(s) && !strings.Contains(s, "..")
}

// BaseRefNone is the value of --base-ref and queue.base_ref that means "no base ref": it also switches off
// the default (origin's default branch, when the repository has one).
const BaseRefNone = "none"

// ValidBaseRef reports whether s is a remote/branch ref, the shape --base-ref and queue.base_ref take
// (BaseRefNone is accepted by both as well, but is not a ref).
func ValidBaseRef(s string) bool { return validBaseRef(s) }
