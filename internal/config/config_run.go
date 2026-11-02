package config

import "fmt"

// RunConfig is the [run] section: defaults for `heavyq <prio> -- cmd`.
type RunConfig struct {
	// NeedGHRepos are repository directory names whose commands call the
	// GitHub CLI with this machine's login. A run in one of them behaves as if
	// --need-gh were given: it only runs on this machine.
	NeedGHRepos []string `toml:"need_gh_repos"`
}

// normalize checks that every entry is a plain directory name and listed once.
func (r *RunConfig) normalize() error {
	seen := map[string]bool{}
	for i, n := range r.NeedGHRepos {
		if !SafeName.MatchString(n) {
			return fmt.Errorf("[run] need_gh_repos[%d]: %q must be a repository directory name matching %s", i, n, SafeName)
		}
		if seen[n] {
			return fmt.Errorf("[run] need_gh_repos: %q is listed twice", n)
		}
		seen[n] = true
	}
	return nil
}

// NeedsGH reports whether runs in the repository directory repoDir always need
// the GitHub CLI login of this machine (it is in [run] need_gh_repos).
func (c *Config) NeedsGH(repoDir string) bool {
	for _, n := range c.Run.NeedGHRepos {
		if n == repoDir {
			return true
		}
	}
	return false
}
