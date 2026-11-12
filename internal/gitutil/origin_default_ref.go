package gitutil

import "strings"

// OriginDefaultRef is the remote-tracking ref (origin/main) of the branch the work of the repository's
// "origin" lands on: where refs/remotes/origin/HEAD points when that branch exists here, else origin/main
// when that exists. ok is false when the repository has neither (no remote called origin, or one that was
// never fetched), and then there is no default to ship.
//
// One git process answers it: this runs for every proof lookup (`gate status`, `why`), which are hot paths.
func OriginDefaultRef(repo string) (ref string, ok bool) {
	const head, main = "refs/remotes/origin/HEAD", "refs/remotes/origin/main"
	// %(symref) is where a symbolic ref points; %(objectname) is the commit it resolves to, and a ref that
	// does not resolve (a dangling origin/HEAD) is not listed at all.
	out, err := gitRaw(repo, "for-each-ref", "--format=%(refname)%09%(symref)%09%(objectname)", head, main)
	if err != nil {
		return "", false
	}
	hasMain := false
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 3 || !shaRe.MatchString(f[2]) {
			continue
		}
		switch f[0] {
		case head:
			if target, found := strings.CutPrefix(f[1], "refs/remotes/"); found && strings.HasPrefix(target, "origin/") {
				return target, true
			}
		case main:
			hasMain = true
		}
	}
	if hasMain {
		return "origin/main", true
	}
	return "", false
}
