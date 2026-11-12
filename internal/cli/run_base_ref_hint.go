package cli

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/gitutil"
)

// originRefRe finds an origin/<branch> name in a line of output (group 1). It starts at a word boundary, so
// src/origin/x.ts is a path and not a ref, and stops at the first character a branch name does not hold here,
// so the quotes git puts around it do not belong to it.
var originRefRe = regexp.MustCompile(`(?:^|[^A-Za-z0-9._/-])(origin/[A-Za-z0-9._-]+(?:/[A-Za-z0-9._-]+)*)`)

// missingRevisionWords are what git and the tools around it say when a revision they were asked for does not
// exist (matched in lower case).
var missingRevisionWords = []string{
	"unknown revision", "bad revision", "not a valid", "invalid reference", "did not match any",
	"no such", "not found", "could not find", "couldn't find", "does not exist",
}

// missingOriginRef is the origin/<branch> that the last lines of a command's output say is missing, or "".
// It reads from the end, where a failing command says why.
func missingOriginRef(out []byte) string {
	lines := strings.Split(string(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		low := strings.ToLower(lines[i])
		found := false
		for _, w := range missingRevisionWords {
			if strings.Contains(low, w) {
				found = true
				break
			}
		}
		if !found {
			continue
		}
		if m := originRefRe.FindStringSubmatch(lines[i]); m != nil {
			// "origin/main..HEAD" and "origin/main...HEAD" name origin/main; so does "origin/main." ending a sentence.
			ref, _, _ := strings.Cut(m[1], "..")
			if ref = strings.TrimRight(ref, "."); config.ValidBaseRef(ref) {
				return ref
			}
		}
	}
	return ""
}

// baseRefNext is the one thing to do after a command that ran on another machine failed saying an
// origin/<branch> ref is missing there: ship that ref. "" when the output says nothing of the kind, when the
// run already ships that ref (then it is missing for another reason), or when this repository does not have
// the ref either (--base-ref could not recreate it).
func (r *run) baseRefNext() string {
	if r.out.capture == nil || r.pl.repo == "" {
		return ""
	}
	tail, _ := r.out.capture.Tail()
	ref := missingOriginRef(tail)
	if ref == "" || ref == r.opts.BaseRef {
		return ""
	}
	if _, err := gitutil.ResolveCommit(r.pl.repo, "refs/remotes/"+ref); err != nil {
		return ""
	}
	if r.opts.Gate != "" { // a gate's commands take their base ref from the configuration
		return fmt.Sprintf("the remote has no %s; set queue.base_ref = %q in the config, then run: heavyq gate run %s", ref, ref, r.opts.Gate)
	}
	return fmt.Sprintf("the remote has no %s; re-run with --base-ref %s", ref, ref)
}
