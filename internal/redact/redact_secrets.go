// Package redact hides likely secrets in command lines.
package redact

import "regexp"

// Patterns of secrets that commonly appear in command lines.
var secretPatterns = []struct {
	re   *regexp.Regexp
	repl string
}{
	// scheme://user:password@host -> scheme://***@host
	{regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://)[^/\s:@]+:[^/\s@]+@`), "${1}***@"},
	// NAME=value where NAME looks secret (FOO_TOKEN=, password=, API_KEY=...)
	{regexp.MustCompile(`(?i)\b([A-Za-z0-9_]*(?:token|secret|passw(?:or)?d|api[_-]?key|access[_-]?key|private[_-]?key|credential|auth)[A-Za-z0-9_]*=)\S+`), "${1}***"},
	// --password value / --token=value / --api-key value
	{regexp.MustCompile(`(?i)(--?(?:password|passwd|token|secret|api-key|apikey|auth)[= ])\S+`), "${1}***"},
	// header-style values: "X-Api-Key: v", "token: v"
	{regexp.MustCompile(`(?i)\b((?:x-)?(?:api[-_]?key|auth[-_]?token|token|secret)\s*:\s*)[^\s'"]+`), "${1}***"},
	// Authorization headers: "Bearer xxx", "Basic xxx"
	{regexp.MustCompile(`(?i)\b(bearer|basic) [A-Za-z0-9._~+/=-]{8,}`), "${1} ***"},
	// well-known token shapes
	{regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|sk-[A-Za-z0-9_-]{20,}|xox[abpr]-[A-Za-z0-9-]{10,}|AKIA[0-9A-Z]{16})\b`), "***"},
}

// Command hides likely secrets in a command line before it is stored,
// shown or served. It errs on the side of hiding.
func Command(cmd string) string {
	for _, p := range secretPatterns {
		cmd = p.re.ReplaceAllString(cmd, p.repl)
	}
	return cmd
}
