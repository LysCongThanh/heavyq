package hook

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/agentmode"
)

// ClaudeSafeEnvVars are the leading assignments Claude Code was seen to strip
// before it matches a prefix rule such as Bash(go test:*). Any other
// assignment makes it prompt (recorded corpus: internal/preflight/testdata).
var ClaudeSafeEnvVars = []string{"NODE_ENV", "CI", "GOOS", "GOARCH", "CGO_ENABLED", "RUST_LOG", "NO_COLOR", "TZ", "LANG"}

// envName is the NAME of a NAME=VAL assignment.
func envName(assign string) string {
	name, _, _ := strings.Cut(assign, "=")
	return name
}

// changesHeavyq reports whether an assignment would change heavyq itself
// rather than only the wrapped program: the rewrite puts assignments in front
// of heavyq, so PATH decides whether the shell finds it, and HOME, TMPDIR,
// XDG_* and HEAVYQ_* decide its queue dir, slots, config and ledger. The
// agent-detection variables switch its output mode.
func changesHeavyq(name string) bool {
	switch {
	case name == "PATH", name == "HOME", name == "TMPDIR", name == "CLAUDE_CONFIG_DIR":
		return true
	case strings.HasPrefix(name, "XDG_"), strings.HasPrefix(name, "HEAVYQ_"):
		return true
	}
	return agentmode.IsAgentVar(name)
}

// heavyqEnvConflict returns the first assignment that would change heavyq
// itself, or "".
func heavyqEnvConflict(env []string) string {
	for _, a := range env {
		if name := envName(a); changesHeavyq(name) {
			return name
		}
	}
	return ""
}

// allowScopeOK reports whether hook.permission = allow may approve p: no
// broader than the Claude Code prefix rule a user would have written, so
// every assignment is one Claude Code strips and a cd stays inside cwd.
func allowScopeOK(p *Parsed, cwd string) bool {
	for _, a := range p.Env {
		if !slices.Contains(ClaudeSafeEnvVars, envName(a)) {
			return false
		}
	}
	return !p.HasCd || cdStaysInside(p.Cd, cwd)
}

// cdStaysInside reports whether `cd dir` from cwd lands in cwd or below it,
// judged on the text alone: no "..", no "~", no option, no glob, an absolute
// dir only under cwd. A glob is out because the text is not the path: where
// globskipdots is off (bash before 5.2) `.[.]` and `.?` expand to "..".
func cdStaysInside(dir, cwd string) bool {
	if dir == "" || strings.HasPrefix(dir, "-") || strings.HasPrefix(dir, "~") ||
		strings.ContainsAny(dir, "*?[") || slices.Contains(strings.Split(dir, "/"), "..") {
		return false
	}
	if !filepath.IsAbs(dir) {
		return true
	}
	if cwd == "" || !filepath.IsAbs(cwd) {
		return false
	}
	d, c := filepath.Clean(dir), filepath.Clean(cwd)
	return d == c || strings.HasPrefix(d, strings.TrimSuffix(c, "/")+"/")
}

// cacheSeesEnv reports whether every assignment is a cache.env variable, so
// --cache keys on its value. Otherwise a variant run would replay a result it
// never produced.
func cacheSeesEnv(env, cacheEnv []string) bool {
	for _, a := range env {
		if !slices.Contains(cacheEnv, envName(a)) {
			return false
		}
	}
	return true
}
