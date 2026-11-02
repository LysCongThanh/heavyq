package collect

import (
	"errors"
	"fmt"
	"time"

	"github.com/LysCongThanh/heavyq/internal/flow"
	"github.com/LysCongThanh/heavyq/internal/gitutil"
)

// Limits of one git collection.
const (
	// ConflictCap is the longest a conflict-resolving merge counts as rework.
	// Git knows when a merge was made, not how long it took: the span is the
	// time since the branch's previous commit, and a branch that sat idle for
	// hours before the merge was not reworking all that time.
	ConflictCap = 15 * time.Minute
	// minConflict is the shortest span a conflicting merge gets (a clock that
	// runs backwards, or a merge made seconds after the last commit).
	minConflict = time.Minute
	// maxBranches bounds the branches looked at; only those named after an item
	// are walked at all.
	maxBranches = 100
	// maxMerges bounds the merges re-done per collection (each one is a
	// `git merge-tree`).
	maxMerges = 40
)

// Git is the collector of conflict_rework: for every branch named after an
// item, the merge commits that merged the default branch in and conflicted.
// Such a merge ends a span of ConflictRework on the item (capped at
// ConflictCap before it) and carries the conflicted files, so a hotspot (one
// file in most conflicts) can be told from bad luck (flow.ConflictHotspots).
//
// A clean merge of main is no rework (what it costs is a CI run, which the gh
// collector sees). Local only: no fetch, so offline it is as fresh as the last
// fetch, and a repository with no default branch yields nothing.
func Git(o Options) flow.Collector {
	return func(repo string, from, to time.Time) ([]flow.External, error) {
		if len(o.Items) == 0 {
			return nil, nil
		}
		base, ok := gitutil.DefaultBranchRef(repo)
		if !ok {
			return nil, nil
		}
		branches, err := gitutil.Branches(repo, from, maxBranches)
		if err != nil {
			return nil, fmt.Errorf("collect git: %w", err)
		}
		var out []flow.External
		var errs []error
		seen := map[string]bool{} // item+merge: a merge is on every branch that holds it
		budget := maxMerges
		for _, b := range branches {
			item := ItemForBranch(b.Name, o.Items)
			if item == "" || b.Ref == "refs/heads/"+base || b.Ref == "refs/remotes/"+base {
				continue
			}
			merges, err := gitutil.MainMerges(repo, base, b.Ref, from, to)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			for _, m := range merges {
				key := item + m.SHA
				if seen[key] || budget == 0 {
					continue
				}
				seen[key] = true
				budget--
				files, err := gitutil.MergeConflictFiles(repo, m)
				if err != nil {
					errs = append(errs, err)
					continue
				}
				if len(files) > 0 {
					out = append(out, flow.External{Item: item, Span: conflictSpan(m), Files: files})
				}
			}
		}
		return out, errors.Join(errs...)
	}
}

// conflictSpan ends at the merge and starts at the branch's previous commit,
// between minConflict and ConflictCap before it.
func conflictSpan(m gitutil.MergeCommit) flow.Span {
	d := ConflictCap
	if !m.PrevAt.IsZero() {
		d = min(max(m.At.Sub(m.PrevAt), minConflict), ConflictCap)
	}
	return flow.Span{State: flow.ConflictRework, Start: m.At.Add(-d), End: m.At}
}
