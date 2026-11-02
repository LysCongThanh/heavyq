package gitutil

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Read-only views of branch history for internal/flow/collect: which branches
// exist and when the base branch was merged into them. Everything is local
// (no fetch): offline, a branch is as fresh as the last fetch made it.

// Branch is a local branch, or a branch of origin that has no local one.
type Branch struct {
	Name string    // short name, "feat/x" for refs/heads/feat/x and refs/remotes/origin/feat/x
	Ref  string    // full ref name, resolves in the repository
	Tip  time.Time // committer time of the tip
}

// Branches lists the branches whose tip is at or after since, newest first, at
// most max. A branch that exists locally and on origin appears once, with the
// ref of the newer tip.
func Branches(repo string, since time.Time, max int) ([]Branch, error) {
	out, err := gitRaw(repo, "for-each-ref", "--sort=-committerdate",
		"--format=%(refname)%09%(committerdate:unix)", "refs/heads", "refs/remotes/origin")
	if err != nil {
		return nil, err
	}
	var res []Branch
	index := map[string]int{}
	for _, line := range strings.Split(out, "\n") {
		ref, secs, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if !ok {
			continue
		}
		name := strings.TrimPrefix(strings.TrimPrefix(ref, "refs/heads/"), "refs/remotes/origin/")
		n, err := strconv.ParseInt(secs, 10, 64)
		if err != nil || name == "HEAD" {
			continue
		}
		b := Branch{Name: name, Ref: ref, Tip: time.Unix(n, 0)}
		if b.Tip.Before(since) {
			continue
		}
		if i, seen := index[name]; seen {
			if b.Tip.After(res[i].Tip) {
				res[i] = b
			}
			continue
		}
		index[name] = len(res)
		res = append(res, b)
	}
	if max > 0 && len(res) > max {
		res = res[:max]
	}
	return res, nil
}

// MergeCommit is a merge commit made on a branch.
type MergeCommit struct {
	SHA     string
	Parent1 string    // the branch's own line
	Parent2 string    // what was merged in
	At      time.Time // committer time of the merge
	// PrevAt is the committer time of Parent1: the branch's previous commit, so
	// At-PrevAt is the least time the merge took to make.
	PrevAt time.Time
}

// maxMergesListed bounds the merge commits one call looks at.
const maxMergesListed = 100

// MainMerges lists the merge commits on branch (not on base) that merged base
// in, with a committer time from since to until, oldest first. base and branch
// are refs that resolve in repo. A merge of any other branch is not listed.
func MainMerges(repo, base, branch string, since, until time.Time) ([]MergeCommit, error) {
	if strings.HasPrefix(base, "-") || strings.HasPrefix(branch, "-") {
		return nil, fmt.Errorf("git: refusing ref %q", base+" "+branch)
	}
	out, err := gitRaw(repo, "log", "--merges", "--max-count="+strconv.Itoa(maxMergesListed),
		"--format=%H %P %ct", base+".."+branch)
	if err != nil {
		return nil, err
	}
	var res []MergeCommit
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 { // sha, at least two parents, time
			continue
		}
		secs, err := strconv.ParseInt(f[len(f)-1], 10, 64)
		if err != nil {
			continue
		}
		m := MergeCommit{SHA: f[0], Parent1: f[1], Parent2: f[2], At: time.Unix(secs, 0)}
		if m.At.Before(since) || m.At.After(until) || !IsAncestor(repo, m.Parent2, base) {
			continue
		}
		m.PrevAt = commitTime(repo, m.Parent1)
		res = append(res, m)
	}
	for i, j := 0, len(res)-1; i < j; i, j = i+1, j-1 { // git prints newest first
		res[i], res[j] = res[j], res[i]
	}
	return res, nil
}

// commitTime is the committer time of rev; the zero time when it is unknown.
func commitTime(repo, rev string) time.Time {
	out, err := git(repo, "show", "-s", "--format=%ct", rev)
	if err != nil {
		return time.Time{}
	}
	n, err := strconv.ParseInt(out, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(n, 0)
}
