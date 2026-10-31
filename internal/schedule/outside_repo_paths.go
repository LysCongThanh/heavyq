package schedule

import (
	"regexp"
	"strings"
)

// Remote execution only ships the committed tree of the repository, so a
// command that touches anything outside the repo would silently see a
// different world on the remote. OutsideRepoReference is deliberately
// conservative: when in doubt, the ticket stays local.

const repoToken = "@REPO@"

var (
	// a ".." path component: "..", "../x", "a/../b", "cd .."
	dotDotRe = regexp.MustCompile(`(^|[^A-Za-z0-9_.-])\.\.(/|$|[\s"'` + "`" + `)])|/\.\.(/|$|[\s"'` + "`" + `)])`)
	// "~" at the start of a word or after = : ( etc.
	tildeRe = regexp.MustCompile(`(^|[\s=:"'(<>,;|&` + "`" + `])~`)
	homeRe  = regexp.MustCompile(`\$HOME\b|\$\{HOME\}|\$\{?OLDPWD\b|\bdirname\b`)
	// variables whose value is machine-local or sensitive and does not exist remotely
	sensitiveVarRe = regexp.MustCompile(`(?i)\$\{?[A-Za-z_]*(URL|DSN|URI|DB|DATABASE|PROD|SECRET|TOKEN|PASS|KEY|CRED|DUMP|BACKUP|DIR|PATH|HOME)[A-Za-z_0-9]*`)
	// a variable used as a path prefix: $X/..., ${X}/...
	varPathRe = regexp.MustCompile(`\$\{?[A-Za-z_][A-Za-z0-9_]*\}?/`)
	// absolute paths: "/x" at the start of a word, after = : etc, or after a short flag like -I/x
	absPathRe = regexp.MustCompile(`(?:^|[\s=:"'(<>,;|&` + "`" + `]|\s-[A-Za-z]+)(/[^\s"')` + "`" + `;|&<>]*)`)
)

// allowedAbs lists absolute paths that exist identically on any machine.
var allowedAbs = []string{"/dev/null", "/dev/stdin", "/dev/stdout", "/dev/stderr"}
var allowedAbsPrefixes = []string{"/usr/bin/", "/usr/local/bin/", "/bin/"}

// OutsideRepoReference returns a non-empty reason when argv appears to
// reference something outside repoRoot (so it must not run remotely).
func OutsideRepoReference(argv []string, repoRoot string) string {
	all := strings.Join(argv, " ")
	if root := strings.TrimSuffix(repoRoot, "/"); root != "" {
		// Only whole-path matches: "/src/repo-other" must not become "@REPO@-other".
		re := regexp.MustCompile(regexp.QuoteMeta(root) + `(/|$|[\s"'` + "`" + `;)])`)
		all = re.ReplaceAllString(all, repoToken+"$1")
	}
	switch {
	case dotDotRe.MatchString(all):
		return "uses a '..' path"
	case tildeRe.MatchString(all), homeRe.MatchString(all):
		return "uses ~, $HOME, $OLDPWD or dirname"
	case sensitiveVarRe.MatchString(all):
		return "uses a machine-local variable (URL/DB/TOKEN/DIR/PATH-like)"
	case varPathRe.MatchString(all):
		return "uses a variable as a path prefix"
	}
	for _, m := range absPathRe.FindAllStringSubmatch(all, -1) {
		if p := m[1]; !absAllowed(p) {
			return "uses absolute path " + p
		}
	}
	return ""
}

func absAllowed(p string) bool {
	for _, a := range allowedAbs {
		if p == a {
			return true
		}
	}
	for _, pre := range allowedAbsPrefixes {
		if strings.HasPrefix(p, pre) && !strings.Contains(p, "..") {
			return true
		}
	}
	return false
}
