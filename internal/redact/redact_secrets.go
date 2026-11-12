// Package redact hides likely secrets in command lines.
package redact

import "regexp"

// Building blocks of the patterns. Command treats NUL like a space between a
// key and its value but as part of a value: Argv uses it to stand for the
// spaces inside one argv element, so the whole element is one value.
const (
	sep = `[\s\x00]` // between a key and its value
	// a shell word: unquoted runs and quoted strings ("a b", 'a b'); an
	// unterminated quote runs to the end, so a cut line hides its tail
	quoted = `"[^"]*(?:"|$)|'[^']*(?:'|$)`
	word   = `(?:[^\s"']+|` + quoted + `)+`
	// a word that is not a flag (does not start with '-')
	plainWord = `(?:[^\s"'-]|` + quoted + `)` + `(?:[^\s"']+|` + quoted + `)*`
	// a name that looks like it holds a secret
	secretKey = `(?:token|secret|passw(?:or)?d|api[_-]?key|access[_-]?key|private[_-]?key|credential|auth|_pwd)`
)

// Patterns of secrets that commonly appear in command lines, in order.
var secretPatterns = []struct {
	re   *regexp.Regexp
	repl string
}{
	// scheme://user:password@host -> scheme://***@host; the password runs to
	// the last '@' before the host, so a raw '@' in it is hidden too
	{regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://)[^/\s\x00:]+:[^/\s\x00]+@`), "${1}***@"},
	// NAME=value where NAME looks secret (FOO_TOKEN=, password=, API_KEY=, MYSQL_PWD=...)
	{regexp.MustCompile(`(?i)\b([A-Za-z0-9_]*` + secretKey + `[A-Za-z0-9_]*=)` + word), "${1}***"},
	// --password value / --token=value / --api-key "value"
	{regexp.MustCompile(`(?i)(--?(?:password|passwd|token|secret|api-key|apikey|auth)(?:=|` + sep + `+))` + word), "${1}***"},
	// `set <secret-looking key> <value>` (aws configure set, npm config set...)
	{regexp.MustCompile(`(?i)(\bset` + sep + `+[^\s\x00=]*` + secretKey + `[^\s\x00=]*` + sep + `+)` + plainWord), "${1}***"},
	// docker/podman login -p value
	{regexp.MustCompile(`(?i)(\b(?:docker|podman|nerdctl|buildah)` + sep + `+login\b[^|;&\n]*?` + sep + `(?-i:-p)(?:=|` + sep + `+)?)` + word), "${1}***"},
	// sshpass -p value (its own -p, not the -p of the ssh it runs)
	{regexp.MustCompile(`(\bsshpass(?:` + sep + `+-[^p\s\x00][^\s\x00]*)*` + sep + `+-p` + sep + `*)` + word), "${1}***"},
	// Authorization headers of any scheme: "Authorization: token v", "Authorization: v"
	{regexp.MustCompile(`(?i)\b(authorization` + sep + `*:` + sep + `*(?:[A-Za-z][A-Za-z0-9._-]*` + sep + `+)?)[^\s'"]+`), "${1}***"},
	// header-style values: "X-Api-Key: v", "token: v"
	{regexp.MustCompile(`(?i)\b((?:x-)?(?:api[-_]?key|auth[-_]?token|token|secret)` + sep + `*:` + sep + `*)[^\s'"]+`), "${1}***"},
	// Authorization values outside a header: "Bearer xxx", "Basic xxx"
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
	cmd = userPassword.ReplaceAllStringFunc(cmd, maskUserPassword)
	return mysqlLine.ReplaceAllStringFunc(cmd, func(s string) string {
		return mysqlPassword.ReplaceAllString(s, "${1}***")
	})
}
