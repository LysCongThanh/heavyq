package hook

import (
	"path/filepath"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/config"
)

// DefaultRules are the built-in commands worth queueing. User rules from
// [[hook.rule]] are checked first; these follow unless default_rules = false.
var DefaultRules = func() []config.HookRule {
	var heavy, light []string
	for _, pm := range []string{"npm", "pnpm", "yarn"} {
		heavy = append(heavy, pm+" test", pm+" run test", pm+" run build", pm+" run gate")
		light = append(light, pm+" run typecheck", pm+" run lint")
	}
	heavy = append(heavy,
		"go test", "go build ./...",
		"pytest", "python -m pytest", "python3 -m pytest",
		"cargo test", "cargo build",
		"make test", "make check",
		"gradle test", "./gradlew test", "mvn test", "./mvnw test")
	rules := []config.HookRule{{Match: heavy}, {Match: light, Light: true}}
	for i := range rules {
		_ = rules[i].Normalize() // fills the default priority; patterns are static and valid
	}
	return rules
}()

// MatchRule returns the first rule with a pattern that is a word prefix of
// words. Patterns compare whole words: "npm run test" does not match
// "npm run test:e2e".
func MatchRule(rules []config.HookRule, words []string) (config.HookRule, string, bool) {
	for _, r := range rules {
		for _, m := range r.Match {
			pat := strings.Fields(m)
			if len(pat) > len(words) {
				continue
			}
			ok := true
			for i, w := range pat {
				if words[i] != w {
					ok = false
					break
				}
			}
			if ok {
				return r, m, true
			}
		}
	}
	return config.HookRule{}, "", false
}

// IsHeavyq reports whether a word names the heavyq binary.
func IsHeavyq(word string) bool { return filepath.Base(word) == "heavyq" }

// ShellQuote quotes s for a POSIX shell when it is not a plain word.
func ShellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_./:@%+=,-") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
