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

// ValidBaseRef reports whether s is an acceptable --base-ref value.
func ValidBaseRef(s string) bool { return validBaseRef(s) }
